// Package backend defines the common abstraction that lets the CLI talk to
// either a self-hosted mineru-api server or the official mineru.net cloud API
// through the same command implementations.
package backend

import "context"

// Job states, normalized across backends.
const (
	StatePending = "pending"
	StateRunning = "running"
	StateDone    = "done"
	StateFailed  = "failed"
	// StatePartial is an overall-job state (never a per-file state): some
	// files in a multi-file job succeeded and others failed.
	StatePartial = "partial"
)

// ParseOptions carries the parsing knobs shared by both backends. Not every
// field is honored by every backend; each implementation maps what it can.
type ParseOptions struct {
	Lang          string // e.g. "ch", "en"
	Model         string // local: pipeline/vlm-engine/hybrid-engine; cloud: pipeline/vlm/MinerU-HTML
	OCR           bool
	FormulaEnable bool
	TableEnable   bool
	PageRange     string // cloud only, e.g. "1-10"
}

// FileResult is the per-file outcome of a submitted job.
type FileResult struct {
	Name   string
	State  string
	ZipURL string // set when the backend hands back a URL to fetch instead of streaming bytes
	Error  string
}

// JobStatus is the normalized status of a submitted job (one or more files).
type JobStatus struct {
	JobID string
	State string
	Files []FileResult
}

// Terminal reports whether every file in the job reached a terminal state
// (done or failed), i.e. whether polling should stop.
func (s JobStatus) Terminal() bool {
	if len(s.Files) == 0 {
		return false
	}
	for _, f := range s.Files {
		if f.State != StateDone && f.State != StateFailed {
			return false
		}
	}
	return true
}

// Overall folds the per-file states into a single job-level state:
// running while anything is still in flight, done/failed when every file
// agrees, partial when the job finished with a mix of both.
func (s JobStatus) Overall() string {
	if !s.Terminal() {
		return StateRunning
	}
	allDone, allFailed := true, true
	for _, f := range s.Files {
		if f.State != StateDone {
			allDone = false
		}
		if f.State != StateFailed {
			allFailed = false
		}
	}
	switch {
	case allDone:
		return StateDone
	case allFailed:
		return StateFailed
	default:
		return StatePartial
	}
}

// Backend is implemented once per API flavor (local mineru-api, mineru.net cloud).
type Backend interface {
	// Name identifies the backend for CLI output ("local" or "cloud").
	Name() string

	// Submit uploads the given local files and starts a parse job, returning
	// an opaque job id that Status/Download accept.
	Submit(ctx context.Context, files []string, opts ParseOptions) (jobID string, err error)

	// Status fetches the current state of a previously submitted job.
	Status(ctx context.Context, jobID string) (JobStatus, error)

	// Download fetches the finished results into outDir and returns the
	// paths it wrote (extracted files, not the intermediate zips).
	Download(ctx context.Context, jobID string, status JobStatus, outDir string) ([]string, error)
}
