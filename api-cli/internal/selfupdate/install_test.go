package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func makeTarGz(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func makeZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractBinaryTarGz(t *testing.T) {
	archive := makeTarGz(t, map[string][]byte{"mineru-cli": []byte("fake-elf-bytes")})
	got, err := ExtractBinary(archive, "mineru-cli_linux_amd64.tar.gz")
	if err != nil {
		t.Fatalf("ExtractBinary: %v", err)
	}
	if string(got) != "fake-elf-bytes" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractBinaryZip(t *testing.T) {
	archive := makeZip(t, map[string][]byte{"mineru-cli.exe": []byte("fake-pe-bytes")})
	got, err := ExtractBinary(archive, "mineru-cli_windows_amd64.zip")
	if err != nil {
		t.Fatalf("ExtractBinary: %v", err)
	}
	if string(got) != "fake-pe-bytes" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractBinaryMissing(t *testing.T) {
	archive := makeTarGz(t, map[string][]byte{"README.md": []byte("hi")})
	if _, err := ExtractBinary(archive, "mineru-cli_linux_amd64.tar.gz"); err == nil {
		t.Fatal("expected an error when the binary is missing from the archive")
	}
}

func TestVerifyChecksum(t *testing.T) {
	content := []byte("hello world")
	sum := sha256.Sum256(content)
	hexSum := hex.EncodeToString(sum[:])
	checksums := []byte(hexSum + "  mineru-cli_linux_amd64.tar.gz\n" + "deadbeef  other-file.zip\n")

	if err := VerifyChecksum(checksums, "mineru-cli_linux_amd64.tar.gz", content); err != nil {
		t.Fatalf("VerifyChecksum: %v", err)
	}
	if err := VerifyChecksum(checksums, "mineru-cli_linux_amd64.tar.gz", []byte("tampered")); err == nil {
		t.Fatal("expected a mismatch error for tampered content")
	}
	if err := VerifyChecksum(checksums, "missing.tar.gz", content); err == nil {
		t.Fatal("expected an error for a name with no checksum entry")
	}
}

func TestReplaceExecutableDirectWrite(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "mineru-cli")
	if err := os.WriteFile(target, []byte("old-version"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := ReplaceExecutable(target, []byte("new-version")); err != nil {
		t.Fatalf("ReplaceExecutable: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new-version" {
		t.Fatalf("content = %q, want %q", got, "new-version")
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("installed file is not executable: mode = %v", info.Mode())
	}

	// No leftover staging files.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("dir has %d entries, want 1: %v", len(entries), entries)
	}
}

func TestInstallOverWindows(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "mineru-cli.exe")
	tmp := filepath.Join(dir, ".mineru-cli-update-123")

	if err := os.WriteFile(target, []byte("old-version"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("new-version"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := installOverWindows(tmp, target); err != nil {
		t.Fatalf("installOverWindows: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new-version" {
		t.Fatalf("content = %q, want %q", got, "new-version")
	}

	// The backup should be cleaned up once the swap succeeds.
	if _, err := os.Stat(target + ".old"); !os.IsNotExist(err) {
		t.Fatalf("expected backup to be removed, stat err = %v", err)
	}
}

func TestInstallOverWindowsCleansUpStaleBackup(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "mineru-cli.exe")
	tmp := filepath.Join(dir, ".mineru-cli-update-123")
	backup := target + ".old"

	if err := os.WriteFile(target, []byte("old-version"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("new-version"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Simulate a leftover backup from a prior update that couldn't delete
	// itself while still running.
	if err := os.WriteFile(backup, []byte("stale-backup"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := installOverWindows(tmp, target); err != nil {
		t.Fatalf("installOverWindows: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new-version" {
		t.Fatalf("content = %q, want %q", got, "new-version")
	}
}

func TestInstallOverWindowsFreshInstall(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "mineru-cli.exe") // does not exist yet
	tmp := filepath.Join(dir, ".mineru-cli-update-123")

	if err := os.WriteFile(tmp, []byte("new-version"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := installOverWindows(tmp, target); err != nil {
		t.Fatalf("installOverWindows: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new-version" {
		t.Fatalf("content = %q, want %q", got, "new-version")
	}
}
