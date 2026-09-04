// Package config resolves which backend (self-hosted mineru-api, or the
// official mineru.net cloud API) a command should use, and with what
// credentials. Resolution order is: CLI flags > environment variables >
// the optional config file > built-in defaults.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"mineru-cli/internal/backend"
)

// FileConfig is the on-disk shape of ~/.config/mineru-cli/config.json,
// written by `mineru-cli config set` (see cmd/config.go).
type FileConfig struct {
	Backend    string `json:"backend,omitempty"` // "local" or "cloud"
	APIURL     string `json:"api_url,omitempty"`
	CloudToken string `json:"cloud_token,omitempty"`
	CloudURL   string `json:"cloud_url,omitempty"`
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "mineru-cli", "config.json"), nil
}

// Load reads the config file, returning a zero-value FileConfig (not an
// error) if it doesn't exist yet.
func Load() (FileConfig, error) {
	path, err := configPath()
	if err != nil {
		return FileConfig{}, err
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return FileConfig{}, nil
	}
	if err != nil {
		return FileConfig{}, err
	}
	var cfg FileConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return FileConfig{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

// Save writes cfg to the config file, creating its directory if needed.
func Save(cfg FileConfig) (string, error) {
	path, err := configPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// Flags carries the raw --backend/--api-url/--token/--cloud-url flag values
// as passed on the command line; empty means "not set".
type Flags struct {
	Backend  string
	APIURL   string
	Token    string
	CloudURL string
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// Resolve merges flags, environment variables, and the config file (in that
// priority order) into a ready-to-use Backend.
func Resolve(flags Flags) (backend.Backend, error) {
	file, err := Load()
	if err != nil {
		return nil, err
	}

	apiURL := firstNonEmpty(flags.APIURL, os.Getenv("MINERU_API_URL"), file.APIURL)
	token := firstNonEmpty(flags.Token, os.Getenv("MINERU_CLOUD_TOKEN"), file.CloudToken)
	cloudURL := firstNonEmpty(flags.CloudURL, os.Getenv("MINERU_CLOUD_URL"), file.CloudURL, backend.DefaultCloudBaseURL)
	which := firstNonEmpty(flags.Backend, os.Getenv("MINERU_BACKEND"), file.Backend)

	switch which {
	case "local":
		if apiURL == "" {
			return nil, fmt.Errorf("backend=local but no --api-url given (also checked MINERU_API_URL and the config file)")
		}
		return backend.NewLocalBackend(apiURL), nil
	case "cloud":
		if token == "" {
			return nil, fmt.Errorf("backend=cloud but no --token given (also checked MINERU_CLOUD_TOKEN and the config file); get one at https://mineru.net/apiManage/docs")
		}
		return backend.NewCloudBackend(cloudURL, token), nil
	case "":
		// Not explicitly chosen: infer from whichever credential was given.
		switch {
		case apiURL != "" && token != "":
			return nil, fmt.Errorf("both --api-url and --token are set; pass --backend local or --backend cloud to disambiguate")
		case apiURL != "":
			return backend.NewLocalBackend(apiURL), nil
		case token != "":
			return backend.NewCloudBackend(cloudURL, token), nil
		default:
			return nil, fmt.Errorf("no backend configured: pass --api-url (self-hosted mineru-api) or --token (mineru.net cloud API), or run `mineru-cli config set`")
		}
	default:
		return nil, fmt.Errorf("unknown backend %q: must be \"local\" or \"cloud\"", which)
	}
}
