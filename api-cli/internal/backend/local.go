package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// LocalBackend talks to a self-hosted mineru-api server
// (https://github.com/opendatalab/MinerU, the `mineru-api` entrypoint).
type LocalBackend struct {
	BaseURL string
	Client  *http.Client
}

func NewLocalBackend(baseURL string) *LocalBackend {
	return &LocalBackend{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Client:  &http.Client{Timeout: 5 * time.Minute},
	}
}

func (b *LocalBackend) Name() string { return "local" }

type localSubmitResponse struct {
	TaskID string `json:"task_id"`
	Error  string `json:"error"`
	Detail string `json:"detail"`
}

func (b *LocalBackend) Submit(ctx context.Context, files []string, opts ParseOptions) (string, error) {
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)

	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			return "", fmt.Errorf("open %s: %w", path, err)
		}
		part, err := w.CreateFormFile("files", filepath.Base(path))
		if err != nil {
			f.Close()
			return "", err
		}
		if _, err := io.Copy(part, f); err != nil {
			f.Close()
			return "", fmt.Errorf("read %s: %w", path, err)
		}
		f.Close()
	}

	model := opts.Model
	if model == "" {
		model = "hybrid-engine"
	}
	parseMethod := "auto"
	if opts.OCR {
		parseMethod = "ocr"
	}
	fields := map[string]string{
		"backend":              model,
		"parse_method":         parseMethod,
		"formula_enable":       strconv.FormatBool(opts.FormulaEnable),
		"table_enable":         strconv.FormatBool(opts.TableEnable),
		"response_format_zip":  "true",
		"return_images":        "true",
		"return_content_list":  strconv.FormatBool(opts.Verbose),
		"return_middle_json":   strconv.FormatBool(opts.Verbose),
		"return_model_output":  strconv.FormatBool(opts.Verbose),
		"return_original_file": strconv.FormatBool(opts.Verbose),
	}
	if opts.Lang != "" {
		fields["lang_list"] = opts.Lang
	}
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			return "", err
		}
	}
	if err := w.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.BaseURL+"/tasks", body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := b.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("submit to %s: %w", b.BaseURL, err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("submit failed (HTTP %d): %s", resp.StatusCode, string(raw))
	}

	var parsed localSubmitResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("decode submit response: %w", err)
	}
	if parsed.TaskID == "" {
		return "", fmt.Errorf("submit response missing task_id: %s", string(raw))
	}
	return parsed.TaskID, nil
}

type localStatusResponse struct {
	TaskID    string   `json:"task_id"`
	Status    string   `json:"status"`
	FileNames []string `json:"file_names"`
	Error     *string  `json:"error"`
	Message   string   `json:"message"`
}

func (b *LocalBackend) Status(ctx context.Context, jobID string) (JobStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.BaseURL+"/tasks/"+jobID, nil)
	if err != nil {
		return JobStatus{}, err
	}
	resp, err := b.Client.Do(req)
	if err != nil {
		return JobStatus{}, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return JobStatus{}, fmt.Errorf("task %s not found (results expire after the server's retention window)", jobID)
	}
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusOK {
		return JobStatus{}, fmt.Errorf("status check failed (HTTP %d): %s", resp.StatusCode, string(raw))
	}

	var parsed localStatusResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return JobStatus{}, fmt.Errorf("decode status response: %w", err)
	}

	state := StateRunning
	errMsg := ""
	switch parsed.Status {
	case "pending", "processing":
		state = StateRunning
	case "completed":
		state = StateDone
	case "failed":
		state = StateFailed
		if parsed.Error != nil {
			errMsg = *parsed.Error
		}
	default:
		state = StateRunning
	}

	names := parsed.FileNames
	if len(names) == 0 {
		names = []string{jobID}
	}
	files := make([]FileResult, 0, len(names))
	for _, n := range names {
		files = append(files, FileResult{Name: n, State: state, Error: errMsg})
	}

	return JobStatus{JobID: jobID, State: state, Files: files}, nil
}

// Download extracts whatever the server sent; verbose is unused because the
// local backend already chose what to include back at Submit time.
func (b *LocalBackend) Download(ctx context.Context, jobID string, status JobStatus, outDir string, verbose bool) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.BaseURL+"/tasks/"+jobID+"/result", nil)
	if err != nil {
		return nil, err
	}
	resp, err := b.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusAccepted {
		return nil, fmt.Errorf("task %s is not finished yet", jobID)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("download failed (HTTP %d): %s", resp.StatusCode, string(raw))
	}

	tmp, err := os.CreateTemp("", "mineru-cli-*.zip")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return nil, err
	}
	tmp.Close()

	return extractZip(tmp.Name(), outDir, flattenLocalEntry(len(status.Files) > 1))
}

// flattenLocalEntry strips the local mineru-api server's zip layout of
// "<pdf_name>/<method_dir>/<relative_path>" (method_dir being e.g. "auto",
// "vlm", "hybrid_auto" — an implementation detail the caller already knows,
// having asked for it) down to just "<relative_path>". When the job has more
// than one file, the "<pdf_name>" level is kept as "<pdf_name>/<relative_path>"
// so results from different files don't collide in outDir; with a single
// file there's nothing to disambiguate, so it's dropped too.
func flattenLocalEntry(multiFile bool) func(string) string {
	return func(name string) string {
		parts := strings.Split(filepath.ToSlash(name), "/")
		if len(parts) < 2 {
			return name
		}
		rest := parts[2:]
		if multiFile {
			rest = append([]string{parts[0]}, rest...)
		}
		if len(rest) == 0 {
			return ""
		}
		return filepath.Join(rest...)
	}
}
