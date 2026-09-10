package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// downloadClient is separate from apiClient: archives can be tens of MB, so
// it gets a much longer timeout than the small JSON metadata calls.
var downloadClient = &http.Client{Timeout: 5 * time.Minute}

// Download fetches url and returns its full body.
func Download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := downloadClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET %s: HTTP %d: %s", url, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return io.ReadAll(resp.Body)
}

// VerifyChecksum checks content against its sha256sum-style entry (as
// produced by `sha256sum` and published as the release's checksums.txt) for
// assetName.
func VerifyChecksum(checksumsFile []byte, assetName string, content []byte) error {
	var want string
	sc := bufio.NewScanner(bytes.NewReader(checksumsFile))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && fields[1] == assetName {
			want = strings.ToLower(fields[0])
			break
		}
	}
	if want == "" {
		return fmt.Errorf("no checksum entry for %s", assetName)
	}
	sum := sha256.Sum256(content)
	got := hex.EncodeToString(sum[:])
	if got != want {
		return fmt.Errorf("sha256 mismatch for %s: got %s, want %s", assetName, got, want)
	}
	return nil
}

const (
	binaryNameUnix    = "mineru-cli"
	binaryNameWindows = "mineru-cli.exe"
)

// ExtractBinary pulls the mineru-cli executable out of a release archive
// (a .tar.gz for Unix platforms, a .zip for Windows, per AssetName).
func ExtractBinary(archive []byte, assetName string) ([]byte, error) {
	if strings.HasSuffix(assetName, ".zip") {
		return extractFromZip(archive)
	}
	return extractFromTarGz(archive)
}

func extractFromTarGz(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(hdr.Name) == binaryNameUnix {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("%s not found in archive", binaryNameUnix)
}

func extractFromZip(archive []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if filepath.Base(f.Name) == binaryNameWindows {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("%s not found in archive", binaryNameWindows)
}

// ReplaceExecutable atomically swaps content in as targetPath, which may be
// the binary currently executing this process (safe on Unix: replacing a
// running file's directory entry doesn't disturb the process already
// holding it open).
//
// If targetPath's directory isn't writable by the current user (a common
// case for /usr/local/bin), it falls back to staging content in a temp file
// and installing it with `sudo`, which will prompt for a password on the
// controlling terminal.
func ReplaceExecutable(targetPath string, content []byte) error {
	dir := filepath.Dir(targetPath)

	tmp, err := os.CreateTemp(dir, ".mineru-cli-update-*")
	if err == nil {
		tmpPath := tmp.Name()
		writeErr := writeExecutable(tmp, content)
		if writeErr != nil {
			os.Remove(tmpPath)
			return writeErr
		}
		if err := installOver(tmpPath, targetPath); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("install %s: %w", targetPath, err)
		}
		return nil
	}
	if !os.IsPermission(err) {
		return fmt.Errorf("stage update in %s: %w", dir, err)
	}

	return replaceViaSudo(targetPath, content)
}

// installOver moves the staged file at tmpPath into place as targetPath.
func installOver(tmpPath, targetPath string) error {
	if runtime.GOOS != "windows" {
		return os.Rename(tmpPath, targetPath)
	}
	return installOverWindows(tmpPath, targetPath)
}

// installOverWindows handles the case ReplaceExecutable exists for in the
// first place: targetPath is usually the binary currently executing this
// process. Unlike Unix, Windows won't let a rename atomically replace a
// file that's memory-mapped for execution — os.Rename(tmpPath, targetPath)
// fails with "Access is denied" even though moving that same running file
// *aside* to a fresh name is fine. So here we move the running target out
// of the way first, then move the new binary into its place.
//
// The backup can't always be removed immediately (the old process may still
// be exiting), so cleanup is best-effort; a leftover backup from a previous
// update is cleaned up here too, once Windows has released it.
func installOverWindows(tmpPath, targetPath string) error {
	backupPath := targetPath + ".old"
	os.Remove(backupPath)

	if err := os.Rename(targetPath, backupPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		return err
	}
	os.Remove(backupPath)
	return nil
}

func writeExecutable(f *os.File, content []byte) error {
	if _, err := f.Write(content); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Chmod(f.Name(), 0o755)
}

func replaceViaSudo(targetPath string, content []byte) error {
	sudoPath, err := exec.LookPath("sudo")
	if err != nil {
		return fmt.Errorf(
			"%s is not writable by the current user and sudo was not found on PATH; "+
				"re-run as an administrator, or pass --output to install somewhere else on PATH",
			targetPath,
		)
	}

	stage, err := os.CreateTemp("", "mineru-cli-update-*")
	if err != nil {
		return err
	}
	stagePath := stage.Name()
	defer os.Remove(stagePath)
	if err := writeExecutable(stage, content); err != nil {
		return err
	}

	fmt.Printf("%s is not writable by the current user; installing with sudo (you may be prompted for your password)\n", targetPath)
	cmd := exec.Command(sudoPath, "install", "-m", "0755", stagePath, targetPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sudo install: %w", err)
	}
	return nil
}
