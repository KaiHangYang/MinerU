package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"mineru-cli/internal/config"
)

var (
	resultOutDir     string
	resultWait       bool
	resultTimeout    time.Duration
	resultVerbose    bool
	resultSplitLevel int
)

var resultCmd = &cobra.Command{
	Use:   "result <job-id>",
	Short: "Download a finished job's results into a local directory",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateSplitLevel(resultSplitLevel); err != nil {
			return err
		}

		jobID := args[0]
		be, err := config.Resolve(flags)
		if err != nil {
			return err
		}

		var st, statusErr = be.Status(cmd.Context(), jobID)
		if statusErr != nil {
			return fmt.Errorf("status: %w", statusErr)
		}
		if !st.Terminal() {
			if !resultWait {
				return fmt.Errorf("job %s is still %s; re-run with --wait to block until it finishes", jobID, st.State)
			}
			st, err = pollForTerminal(cmd.Context(), be, jobID, resultTimeout, 3*time.Second)
			if err != nil {
				return err
			}
		}

		if st.State == "failed" {
			return fmt.Errorf("job %s failed", jobID)
		}

		written, err := be.Download(cmd.Context(), jobID, st, resultOutDir, resultVerbose)
		if err != nil {
			return fmt.Errorf("download: %w", err)
		}

		written, err = applySplitLevel(written, resultSplitLevel)
		if err != nil {
			return fmt.Errorf("split markdown: %w", err)
		}

		fmt.Printf("wrote %d file(s) to %s\n", len(written), resultOutDir)
		if st.State == "partial" {
			fmt.Println("note: some files in this job failed; only the successful ones were downloaded (see `mineru-cli status`)")
		}
		return nil
	},
}

func init() {
	resultCmd.Flags().StringVarP(&resultOutDir, "output", "o", "./output", "local directory to write results into")
	resultCmd.Flags().BoolVar(&resultWait, "wait", false, "block until the job finishes instead of failing if it's still running")
	resultCmd.Flags().DurationVar(&resultTimeout, "timeout", 30*time.Minute, "max time to wait with --wait")
	resultCmd.Flags().BoolVar(&resultVerbose, "verbose", false, "keep the full raw output (layout/model/content-list JSON, original file) instead of just markdown + images (cloud backend only; local already decided this at submit time)")
	resultCmd.Flags().IntVar(&resultSplitLevel, "split-level", 0, "split the markdown output by heading level: 0 = one file (default), 1 = split on \"#\" headings, 2 = split on \"##\" headings")
	rootCmd.AddCommand(resultCmd)
}
