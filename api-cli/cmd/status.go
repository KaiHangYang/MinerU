package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"mineru-cli/internal/config"
)

var statusCmd = &cobra.Command{
	Use:   "status <job-id>",
	Short: "Check the status of a previously submitted job",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		be, err := config.Resolve(flags)
		if err != nil {
			return err
		}

		st, err := be.Status(cmd.Context(), args[0])
		if err != nil {
			return fmt.Errorf("status: %w", err)
		}

		fmt.Printf("job:     %s\n", st.JobID)
		fmt.Printf("overall: %s\n", st.State)
		for _, f := range st.Files {
			line := fmt.Sprintf("  - %-30s %s", f.Name, f.State)
			if f.Error != "" {
				line += "  (" + f.Error + ")"
			}
			fmt.Println(line)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
