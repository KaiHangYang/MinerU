package cmd

// Version is overridden at build time via:
//
//	go build -ldflags "-X mineru-cli/cmd.Version=v1.2.3"
var Version = "dev"

func init() {
	rootCmd.Version = Version
}
