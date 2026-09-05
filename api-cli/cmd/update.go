package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"mineru-cli/internal/selfupdate"
)

var (
	updateRepo    string
	updateVersion string
	updateOutput  string
	updateCheck   bool
	updateForce   bool
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Download and install the latest mineru-cli release",
	Long: `Fetches the newest mineru-cli release from GitHub, verifies its checksum,
and installs it in place of the currently running binary.

If that location isn't writable by the current user (e.g. /usr/local/bin),
it re-runs the install step with sudo, which will prompt for a password on
this terminal.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		rel, err := selfupdate.Resolve(ctx, updateRepo, updateVersion)
		if err != nil {
			return err
		}
		version := rel.Version()

		fmt.Printf("current version: %s\n", Version)
		fmt.Printf("latest version:  %s\n", version)

		// An explicit --version always installs; otherwise skip if we're
		// already there (--force overrides that too).
		explicit := updateVersion != ""
		if !explicit && !updateForce && !selfupdate.IsNewer(version, Version) {
			fmt.Println("already up to date")
			return nil
		}
		if updateCheck {
			fmt.Println("update available: run `mineru-cli update` to install it")
			return nil
		}

		assetName := selfupdate.AssetName(runtime.GOOS, runtime.GOARCH)
		asset, ok := rel.FindAsset(assetName)
		if !ok {
			return fmt.Errorf("release %s has no asset for %s/%s (looked for %q)", rel.TagName, runtime.GOOS, runtime.GOARCH, assetName)
		}

		fmt.Printf("downloading %s...\n", asset.Name)
		archive, err := selfupdate.Download(ctx, asset.BrowserDownloadURL)
		if err != nil {
			return fmt.Errorf("download %s: %w", asset.Name, err)
		}

		if checksums, ok := rel.FindAsset("checksums.txt"); ok {
			sums, err := selfupdate.Download(ctx, checksums.BrowserDownloadURL)
			if err != nil {
				return fmt.Errorf("download checksums.txt: %w", err)
			}
			if err := selfupdate.VerifyChecksum(sums, asset.Name, archive); err != nil {
				return fmt.Errorf("verify %s: %w", asset.Name, err)
			}
			fmt.Println("checksum ok")
		}

		binary, err := selfupdate.ExtractBinary(archive, asset.Name)
		if err != nil {
			return fmt.Errorf("extract %s: %w", asset.Name, err)
		}

		target := updateOutput
		if target == "" {
			exe, err := os.Executable()
			if err != nil {
				return fmt.Errorf("locate current executable: %w", err)
			}
			if resolved, err := filepath.EvalSymlinks(exe); err == nil {
				target = resolved
			} else {
				target = exe
			}
		}

		fmt.Printf("installing to %s...\n", target)
		if err := selfupdate.ReplaceExecutable(target, binary); err != nil {
			return fmt.Errorf("install: %w", err)
		}

		fmt.Printf("updated to %s\n", version)
		return nil
	},
}

func init() {
	updateCmd.Flags().StringVar(&updateRepo, "repo", selfupdate.DefaultRepo, "GitHub repo to fetch releases from, as owner/name")
	updateCmd.Flags().StringVar(&updateVersion, "version", "", "install a specific version, e.g. v0.1.0 (default: latest)")
	updateCmd.Flags().StringVar(&updateOutput, "output", "", "path to install to (default: the currently running executable)")
	updateCmd.Flags().BoolVar(&updateCheck, "check", false, "only check whether a newer version is available, don't install it")
	updateCmd.Flags().BoolVar(&updateForce, "force", false, "reinstall even if already up to date")
	rootCmd.AddCommand(updateCmd)
}
