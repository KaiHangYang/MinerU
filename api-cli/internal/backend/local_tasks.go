package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func localTier(model string) (string, error) {
	switch model {
	case "", "flash", "basic", "standard", "advanced":
		return model, nil
	case "hybrid-engine", "hybrid-auto-engine":
		return "standard", nil
	case "pipeline":
		return "basic", nil
	case "vlm", "vlm-engine", "vlm-auto-engine":
		return "advanced", nil
	default:
		return "", fmt.Errorf("unsupported V1 model %q; use flash, basic, standard, or advanced", model)
	}
}

// Resolve server-provided links without duplicating reverse-proxy prefixes.
func (b *LocalBackend) resolveLink(link string) (string, error) {
	base, err := url.Parse(b.BaseURL + "/")
	if err != nil {
		return "", err
	}
	ref, err := url.Parse(link)
	if err != nil {
		return "", err
	}
	target := base.ResolveReference(ref)
	if link == "" || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" || target.User != nil {
		return "", fmt.Errorf("invalid task URL %q", link)
	}
	return target.String(), nil
}

func sameOrigin(a, b *url.URL) bool {
	port := func(u *url.URL) string {
		if u.Port() != "" {
			return u.Port()
		}
		if u.Scheme == "https" {
			return "443"
		}
		return "80"
	}
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Hostname(), b.Hostname()) && port(a) == port(b)
}

// Signed storage URLs must never receive the API deployment's credentials.
func (b *LocalBackend) authorize(req *http.Request) {
	base, err := url.Parse(b.BaseURL)
	if err == nil && b.APIKey != "" && sameOrigin(base, req.URL) {
		req.Header.Set("Authorization", "Bearer "+b.APIKey)
	} else if b.APIKey != "" && req.Header.Get("Authorization") == "Bearer "+b.APIKey {
		req.Header.Del("Authorization")
	}
}

type taskFile struct {
	ID     string `json:"file_id"`
	Name   string `json:"file_name"`
	Status string `json:"status"`
	Upload *struct {
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
	} `json:"upload"`
	ResultURL string `json:"result_url"`
	Error     *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type taskResponse struct {
	ID     string     `json:"task_id"`
	Status string     `json:"status"`
	Files  []taskFile `json:"files"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (b *LocalBackend) submitTasks(ctx context.Context, paths []string, opts ParseOptions) (string, error) {
	tier, err := localTier(opts.Model)
	if err != nil {
		return "", err
	}
	if len(paths) == 0 || len(paths) > 100 {
		return "", fmt.Errorf("Tasks requires 1–100 files")
	}
	files := make([]map[string]any, 0, len(paths))
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return "", err
		}
		hash := sha256.New()
		size, err := io.Copy(hash, f)
		f.Close()
		if err != nil {
			return "", err
		}
		if size == 0 {
			return "", fmt.Errorf("empty input file: %s", path)
		}
		files = append(files, map[string]any{
			"file_name": filepath.Base(path), "bytes": size, "sha256sum": hex.EncodeToString(hash.Sum(nil)),
		})
	}
	mode := "auto"
	if opts.OCR {
		mode = "ocr"
	}
	body := map[string]any{"files": files, "ocr_mode": mode, "page_range": opts.PageRange, "output_formats": []string{"default"}}
	if tier != "" {
		body["tier"] = tier
	}
	var task taskResponse
	if err := b.v1JSON(ctx, http.MethodPost, "/v1/tasks", body, &task); err != nil {
		return "", err
	}
	if task.ID == "" || len(task.Files) != len(paths) {
		return "", fmt.Errorf("invalid Tasks creation response")
	}
	// Only the creation receipt is ordered. Later snapshots are matched by ID,
	// allowing repeated filenames and server-side reordering.
	inputs := make(map[string]int, len(paths))
	for i, file := range task.Files {
		if _, duplicate := inputs[file.ID]; file.ID == "" || duplicate || file.Name != filepath.Base(paths[i]) {
			return "", fmt.Errorf("invalid task input identity (task %s)", task.ID)
		}
		inputs[file.ID] = i
	}
	// Router/storage preparation may temporarily return no upload descriptor.
	// Finish handing off all inputs before the submit command exits.
	uploadCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	accepted := make(map[string]bool, len(paths))
	id := task.ID
	for {
		if task.ID != id || len(task.Files) != len(paths) {
			return "", fmt.Errorf("task input identity changed (task %s)", id)
		}
		seen := make(map[string]bool, len(paths))
		for _, file := range task.Files {
			i, exists := inputs[file.ID]
			if !exists || seen[file.ID] || file.Name != filepath.Base(paths[i]) {
				return "", fmt.Errorf("task input identity changed (task %s)", id)
			}
			seen[file.ID] = true
			if task.Status == "failed" || task.Status == "canceled" {
				accepted[file.ID] = true
			}
			if accepted[file.ID] {
				continue
			}
			switch file.Status {
			case "waiting-file":
				if file.Upload == nil {
					continue
				}
				if err := b.uploadTaskFile(uploadCtx, paths[i], file); err != nil {
					return "", fmt.Errorf("upload %s (task %s): %w", paths[i], id, err)
				}
			case "pending", "running", "completed", "failed", "canceled":
				// A cache hit or terminal outcome needs no client upload.
			default:
				return "", fmt.Errorf("unknown file status %q (task %s)", file.Status, id)
			}
			accepted[file.ID] = true
		}
		if len(accepted) == len(paths) {
			return id, nil
		}
		select {
		case <-uploadCtx.Done():
			return "", fmt.Errorf("waiting for upload links (task %s): %w", id, uploadCtx.Err())
		case <-time.After(250 * time.Millisecond):
		}
		task = taskResponse{}
		if err := b.v1JSON(uploadCtx, http.MethodGet, "/v1/tasks/"+url.PathEscape(id), nil, &task); err != nil {
			return "", fmt.Errorf("query upload links (task %s): %w", id, err)
		}
	}
}

func (b *LocalBackend) uploadTaskFile(ctx context.Context, path string, file taskFile) error {
	target, err := b.resolveLink(file.Upload.URL)
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, target, f)
	if err != nil {
		return err
	}
	for key, value := range file.Upload.Headers {
		req.Header.Set(key, value)
	}
	b.authorize(req)
	req.ContentLength = info.Size()
	resp, err := b.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("upload failed (HTTP %d): %s", resp.StatusCode, raw)
	}
	return nil
}

func (b *LocalBackend) statusTasks(ctx context.Context, id string) (JobStatus, error) {
	var task taskResponse
	if err := b.v1JSON(ctx, http.MethodGet, "/v1/tasks/"+url.PathEscape(id), nil, &task); err != nil {
		return JobStatus{}, err
	}
	if task.ID != id || len(task.Files) == 0 {
		return JobStatus{}, fmt.Errorf("invalid Tasks status response for %s", id)
	}
	result := JobStatus{JobID: id}
	for _, file := range task.Files {
		item := FileResult{Name: file.Name, State: StateRunning}
		switch file.Status {
		case "completed":
			item.State = StateDone
			link, err := b.resolveLink(file.ResultURL)
			if err != nil {
				return JobStatus{}, fmt.Errorf("missing or invalid result_url for %s: %w", file.Name, err)
			}
			item.ZipURL = link
		case "failed", "canceled":
			item.State = StateFailed
		case "waiting-file", "pending", "running":
			if task.Status == "failed" || task.Status == "canceled" {
				item.State = StateFailed
				item.Error = "task " + task.Status
			}
		default:
			return JobStatus{}, fmt.Errorf("unknown task file status %q", file.Status)
		}
		if file.Error != nil {
			item.Error = file.Error.Message
		} else if item.State == StateFailed && task.Error != nil {
			item.Error = task.Error.Message
		}
		result.Files = append(result.Files, item)
	}
	result.State = result.Overall()
	return result, nil
}
