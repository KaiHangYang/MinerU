package backend

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalTasksFlow(t *testing.T) {
	for _, cached := range []bool{false, true} {
		t.Run(fmt.Sprint(cached), func(t *testing.T) {
			const prefix = "/proxy/mineru"
			const key = "secret-key"
			uploads, queries := 0, 0
			mux := http.NewServeMux()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer "+key {
					t.Error("missing API key")
					w.WriteHeader(401)
					return
				}
				mux.ServeHTTP(w, r)
			}))
			defer srv.Close()
			mux.HandleFunc("GET "+prefix+"/v1/health", func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, `{"status":"ok","features":{"task_output_formats":["default","docx","html","latex"],"sources":["upload","url"]}}`)
			})
			mux.HandleFunc("POST "+prefix+"/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if _, exists := body["tier"]; exists {
					t.Error("omitted model must select the server default")
				}
				if body["ocr_mode"] != "ocr" || body["page_range"] != "r1" || fmt.Sprint(body["output_formats"]) != "[default]" {
					t.Errorf("request: %v", body)
				}
				files := body["files"].([]any)
				if len(files) != 2 {
					t.Fatalf("files: %v", files)
				}
				for _, raw := range files {
					f := raw.(map[string]any)
					hash := fmt.Sprintf("%x", sha256.Sum256([]byte("%PDF-1.4")))
					if f["file_name"] != "same.pdf" || f["bytes"] != float64(8) || f["sha256sum"] != hash {
						t.Errorf("manifest: %v", f)
					}
				}
				w.WriteHeader(202)
				// First file can already be cached. Second waits for Router preparation.
				state := "waiting-file"
				if cached {
					state = "pending"
				}
				fmt.Fprintf(w, `{"task_id":"task-1","status":"waiting-file","files":[{"file_id":"a","file_name":"same.pdf","status":%q},{"file_id":"b","file_name":"same.pdf","status":"waiting-file"}]}`, state)
			})
			mux.HandleFunc("GET "+prefix+"/v1/tasks/task-1", func(w http.ResponseWriter, r *http.Request) {
				queries++
				if queries == 1 {
					first := `{"file_id":"a","file_name":"same.pdf","status":"pending"}`
					if !cached {
						first = fmt.Sprintf(`{"file_id":"a","file_name":"same.pdf","status":"waiting-file","upload":{"url":%q,"headers":{"X-Upload":"yes"}}}`, prefix+"/put/a")
					}
					// Snapshot order differs from creation; same names still have distinct IDs.
					fmt.Fprintf(w, `{"task_id":"task-1","status":"waiting-file","files":[{"file_id":"b","file_name":"same.pdf","status":"waiting-file","upload":{"url":%q,"headers":{"X-Upload":"yes"}}},%s]}`, srv.URL+prefix+"/put/b", first)
					return
				}
				fmt.Fprintf(w, `{"task_id":"task-1","status":"completed","files":[{"file_id":"b","file_name":"same.pdf","status":"completed","result_url":%q},{"file_id":"a","file_name":"same.pdf","status":"completed","result_url":%q}]}`, prefix+"/result", srv.URL+prefix+"/result")
			})
			mux.HandleFunc("PUT "+prefix+"/put/{id}", func(w http.ResponseWriter, r *http.Request) {
				uploads++
				data, _ := io.ReadAll(r.Body)
				if string(data) != "%PDF-1.4" || r.ContentLength != 8 || r.Header.Get("X-Upload") != "yes" {
					t.Errorf("upload: %q %v", data, r.Header)
				}
				w.WriteHeader(200)
				fmt.Fprint(w, `{"task_id":"task-1","status":"pending"}`)
			})
			mux.HandleFunc("GET "+prefix+"/result", func(w http.ResponseWriter, r *http.Request) {
				zw := zip.NewWriter(w)
				for name, content := range map[string]string{"markdown.md": "# parsed", "images/p.png": "png", "middle_json.json": "{}"} {
					f, _ := zw.Create(name)
					fmt.Fprint(f, content)
				}
				zw.Close()
			})
			dir := t.TempDir()
			var paths []string
			for _, sub := range []string{"one", "two"} {
				folder := filepath.Join(dir, sub)
				os.MkdirAll(folder, 0700)
				path := filepath.Join(folder, "same.pdf")
				os.WriteFile(path, []byte("%PDF-1.4"), 0600)
				paths = append(paths, path)
			}
			b := NewLocalBackend(srv.URL + prefix)
			b.APIKey = key
			id, err := b.Submit(context.Background(), paths, ParseOptions{OCR: true, PageRange: "r1"})
			if err != nil || id != "task-1" {
				t.Fatalf("submit: %q %v", id, err)
			}
			wantUploads := 2
			if cached {
				wantUploads = 1
			}
			if uploads != wantUploads {
				t.Fatalf("uploads: %d", uploads)
			}
			// status/result run in another process without retaining any submit state.
			b = NewLocalBackend(srv.URL + prefix)
			b.APIKey = key
			status, err := b.Status(context.Background(), id)
			if err != nil || status.State != StateDone {
				t.Fatalf("status: %+v %v", status, err)
			}
			for _, verbose := range []bool{false, true} {
				out := filepath.Join(dir, fmt.Sprint(verbose))
				written, err := b.Download(context.Background(), id, status, out, verbose)
				want := 4
				if verbose {
					want = 6
				}
				if err != nil || len(written) != want {
					t.Fatalf("download: %v %v", written, err)
				}
				for _, folder := range []string{"same", "same-2"} {
					data, err := os.ReadFile(filepath.Join(out, folder, "markdown.md"))
					if err != nil || string(data) != "# parsed" {
						t.Fatalf("output: %q %v", data, err)
					}
				}
			}
		})
	}
}

func TestLocalTasksExternalStorageAuth(t *testing.T) {
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("API key leaked to storage")
		}
		if r.Method == "PUT" && r.Header.Get("X-Signed") != "yes" {
			t.Error("missing signed header")
		}
		io.Copy(io.Discard, r.Body)
		if r.Method == http.MethodGet {
			zw := zip.NewWriter(w)
			f, _ := zw.Create("markdown.md")
			fmt.Fprint(f, "# external result")
			zw.Close()
			return
		}
		w.WriteHeader(204)
	}))
	defer storage.Close()
	b := NewLocalBackend("https://api.example.com/prefix")
	b.APIKey = "secret"
	dir := t.TempDir()
	path := filepath.Join(dir, "a.pdf")
	os.WriteFile(path, []byte("pdf"), 0600)
	var file taskFile
	json.Unmarshal([]byte(fmt.Sprintf(`{"upload":{"url":%q,"headers":{"X-Signed":"yes"}}}`, storage.URL)), &file)
	if err := b.uploadTaskFile(context.Background(), path, file); err != nil {
		t.Fatal(err)
	}
	if written, err := b.downloadV1Zip(context.Background(), storage.URL, filepath.Join(dir, "out"), false); err != nil || len(written) != 1 {
		t.Fatalf("external download: %v %v", written, err)
	}
}

func TestLocalTasksTerminalStates(t *testing.T) {
	for _, state := range []string{"failed", "canceled", "partial"} {
		t.Run(state, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "health") {
					fmt.Fprint(w, `{"status":"ok","features":{"task_output_formats":["default"],"sources":["upload"]}}`)
					return
				}
				first := `{"file_id":"a","file_name":"a.pdf","status":"failed","error":{"message":"bad file"}}`
				if state == "partial" {
					first = `{"file_id":"a","file_name":"a.pdf","status":"completed","result_url":"https://storage.example/result"}`
				}
				fmt.Fprintf(w, `{"task_id":"task-1","status":%q,"error":{"message":"task stopped"},"files":[%s,{"file_id":"b","file_name":"b.pdf","status":"canceled"}]}`, state, first)
			}))
			defer srv.Close()
			status, err := NewLocalBackend(srv.URL).Status(context.Background(), "task-1")
			want := StateFailed
			if state == "partial" {
				want = StatePartial
			}
			if err != nil || !status.Terminal() || status.State != want || status.Files[1].Error != "task stopped" {
				t.Fatalf("status: %+v %v", status, err)
			}
		})
	}
}

func TestLocalTasksSubmissionErrorsDoNotFallBack(t *testing.T) {
	for _, code := range []int{400, 401, 503} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			submissions := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/health":
					fmt.Fprint(w, `{"status":"ok","features":{"task_output_formats":["default"],"sources":["upload"]}}`)
				case "/v1/tasks":
					submissions++
					w.WriteHeader(code)
					fmt.Fprint(w, `{"error":{"code":"invalid_request","message":"rejected"}}`)
				default:
					t.Errorf("unexpected fallback request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer srv.Close()
			path := filepath.Join(t.TempDir(), "a.pdf")
			os.WriteFile(path, []byte("pdf"), 0600)
			if _, err := NewLocalBackend(srv.URL).Submit(context.Background(), []string{path}, ParseOptions{}); err == nil {
				t.Fatal("expected submission error")
			}
			if submissions != 1 {
				t.Fatalf("submissions: %d", submissions)
			}
		})
	}
}
