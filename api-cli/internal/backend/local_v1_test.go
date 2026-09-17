package backend

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalV1Flow(t *testing.T) {
	for _, verbose := range []bool{false, true} {
		t.Run(fmt.Sprint(verbose), func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"status":"ok"}`) })
			mux.HandleFunc("POST /v1/uploads", func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body["filename"] != "a.pdf" || body["bytes"] != float64(8) {
					t.Errorf("unexpected upload: %v", body)
				}
				fmt.Fprint(w, `{"id":"upload-1","status":"pending","upload_url":"/v1/uploads/upload-1/content","upload_headers":{"X-Upload":"yes"}}`)
			})
			mux.HandleFunc("PUT /v1/uploads/upload-1/content", func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				if string(raw) != "%PDF-1.4" || r.Header.Get("X-Upload") != "yes" {
					t.Errorf("bad upload: %q", raw)
				}
				w.WriteHeader(http.StatusNoContent)
			})
			mux.HandleFunc("POST /v1/uploads/upload-1/complete", func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, `{"status":"completed","file":{"id":"input-1"}}`)
			})
			mux.HandleFunc("POST /v1/parse/jobs", func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Tier    string   `json:"tier"`
					OCR     string   `json:"ocr_mode"`
					Formats []string `json:"output_formats"`
					Files   []struct {
						PageRange string `json:"page_range"`
						Source    struct {
							Type string `json:"type"`
							ID   string `json:"file_id"`
						} `json:"source"`
					} `json:"files"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body.Tier != "standard" || body.OCR != "ocr" || len(body.Formats) != 1 || body.Formats[0] != "zip" || len(body.Files) != 1 {
					t.Errorf("unexpected job: %+v", body)
					return
				}
				if body.Files[0].PageRange != "1-2" || body.Files[0].Source.ID != "input-1" {
					t.Errorf("unexpected files: %+v", body.Files)
				}
				w.WriteHeader(http.StatusAccepted)
				fmt.Fprint(w, `{"job_id":"job-1"}`)
			})
			mux.HandleFunc("GET /v1/parse/jobs/job-1", func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, `{"status":"completed","files":[{"name":"a.pdf","status":"completed","output_files":{"zip":{"file_id":"zip-1"}}}]}`)
			})
			mux.HandleFunc("GET /v1/files/zip-1/content", func(w http.ResponseWriter, r *http.Request) {
				zw := zip.NewWriter(w)
				for name, data := range map[string]string{"markdown.md": "# A", "images/a.png": "image", "middle_json.json": "{}"} {
					f, _ := zw.Create(name)
					fmt.Fprint(f, data)
				}
				zw.Close()
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()
			dir := t.TempDir()
			path := filepath.Join(dir, "a.pdf")
			if err := os.WriteFile(path, []byte("%PDF-1.4"), 0600); err != nil {
				t.Fatal(err)
			}
			b := NewLocalBackend(srv.URL)
			id, err := b.Submit(context.Background(), []string{path}, ParseOptions{OCR: true, PageRange: "1-2"})
			if err != nil || id != "job-1" {
				t.Fatalf("submit: %q %v", id, err)
			}
			// Separate CLI invocations must rediscover V1 from just the URL and job ID.
			b = NewLocalBackend(srv.URL)
			status, err := b.Status(context.Background(), id)
			if err != nil || status.State != StateDone {
				t.Fatalf("status: %+v %v", status, err)
			}
			out := filepath.Join(dir, "out")
			paths, err := b.Download(context.Background(), id, status, out, verbose)
			if err != nil {
				t.Fatal(err)
			}
			want := 2
			if verbose {
				want = 3
			}
			if len(paths) != want {
				t.Fatalf("paths: %v", paths)
			}
			if raw, err := os.ReadFile(filepath.Join(out, "markdown.md")); err != nil || string(raw) != "# A" {
				t.Fatalf("markdown: %q %v", raw, err)
			}
		})
	}
}

func TestLocalV1UnhealthyDoesNotFallBack(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "GPU unhealthy", 503) })
	mux.HandleFunc("/tasks", func(w http.ResponseWriter, r *http.Request) { t.Error("must not submit to legacy API") })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	if _, err := NewLocalBackend(srv.URL).Submit(context.Background(), nil, ParseOptions{}); err == nil {
		t.Fatal("expected health error")
	}
}

func TestLocalV1PartialAndCanceled(t *testing.T) {
	for _, state := range []string{"partial", "canceled"} {
		t.Run(state, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"status":"ok"}`) })
			mux.HandleFunc("/v1/parse/jobs/job-1", func(w http.ResponseWriter, r *http.Request) {
				pending := "failed"
				if state == "canceled" {
					pending = "queued"
				}
				fmt.Fprintf(w, `{"status":%q,"files":[{"name":"a.pdf","status":"completed","output_files":{"zip":{"file_id":"zip-1"}}},{"name":"b.pdf","status":%q,"error":{"message":"failed"}}]}`, state, pending)
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()
			status, err := NewLocalBackend(srv.URL).Status(context.Background(), "job-1")
			if err != nil || !status.Terminal() || status.State != StatePartial {
				t.Fatalf("status: %+v %v", status, err)
			}
		})
	}
}
