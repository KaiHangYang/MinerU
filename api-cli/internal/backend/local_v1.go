package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Detect before submitting: only a missing V1 endpoint selects the legacy API.
// An unhealthy or unauthorized V1 server must never trigger a second submission.
func (b *LocalBackend) detectProtocol(ctx context.Context) error {
	if b.protocolChecked {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.BaseURL+"/v1/health", nil)
	if err != nil {
		return err
	}
	resp, err := b.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNotFound:
		b.v1 = false
	case http.StatusOK:
		var health struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
			return fmt.Errorf("decode V1 health: %w", err)
		}
		if health.Status != "ok" {
			return fmt.Errorf("unexpected V1 health status: %q", health.Status)
		}
		b.v1 = true
	default:
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("V1 health failed (HTTP %d): %s", resp.StatusCode, raw)
	}
	b.protocolChecked = true
	return nil
}

func (b *LocalBackend) v1JSON(ctx context.Context, method, path string, body, result any) error {
	var data io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		data = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, b.BaseURL+path, data)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s %s failed (HTTP %d): %s", method, path, resp.StatusCode, raw)
	}
	if result == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(result)
}

type v1Upload struct {
	ID      string            `json:"id"`
	Status  string            `json:"status"`
	URL     string            `json:"upload_url"`
	Headers map[string]string `json:"upload_headers"`
	File    struct {
		ID string `json:"id"`
	} `json:"file"`
}

func (b *LocalBackend) uploadV1(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	contentType := mime.TypeByExtension(filepath.Ext(path))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	var upload v1Upload
	err = b.v1JSON(ctx, http.MethodPost, "/v1/uploads", map[string]any{
		"filename": filepath.Base(path), "bytes": info.Size(), "mime_type": contentType, "purpose": "parse",
	}, &upload)
	if err != nil {
		return "", err
	}
	if upload.Status != "completed" {
		if upload.ID == "" || upload.URL == "" {
			return "", fmt.Errorf("upload response missing id or upload_url")
		}
		uploadURL := upload.URL
		if strings.HasPrefix(uploadURL, "/") {
			uploadURL = b.BaseURL + uploadURL
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, f)
		if err != nil {
			return "", err
		}
		req.ContentLength = info.Size()
		for k, v := range upload.Headers {
			req.Header.Set(k, v)
		}
		resp, err := b.Client.Do(req)
		if err != nil {
			return "", err
		}
		raw, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return "", readErr
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return "", fmt.Errorf("upload failed (HTTP %d): %s", resp.StatusCode, raw)
		}
		if err := b.v1JSON(ctx, http.MethodPost, "/v1/uploads/"+url.PathEscape(upload.ID)+"/complete", map[string]any{}, &upload); err != nil {
			return "", err
		}
	}
	if upload.File.ID == "" {
		return "", fmt.Errorf("completed upload missing file.id")
	}
	return upload.File.ID, nil
}

func (b *LocalBackend) submitV1(ctx context.Context, paths []string, opts ParseOptions) (string, error) {
	var tier string
	switch opts.Model {
	case "", "hybrid-engine", "hybrid-auto-engine", "standard":
		tier = "standard"
	case "pipeline", "basic":
		tier = "basic"
	case "vlm", "vlm-engine", "vlm-auto-engine", "advanced":
		tier = "advanced"
	case "flash":
		tier = "flash"
	default:
		return "", fmt.Errorf("unsupported V1 model %q; use flash, basic, standard, or advanced", opts.Model)
	}
	files := make([]map[string]any, 0, len(paths))
	for _, path := range paths {
		id, err := b.uploadV1(ctx, path)
		if err != nil {
			return "", fmt.Errorf("upload %s: %w", path, err)
		}
		entry := map[string]any{"source": map[string]string{"type": "file_id", "file_id": id}}
		if opts.PageRange != "" {
			entry["page_range"] = opts.PageRange
		}
		files = append(files, entry)
	}
	mode := "auto"
	if opts.OCR {
		mode = "ocr"
	}
	var job struct {
		ID string `json:"job_id"`
	}
	err := b.v1JSON(ctx, http.MethodPost, "/v1/parse/jobs", map[string]any{
		"files": files, "tier": tier, "ocr_mode": mode, "output_formats": []string{"zip"},
	}, &job)
	if err != nil {
		return "", err
	}
	if job.ID == "" {
		return "", fmt.Errorf("submit response missing job_id")
	}
	return job.ID, nil
}

func (b *LocalBackend) statusV1(ctx context.Context, jobID string) (JobStatus, error) {
	var job struct {
		Status string `json:"status"`
		Files  []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
			Output struct {
				Zip *struct {
					ID string `json:"file_id"`
				} `json:"zip"`
			} `json:"output_files"`
		} `json:"files"`
	}
	if err := b.v1JSON(ctx, http.MethodGet, "/v1/parse/jobs/"+url.PathEscape(jobID), nil, &job); err != nil {
		return JobStatus{}, err
	}
	result := JobStatus{JobID: jobID}
	for _, file := range job.Files {
		item := FileResult{Name: file.Name, State: StateRunning}
		switch file.Status {
		case "completed":
			item.State = StateDone
		case "failed", "canceled":
			item.State = StateFailed
		}
		if file.Error != nil {
			item.Error = file.Error.Message
		}
		if file.Output.Zip != nil {
			item.ArtifactID = file.Output.Zip.ID
		}
		if job.Status == "canceled" && item.State == StateRunning {
			item.State = StateFailed
			item.Error = "job canceled"
		}
		result.Files = append(result.Files, item)
	}
	if len(result.Files) == 0 {
		return JobStatus{}, fmt.Errorf("job %s returned no files", jobID)
	}
	result.State = result.Overall()
	return result, nil
}

func (b *LocalBackend) downloadV1(ctx context.Context, status JobStatus, outDir string, verbose bool) ([]string, error) {
	if !status.Terminal() {
		return nil, fmt.Errorf("job %s is not finished yet", status.JobID)
	}
	var written []string
	used := map[string]bool{}
	for _, file := range status.Files {
		if file.State != StateDone {
			continue
		}
		if file.ArtifactID == "" {
			return written, fmt.Errorf("missing ZIP output for %s", file.Name)
		}
		dest := outDir
		if len(status.Files) > 1 {
			name := filepath.Base(strings.ReplaceAll(file.Name, "\\", "/"))
			stem := strings.TrimSuffix(name, filepath.Ext(name))
			if stem == "" || stem == "." || stem == ".." {
				stem = "document"
			}
			folder := stem
			for n := 2; used[strings.ToLower(folder)]; n++ {
				folder = fmt.Sprintf("%s-%d", stem, n)
			}
			used[strings.ToLower(folder)] = true
			dest = filepath.Join(outDir, folder)
		}
		paths, err := b.downloadV1Zip(ctx, file.ArtifactID, dest, verbose)
		written = append(written, paths...)
		if err != nil {
			return written, err
		}
	}
	return written, nil
}

func (b *LocalBackend) downloadV1Zip(ctx context.Context, id, dest string, verbose bool) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.BaseURL+"/v1/files/"+url.PathEscape(id)+"/content", nil)
	if err != nil {
		return nil, err
	}
	resp, err := b.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed (HTTP %d)", resp.StatusCode)
	}
	tmp, err := os.CreateTemp("", "mineru-cli-*.zip")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	_, copyErr := io.Copy(tmp, resp.Body)
	closeErr := tmp.Close()
	if copyErr != nil {
		return nil, copyErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return extractZip(tmp.Name(), dest, func(name string) string {
		// V1 bundles already use a flat layout; keep image sidecars with Markdown.
		if !verbose && strings.ToLower(filepath.Ext(name)) == ".json" {
			return ""
		}
		return name
	})
}
