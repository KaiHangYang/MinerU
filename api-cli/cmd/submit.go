package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"mineru-cli/internal/config"
)

var submitOpts parseOptFlags

var submitCmd = &cobra.Command{
	Use:   "submit <file>...",
	Short: "Upload files and start a parse job, without waiting for it to finish",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		be, err := config.Resolve(flags)
		if err != nil {
			return err
		}

		jobID, err := be.Submit(cmd.Context(), args, submitOpts.toParseOptions())
		if err != nil {
			return fmt.Errorf("submit: %w", err)
		}

		fmt.Printf("backend: %s\n", be.Name())
		fmt.Printf("job id:  %s\n", jobID)
		fmt.Printf("\nCheck it later with:\n  mineru-cli status %s\n  mineru-cli result %s -o <output-dir>\n", jobID, jobID)
		return nil
	},
}

func init() {
	addParseOptFlags(submitCmd, &submitOpts)
	rootCmd.AddCommand(submitCmd)
}
