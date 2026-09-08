package backend

import (
	"archive/zip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFlattenLocalEntry(t *testing.T) {
	cases := []struct {
		name      string
		multiFile bool
		in        string
		want      string
	}{
		{"single file drops pdf name and method dir", false, "report/auto/report.md", "report.md"},
		{"single file flattens images too", false, "report/hybrid_auto/images/pic.jpg", filepath.Join("images", "pic.jpg")},
		{"single file bare dir entry is skipped", false, "report/auto", ""},
		{"multi file keeps pdf name, drops method dir", true, "report/auto/report.md", filepath.Join("report", "report.md")},
		{"multi file keeps pdf name for images", true, "report/vlm/images/pic.jpg", filepath.Join("report", "images", "pic.jpg")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := flattenLocalEntry(tc.multiFile)(tc.in)
			if got != tc.want {
				t.Fatalf("flattenLocalEntry(%v)(%q) = %q, want %q", tc.multiFile, tc.in, got, tc.want)
			}
		})
	}
}

// TestLocalBackendDownloadFlattensSingleFile checks that a single-file job's
// server-side "<pdf_name>/<method_dir>/..." zip layout lands flat in outDir.
func TestLocalBackendDownloadFlattensSingleFile(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/tasks/job-1/result", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		zw := zip.NewWriter(w)
		for name, content := range map[string]string{
			"report/auto/report.md":    "# report",
			"report/auto/images/a.jpg": "imgbytes",
		} {
			f, _ := zw.Create(name)
			_, _ = f.Write([]byte(content))
		}
		_ = zw.Close()
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	be := NewLocalBackend(srv.URL)
	status := JobStatus{Files: []FileResult{{Name: "report.pdf", State: StateDone}}}

	outDir := t.TempDir()
	written, err := be.Download(context.Background(), "job-1", status, outDir, false)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if len(written) != 2 {
		t.Fatalf("written = %v, want 2 files", written)
	}
	if _, err := os.ReadFile(filepath.Join(outDir, "report.md")); err != nil {
		t.Fatalf("expected flat report.md in outDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "report")); !os.IsNotExist(err) {
		t.Fatalf("did not expect a leftover pdf-name subfolder, stat err = %v", err)
	}
}

// TestLocalBackendDownloadKeepsPerFileFolderForMultiFile checks that a
// multi-file job still gets one subfolder per file (dropping only the
// method dir), so results from different files don't collide.
func TestLocalBackendDownloadKeepsPerFileFolderForMultiFile(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/tasks/job-2/result", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		zw := zip.NewWriter(w)
		for name, content := range map[string]string{
			"a/auto/a.md": "# a",
			"b/auto/b.md": "# b",
		} {
			f, _ := zw.Create(name)
			_, _ = f.Write([]byte(content))
		}
		_ = zw.Close()
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	be := NewLocalBackend(srv.URL)
	status := JobStatus{Files: []FileResult{
		{Name: "a.pdf", State: StateDone},
		{Name: "b.pdf", State: StateDone},
	}}

	outDir := t.TempDir()
	if _, err := be.Download(context.Background(), "job-2", status, outDir, false); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if _, err := os.ReadFile(filepath.Join(outDir, "a", "a.md")); err != nil {
		t.Fatalf("expected outDir/a/a.md: %v", err)
	}
	if _, err := os.ReadFile(filepath.Join(outDir, "b", "b.md")); err != nil {
		t.Fatalf("expected outDir/b/b.md: %v", err)
	}
}

// TestLocalBackendSubmitDefaultsModelToHybridEngine checks that omitting
// --model on the local backend now defaults to "hybrid-engine" rather than
// "pipeline".
func TestLocalBackendSubmitDefaultsModelToHybridEngine(t *testing.T) {
	var gotBackend string
	mux := http.NewServeMux()
	mux.HandleFunc("/tasks", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		gotBackend = r.FormValue("backend")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"task_id":"job-1"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.4"), 0o644); err != nil {
		t.Fatal(err)
	}

	be := NewLocalBackend(srv.URL)
	if _, err := be.Submit(context.Background(), []string{path}, ParseOptions{}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if gotBackend != "hybrid-engine" {
		t.Fatalf("backend field = %q, want hybrid-engine", gotBackend)
	}
}
