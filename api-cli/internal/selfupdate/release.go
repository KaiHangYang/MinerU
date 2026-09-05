// Package selfupdate implements `mineru-cli update`: finding the newest
// GitHub release, downloading and verifying the right platform archive, and
// swapping it in for the currently running binary (elevating via sudo if
// the install location needs it).
package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// DefaultRepo is where the released mineru-cli binaries live, as
// "owner/name". Overridden at build time via:
//
//	go build -ldflags "-X mineru-cli/internal/selfupdate.DefaultRepo=owner/name"
//
// so a build always points `update` at the repo it was actually released
// from (see .github/workflows/api-cli-release.yml), without editing source.
var DefaultRepo = "KaiHangYang/MinerU"

// tagPrefix distinguishes api-cli releases from anything else tagged in the
// same repo (see .github/workflows/api-cli-release.yml: tag = "api-cli-"+version).
const tagPrefix = "api-cli-"

// Asset is one file attached to a GitHub release.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Release is the subset of the GitHub release API response this package uses.
type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

// Version strips the release-tag prefix, e.g. "api-cli-v0.1.1" -> "v0.1.1".
func (r Release) Version() string {
	return strings.TrimPrefix(r.TagName, tagPrefix)
}

// FindAsset looks up a release asset by exact file name.
func (r Release) FindAsset(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// apiClient is deliberately short-timeout: it's only used for the small
// JSON metadata calls, never the archive download itself.
var apiClient = &http.Client{Timeout: 30 * time.Second}

// githubAPIBase is a var (not a const) so tests can point it at a mock server.
var githubAPIBase = "https://api.github.com"

func apiGet(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := apiClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("GET %s: HTTP %d: %s", url, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return json.Unmarshal(raw, out)
}

// Resolve fetches the release to install: the exact version if one is
// given (with or without a leading "v"), otherwise the newest api-cli-v*
// release in the repo (releases with non-semver tags, e.g. "-dev-<sha>"
// builds, are ignored when picking "latest").
func Resolve(ctx context.Context, repo, version string) (Release, error) {
	base := fmt.Sprintf("%s/repos/%s", githubAPIBase, repo)

	if version != "" {
		norm := version
		if !strings.HasPrefix(norm, "v") {
			norm = "v" + norm
		}
		tag := tagPrefix + norm
		var rel Release
		if err := apiGet(ctx, base+"/releases/tags/"+tag, &rel); err != nil {
			return Release{}, fmt.Errorf("fetch release %s: %w", tag, err)
		}
		return rel, nil
	}

	var all []Release
	if err := apiGet(ctx, base+"/releases?per_page=30", &all); err != nil {
		return Release{}, fmt.Errorf("list releases: %w", err)
	}

	var best Release
	var bestVer [3]int
	found := false
	for _, r := range all {
		if !strings.HasPrefix(r.TagName, tagPrefix) {
			continue
		}
		v, ok := parseSemver(strings.TrimPrefix(r.TagName, tagPrefix))
		if !ok {
			continue
		}
		if !found || semverLess(bestVer, v) {
			best, bestVer, found = r, v, true
		}
	}
	if !found {
		return Release{}, fmt.Errorf("no %sv<semver> releases found in %s", tagPrefix, repo)
	}
	return best, nil
}

// AssetName is the archive name the release workflow publishes for a given
// platform (see .github/workflows/api-cli-release.yml).
func AssetName(goos, goarch string) string {
	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("mineru-cli_%s_%s.%s", goos, goarch, ext)
}

func parseSemver(s string) ([3]int, bool) {
	s = strings.TrimPrefix(s, "v")
	parts := strings.SplitN(s, ".", 3)
	if len(parts) != 3 {
		return [3]int{}, false
	}
	var out [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}

func semverLess(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// IsNewer reports whether latest is a newer version than current. A current
// version that doesn't parse as semver (e.g. "dev", a local build) is
// always treated as older, since there's nothing sound to compare against.
func IsNewer(latest, current string) bool {
	lv, ok := parseSemver(latest)
	if !ok {
		return true
	}
	cv, ok := parseSemver(current)
	if !ok {
		return true
	}
	return semverLess(cv, lv)
}
