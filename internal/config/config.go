// Package config loads settings from environment variables.
package config

import (
	"fmt"
	"os"
)

type Config struct {
	DatabaseURL        string
	TikTokClientKey    string
	TikTokClientSecret string
}

// Load reads required environment variables. Go functions return errors as
// ordinary values instead of throwing; the caller decides what to do.
func Load() (Config, error) {
	cfg := Config{
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		TikTokClientKey:    os.Getenv("TIKTOK_CLIENT_KEY"),
		TikTokClientSecret: os.Getenv("TIKTOK_CLIENT_SECRET"),
	}
	for name, val := range map[string]string{
		"DATABASE_URL":         cfg.DatabaseURL,
		"TIKTOK_CLIENT_KEY":    cfg.TikTokClientKey,
		"TIKTOK_CLIENT_SECRET": cfg.TikTokClientSecret,
	} {
		if val == "" {
			return Config{}, fmt.Errorf("missing required env var %s", name)
		}
	}
	return cfg, nil
}
