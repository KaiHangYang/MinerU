package selfupdate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseSemverAndLess(t *testing.T) {
	cases := []struct {
		s  string
		ok bool
	}{
		{"v1.2.3", true},
		{"1.2.3", true},
		{"dev", false},
		{"dev-5627012", false},
		{"1.2", false},
	}
	for _, c := range cases {
		if _, ok := parseSemver(c.s); ok != c.ok {
			t.Errorf("parseSemver(%q) ok = %v, want %v", c.s, ok, c.ok)
		}
	}

	if !semverLess([3]int{0, 1, 0}, [3]int{0, 1, 1}) {
		t.Error("0.1.0 should be less than 0.1.1")
	}
	if semverLess([3]int{1, 0, 0}, [3]int{0, 9, 9}) {
		t.Error("1.0.0 should not be less than 0.9.9")
	}
}

func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v0.1.1", "v0.1.0", true},
		{"v0.1.0", "v0.1.0", false},
		{"v0.1.0", "v0.1.1", false},
		{"v0.1.0", "dev", true},
		{"v0.1.0", "dev-5627012", true},
	}
	for _, c := range cases {
		if got := IsNewer(c.latest, c.current); got != c.want {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}

func TestAssetName(t *testing.T) {
	if got := AssetName("linux", "amd64"); got != "mineru-cli_linux_amd64.tar.gz" {
		t.Errorf("AssetName(linux, amd64) = %q", got)
	}
	if got := AssetName("windows", "arm64"); got != "mineru-cli_windows_arm64.zip" {
		t.Errorf("AssetName(windows, arm64) = %q", got)
	}
}

func TestResolveLatestPicksHighestSemverAndIgnoresDevTags(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/mineru/releases", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"tag_name":"api-cli-v0.1.1","assets":[{"name":"mineru-cli_linux_amd64.tar.gz","browser_download_url":"x"}]},
			{"tag_name":"api-cli-v0.1.0","assets":[]},
			{"tag_name":"api-cli-dev-5627012","assets":[]},
			{"tag_name":"unrelated-v9.9.9","assets":[]}
		]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	rel, err := resolveAgainst(srv.URL, "acme/mineru", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if rel.TagName != "api-cli-v0.1.1" {
		t.Fatalf("TagName = %q, want api-cli-v0.1.1", rel.TagName)
	}
	if rel.Version() != "v0.1.1" {
		t.Fatalf("Version() = %q, want v0.1.1", rel.Version())
	}
}

func TestResolveSpecificVersionNormalizesVPrefix(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/mineru/releases/tags/api-cli-v0.1.0", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"api-cli-v0.1.0","assets":[]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	rel, err := resolveAgainst(srv.URL, "acme/mineru", "0.1.0")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if rel.TagName != "api-cli-v0.1.0" {
		t.Fatalf("TagName = %q, want api-cli-v0.1.0", rel.TagName)
	}
}

// resolveAgainst is Resolve but pointed at a test server instead of
// api.github.com.
func resolveAgainst(apiBase, repo, version string) (Release, error) {
	orig := githubAPIBase
	githubAPIBase = apiBase
	defer func() { githubAPIBase = orig }()
	return Resolve(context.Background(), repo, version)
}
