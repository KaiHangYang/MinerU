package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"mineru-cli/internal/config"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage the saved default backend (run `mineru-cli config show` to see where it's stored)",
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print the saved config",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		if path, err := config.Path(); err == nil {
			fmt.Printf("path:        %s\n", path)
		}
		fmt.Printf("backend:     %s\n", orNone(cfg.Backend))
		fmt.Printf("api_url:     %s\n", orNone(cfg.APIURL))
		fmt.Printf("cloud_url:   %s\n", orNone(cfg.CloudURL))
		if cfg.CloudToken != "" {
			fmt.Println("cloud_token: (set)")
		} else {
			fmt.Println("cloud_token: (none)")
		}
		return nil
	},
}

var (
	setBackend    string
	setAPIURL     string
	setCloudToken string
	setCloudURL   string
)

var configSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Save default backend settings so you don't have to pass flags every time",
	Example: `  mineru-cli config set --backend local --api-url http://192.168.1.10:38000
  mineru-cli config set --backend cloud --token <your-mineru.net-token>`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		if setBackend != "" {
			cfg.Backend = setBackend
		}
		if setAPIURL != "" {
			cfg.APIURL = setAPIURL
		}
		if setCloudToken != "" {
			cfg.CloudToken = setCloudToken
		}
		if setCloudURL != "" {
			cfg.CloudURL = setCloudURL
		}
		path, err := config.Save(cfg)
		if err != nil {
			return err
		}
		fmt.Println("saved to", path)
		return nil
	},
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

func init() {
	configSetCmd.Flags().StringVar(&setBackend, "backend", "", `"local" or "cloud"`)
	configSetCmd.Flags().StringVar(&setAPIURL, "api-url", "", "self-hosted mineru-api base URL")
	configSetCmd.Flags().StringVar(&setCloudToken, "token", "", "mineru.net API token")
	configSetCmd.Flags().StringVar(&setCloudURL, "cloud-url", "", "override the mineru.net API base URL")

	configCmd.AddCommand(configShowCmd, configSetCmd)
	rootCmd.AddCommand(configCmd)
}
