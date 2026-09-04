package cmd

import (
	"context"
	"fmt"
	"time"

	"mineru-cli/internal/backend"
)

// pollForTerminal polls be.Status(jobID) until every file reaches a
// terminal state or timeout elapses, printing a one-line progress update
// each time the overall state changes.
func pollForTerminal(ctx context.Context, be backend.Backend, jobID string, timeout, interval time.Duration) (backend.JobStatus, error) {
	deadline := time.Now().Add(timeout)
	lastState := ""

	for {
		st, err := be.Status(ctx, jobID)
		if err != nil {
			return backend.JobStatus{}, err
		}
		if st.State != lastState {
			fmt.Printf("[%s] %s\n", time.Now().Format("15:04:05"), st.State)
			lastState = st.State
		}
		if st.Terminal() {
			return st, nil
		}
		if time.Now().After(deadline) {
			return st, fmt.Errorf("timed out after %s waiting for job %s (last state: %s)", timeout, jobID, st.State)
		}

		select {
		case <-ctx.Done():
			return st, ctx.Err()
		case <-time.After(interval):
		}
	}
}
