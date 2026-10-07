// Package config loads settings: secrets from environment variables, and the
// list of accounts to poll from accounts.json.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/bwwhitaker/paper-doc-social-poller/internal/platform"
)

type Config struct {
	DatabaseURL string

	// Per-platform credentials. Optional here; checked only when an account
	// for that platform is actually being polled (see RequireTikTok).
	TikTokClientKey    string
	TikTokClientSecret string
}

// Load reads environment variables. Go functions return errors as ordinary
// values instead of throwing; the caller decides what to do.
func Load() (Config, error) {
	cfg := Config{
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		TikTokClientKey:    os.Getenv("TIKTOK_CLIENT_KEY"),
		TikTokClientSecret: os.Getenv("TIKTOK_CLIENT_SECRET"),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("missing required env var DATABASE_URL")
	}
	return cfg, nil
}

func (c Config) RequireTikTok() error {
	if c.TikTokClientKey == "" || c.TikTokClientSecret == "" {
		return errors.New("TIKTOK_CLIENT_KEY and TIKTOK_CLIENT_SECRET are required for tiktok accounts")
	}
	return nil
}

// LoadAccounts reads and validates the list of accounts to poll from a file.
func LoadAccounts(path string) ([]platform.Account, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseAccounts(raw, path)
}

// ParseAccounts validates an accounts list; source is only used in errors.
func ParseAccounts(raw []byte, source string) ([]platform.Account, error) {
	var accounts []platform.Account
	if err := json.Unmarshal(raw, &accounts); err != nil {
		return nil, fmt.Errorf("parse %s: %w", source, err)
	}
	seen := map[string]bool{}
	for i, a := range accounts {
		if a.Platform == "" || a.AccountID == "" {
			return nil, fmt.Errorf("%s: entry %d needs platform and account_id", source, i)
		}
		if seen[a.Key()] {
			return nil, fmt.Errorf("%s: duplicate account %s", source, a.Key())
		}
		seen[a.Key()] = true
	}
	return accounts, nil
}
