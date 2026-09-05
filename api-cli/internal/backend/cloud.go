package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultCloudBaseURL is mineru.net's official 精准解析 (precision parsing) API.
// Docs: https://mineru.net/apiManage/docs
const DefaultCloudBaseURL = "https://mineru.net"

// CloudBackend talks to the official mineru.net cloud parsing API. It always
// uses the batch endpoints (file-urls/batch + extract-results/batch) since
// those are the only ones that accept locally-uploaded files, single file or
// not.
type CloudBackend struct {
	BaseURL string
	Token   string
	Client  *http.Client
}

func NewCloudBackend(baseURL, token string) *CloudBackend {
	if baseURL == "" {
		baseURL = DefaultCloudBaseURL
	}
	return &CloudBackend{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		Client:  &http.Client{Timeout: 2 * time.Minute},
	}
}

func (b *CloudBackend) Name() string { return "cloud" }

type cloudEnvelope struct {
	Code    int             `json:"code"`
	Msg     string          `json:"msg"`
	TraceID string          `json:"trace_id"`
	Data    json.RawMessage `json:"data"`
}

func (b *CloudBackend) doJSON(ctx context.Context, method, path string, reqBody any, out any) error {
	var body io.Reader
	if reqBody != nil {
		raw, err := json.Marshal(reqBody)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, b.BaseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+b.Token)
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := b.Client.Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", path, err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s failed (HTTP %d): %s", path, resp.StatusCode, string(raw))
	}

	var env cloudEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("decode %s response: %w", path, err)
	}
	if env.Code != 0 {
		return fmt.Errorf("%s rejected: code=%d msg=%s trace_id=%s", path, env.Code, env.Msg, env.TraceID)
	}
	if out != nil {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return fmt.Errorf("decode %s data: %w", path, err)
		}
	}
	return nil
}

type cloudBatchFileReq struct {
	Name       string `json:"name"`
	DataID     string `json:"data_id"`
	IsOCR      bool   `json:"is_ocr"`
	PageRanges string `json:"page_ranges,omitempty"`
}

type cloudBatchSubmitReq struct {
	Files         []cloudBatchFileReq `json:"files"`
	ModelVersion  string              `json:"model_version"`
	EnableFormula bool                `json:"enable_formula"`
	EnableTable   bool                `json:"enable_table"`
	Language      string              `json:"language"`
}

type cloudBatchSubmitResp struct {
	BatchID  string   `json:"batch_id"`
	FileURLs []string `json:"file_urls"`
}

func (b *CloudBackend) Submit(ctx context.Context, files []string, opts ParseOptions) (string, error) {
	model := opts.Model
	if model == "" {
		model = "pipeline"
	}
	lang := opts.Lang
	if lang == "" {
		lang = "ch"
	}

	reqFiles := make([]cloudBatchFileReq, 0, len(files))
	for _, path := range files {
		name := filepath.Base(path)
		reqFiles = append(reqFiles, cloudBatchFileReq{
			Name:       name,
			DataID:     name,
			IsOCR:      opts.OCR,
			PageRanges: opts.PageRange,
		})
	}

	var submitResp cloudBatchSubmitResp
	err := b.doJSON(ctx, http.MethodPost, "/api/v4/file-urls/batch", cloudBatchSubmitReq{
		Files:         reqFiles,
		ModelVersion:  model,
		EnableFormula: opts.FormulaEnable,
		EnableTable:   opts.TableEnable,
		Language:      lang,
	}, &submitResp)
	if err != nil {
		return "", err
	}
	if len(submitResp.FileURLs) != len(files) {
		return "", fmt.Errorf("expected %d upload URLs, got %d", len(files), len(submitResp.FileURLs))
	}

	for i, path := range files {
		if err := b.putFile(ctx, submitResp.FileURLs[i], path); err != nil {
			return "", fmt.Errorf("upload %s: %w", path, err)
		}
	}

	return submitResp.BatchID, nil
}

func (b *CloudBackend) putFile(ctx context.Context, uploadURL, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}

	// Signed OSS upload URLs are picky about extra/unsigned headers, so we
	// send nothing but the body, as the docs instruct.
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, f)
	if err != nil {
		return err
	}
	req.ContentLength = info.Size()

	resp, err := b.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload failed (HTTP %d): %s", resp.StatusCode, string(raw))
	}
	return nil
}

type cloudExtractResult struct {
	FileName   string `json:"file_name"`
	State      string `json:"state"`
	FullZipURL string `json:"full_zip_url"`
	DataID     string `json:"data_id"`
	ErrMsg     string `json:"err_msg"`
}

type cloudBatchStatusResp struct {
	BatchID       string               `json:"batch_id"`
	ExtractResult []cloudExtractResult `json:"extract_result"`
}

func (b *CloudBackend) Status(ctx context.Context, jobID string) (JobStatus, error) {
	var statusResp cloudBatchStatusResp
	if err := b.doJSON(ctx, http.MethodGet, "/api/v4/extract-results/batch/"+jobID, nil, &statusResp); err != nil {
		return JobStatus{}, err
	}

	files := make([]FileResult, 0, len(statusResp.ExtractResult))
	for _, r := range statusResp.ExtractResult {
		files = append(files, FileResult{
			Name:   r.FileName,
			State:  normalizeCloudState(r.State),
			ZipURL: r.FullZipURL,
			Error:  r.ErrMsg,
		})
	}

	status := JobStatus{JobID: jobID, Files: files}
	status.State = status.Overall()
	return status, nil
}

func normalizeCloudState(s string) string {
	switch s {
	case "done":
		return StateDone
	case "failed":
		return StateFailed
	default: // pending, running, converting, waiting-file, uploading
		return StateRunning
	}
}

// Download extracts each file's result zip. The cloud API always returns
// its full raw output (layout/model/content-list JSON, the original PDF,
// plus markdown and images); unless verbose is set, everything but the
// markdown and images is discarded so cloud and local output line up.
func (b *CloudBackend) Download(ctx context.Context, jobID string, status JobStatus, outDir string, verbose bool) ([]string, error) {
	var written []string
	for _, f := range status.Files {
		if f.State != StateDone {
			continue
		}
		if f.ZipURL == "" {
			return written, fmt.Errorf("file %s has no result URL", f.Name)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.ZipURL, nil)
		if err != nil {
			return written, err
		}
		resp, err := b.Client.Do(req)
		if err != nil {
			return written, fmt.Errorf("download %s: %w", f.Name, err)
		}
		if resp.StatusCode >= 400 {
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return written, fmt.Errorf("download %s failed (HTTP %d): %s", f.Name, resp.StatusCode, string(raw))
		}

		tmp, err := os.CreateTemp("", "mineru-cli-*.zip")
		if err != nil {
			resp.Body.Close()
			return written, err
		}
		_, copyErr := io.Copy(tmp, resp.Body)
		resp.Body.Close()
		tmp.Close()
		if copyErr != nil {
			os.Remove(tmp.Name())
			return written, copyErr
		}

		stem := strings.TrimSuffix(f.Name, filepath.Ext(f.Name))
		extracted, err := extractZip(tmp.Name(), filepath.Join(outDir, stem))
		os.Remove(tmp.Name())
		if err != nil {
			return written, fmt.Errorf("extract %s: %w", f.Name, err)
		}
		if !verbose {
			extracted = pruneToEssentials(extracted)
		}
		written = append(written, extracted...)
	}
	return written, nil
}

// pruneToEssentials deletes every extracted file except markdown and images,
// returning the paths that remain. It's the cloud API's raw zip trimmed down
// to match what the local backend returns by default.
func pruneToEssentials(paths []string) []string {
	kept := make([]string, 0, len(paths))
	for _, p := range paths {
		isMarkdown := strings.EqualFold(filepath.Ext(p), ".md")
		isImage := false
		for _, part := range strings.Split(filepath.ToSlash(filepath.Dir(p)), "/") {
			if part == "images" {
				isImage = true
				break
			}
		}
		if isMarkdown || isImage {
			kept = append(kept, p)
			continue
		}
		_ = os.Remove(p)
	}
	return kept
}
