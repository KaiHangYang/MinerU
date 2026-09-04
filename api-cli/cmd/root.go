package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"mineru-cli/internal/config"
)

var flags config.Flags

var rootCmd = &cobra.Command{
	Use:   "mineru-cli",
	Short: "Upload documents to MinerU and download parsed results, over HTTP",
	Long: `mineru-cli talks to either a self-hosted mineru-api server or the official
mineru.net cloud API, using the same commands either way.

Pick a backend with --api-url (self-hosted) or --token (mineru.net cloud),
or store defaults once with "mineru-cli config set" and omit the flags after that.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func init() {
	pf := rootCmd.PersistentFlags()
	pf.StringVar(&flags.Backend, "backend", "", `force "local" or "cloud" (default: inferred from --api-url/--token)`)
	pf.StringVar(&flags.APIURL, "api-url", "", "self-hosted mineru-api base URL, e.g. http://192.168.1.10:38000")
	pf.StringVar(&flags.Token, "token", "", "mineru.net API token (Bearer), see https://mineru.net/apiManage/docs")
	pf.StringVar(&flags.CloudURL, "cloud-url", "", "override the mineru.net API base URL (default https://mineru.net)")
}
