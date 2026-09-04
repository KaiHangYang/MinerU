package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"mineru-cli/internal/config"
)

var (
	parseOpts    parseOptFlags
	parseOutDir  string
	parseTimeout time.Duration
)

var parseCmd = &cobra.Command{
	Use:   "parse <file>...",
	Short: "Upload, wait for parsing, and download results, in one step",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		be, err := config.Resolve(flags)
		if err != nil {
			return err
		}

		fmt.Printf("backend: %s\n", be.Name())
		jobID, err := be.Submit(cmd.Context(), args, parseOpts.toParseOptions())
		if err != nil {
			return fmt.Errorf("submit: %w", err)
		}
		fmt.Printf("job id:  %s\n", jobID)

		st, err := pollForTerminal(cmd.Context(), be, jobID, parseTimeout, 3*time.Second)
		if err != nil {
			return err
		}
		if st.State == "failed" {
			return fmt.Errorf("job %s failed (run `mineru-cli status %s` for details)", jobID, jobID)
		}

		written, err := be.Download(cmd.Context(), jobID, st, parseOutDir)
		if err != nil {
			return fmt.Errorf("download: %w", err)
		}

		fmt.Printf("wrote %d file(s) to %s\n", len(written), parseOutDir)
		if st.State == "partial" {
			fmt.Println("note: some files in this job failed; only the successful ones were downloaded (see `mineru-cli status`)")
		}
		return nil
	},
}

func init() {
	addParseOptFlags(parseCmd, &parseOpts)
	parseCmd.Flags().StringVarP(&parseOutDir, "output", "o", "./output", "local directory to write results into")
	parseCmd.Flags().DurationVar(&parseTimeout, "timeout", 30*time.Minute, "max time to wait for parsing to finish")
	rootCmd.AddCommand(parseCmd)
}
