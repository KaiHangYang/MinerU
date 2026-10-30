package config

import (
	"testing"

	"mineru-cli/internal/backend"
)

func TestResolveTasksAPIKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)
	t.Setenv("HOME", dir)
	t.Setenv("MINERU_BACKEND", "")
	t.Setenv("MINERU_API_URL", "")
	t.Setenv("MINERU_CLOUD_TOKEN", "")
	t.Setenv("MINERU_API_KEY", "")
	if _, err := Save(FileConfig{APIURL: "https://api.example.com/prefix", APIKey: "saved-key"}); err != nil {
		t.Fatal(err)
	}
	check := func(flags Flags, want string) {
		t.Helper()
		resolved, err := Resolve(flags)
		if err != nil {
			t.Fatal(err)
		}
		local, ok := resolved.(*backend.LocalBackend)
		if !ok || local.APIKey != want || local.BaseURL != "https://api.example.com/prefix" {
			t.Fatalf("backend: %+v", resolved)
		}
	}
	check(Flags{}, "saved-key")
	t.Setenv("MINERU_API_KEY", "env-key")
	check(Flags{}, "env-key")
	check(Flags{APIKey: "flag-key"}, "flag-key")
	// A Tasks key does not select or replace the mineru.net v4 cloud token.
	resolved, err := Resolve(Flags{Backend: "cloud", Token: "cloud-token", APIKey: "tasks-key"})
	if err != nil {
		t.Fatal(err)
	}
	cloud, ok := resolved.(*backend.CloudBackend)
	if !ok || cloud.Token != "cloud-token" {
		t.Fatalf("backend: %+v", resolved)
	}
}
