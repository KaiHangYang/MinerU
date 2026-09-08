package backend

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestCloudBackendFlow exercises Submit -> Status -> Download against a mock
// server shaped like the documented mineru.net responses, since we don't
// have a real API token to test against in CI. It pins down: auth header,
// the file-urls/batch request/response shape, the header-less PUT upload,
// and the extract-results/batch polling + zip download.
func TestCloudBackendFlow(t *testing.T) {
	const wantToken = "test-token-123"
	var uploadedBytes []byte

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/file-urls/batch", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+wantToken {
			t.Errorf("Authorization header = %q, want Bearer %s", got, wantToken)
		}
		var req cloudBatchSubmitReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(req.Files) != 1 || req.Files[0].Name != "test.pdf" {
			t.Fatalf("unexpected files in request: %+v", req.Files)
		}
		if req.ModelVersion != "pipeline" || req.Language != "ch" {
			t.Fatalf("unexpected options: model=%s lang=%s", req.ModelVersion, req.Language)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"batch_id":"batch-1","file_urls":["` + serverURL + `/upload/1"]}}`))
	})
	mux.HandleFunc("/upload/1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("upload method = %s, want PUT", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "" {
			t.Errorf("upload Content-Type = %q, want empty (docs say no Content-Type)", ct)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read upload body: %v", err)
		}
		uploadedBytes = body
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/v4/extract-results/batch/batch-1", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+wantToken {
			t.Errorf("Authorization header = %q, want Bearer %s", got, wantToken)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"batch_id":"batch-1","extract_result":[
			{"file_name":"test.pdf","state":"done","full_zip_url":"` + serverURL + `/zip/1","data_id":"test.pdf"}
		]}}`))
	})
	mux.HandleFunc("/zip/1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		zw := zip.NewWriter(w)
		f, _ := zw.Create("full.md")
		_, _ = f.Write([]byte("# hello from mineru.net"))
		_ = zw.Close()
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()
	serverURL = srv.URL // referenced by the handlers above via closure

	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "test.pdf")
	if err := os.WriteFile(srcPath, []byte("%PDF-1.4 fake content"), 0o644); err != nil {
		t.Fatal(err)
	}

	be := NewCloudBackend(srv.URL, wantToken)

	jobID, err := be.Submit(context.Background(), []string{srcPath}, ParseOptions{
		Lang: "ch", Model: "pipeline", FormulaEnable: true, TableEnable: true,
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if jobID != "batch-1" {
		t.Fatalf("jobID = %q, want batch-1", jobID)
	}
	if !bytes.Equal(uploadedBytes, []byte("%PDF-1.4 fake content")) {
		t.Fatalf("uploaded bytes = %q, want file contents", uploadedBytes)
	}

	status, err := be.Status(context.Background(), jobID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != StateDone {
		t.Fatalf("status.State = %q, want done", status.State)
	}
	if !status.Terminal() {
		t.Fatalf("status.Terminal() = false, want true")
	}

	outDir := filepath.Join(tmpDir, "out")
	written, err := be.Download(context.Background(), jobID, status, outDir, false)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if len(written) != 1 {
		t.Fatalf("Download wrote %d files, want 1: %v", len(written), written)
	}
	content, err := os.ReadFile(filepath.Join(outDir, "full.md"))
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(content) != "# hello from mineru.net" {
		t.Fatalf("downloaded content = %q", content)
	}
}

// serverURL lets the mock handlers above self-reference the httptest server's
// URL (only known once httptest.NewServer returns).
var serverURL string

// TestCloudBackendDownloadPruning checks that Download keeps only markdown
// and images by default, matching what the local backend returns, and keeps
// everything when verbose is set.
func TestCloudBackendDownloadPruning(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/zip/full", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		zw := zip.NewWriter(w)
		for name, content := range map[string]string{
			"full.md":                  "# doc",
			"layout.json":              "{}",
			"abc123_model.json":        "{}",
			"abc123_origin.pdf":        "%PDF",
			"abc123_content_list.json": "[]",
			"images/pic.jpg":           "jpgbytes",
		} {
			f, _ := zw.Create(name)
			_, _ = f.Write([]byte(content))
		}
		_ = zw.Close()
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	be := NewCloudBackend(srv.URL, "tok")
	status := JobStatus{Files: []FileResult{
		{Name: "doc.pdf", State: StateDone, ZipURL: srv.URL + "/zip/full"},
	}}

	t.Run("default prunes extras", func(t *testing.T) {
		outDir := t.TempDir()
		written, err := be.Download(context.Background(), "job", status, outDir, false)
		if err != nil {
			t.Fatalf("Download: %v", err)
		}
		gotNames := basenames(written)
		wantNames := []string{"full.md", "pic.jpg"}
		if !sameSet(gotNames, wantNames) {
			t.Fatalf("written = %v, want only %v", gotNames, wantNames)
		}
		if _, err := os.Stat(filepath.Join(outDir, "layout.json")); !os.IsNotExist(err) {
			t.Fatalf("layout.json should have been deleted, stat err = %v", err)
		}
	})

	t.Run("verbose keeps everything", func(t *testing.T) {
		outDir := t.TempDir()
		written, err := be.Download(context.Background(), "job", status, outDir, true)
		if err != nil {
			t.Fatalf("Download: %v", err)
		}
		if len(written) != 6 {
			t.Fatalf("written = %v, want 6 files", written)
		}
	})
}

func basenames(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = filepath.Base(p)
	}
	return out
}

func sameSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := map[string]bool{}
	for _, g := range got {
		seen[g] = true
	}
	for _, w := range want {
		if !seen[w] {
			return false
		}
	}
	return true
}
