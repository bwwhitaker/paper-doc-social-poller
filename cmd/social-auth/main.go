// Command social-auth performs the one-time authorization for an account and
// stores its first tokens. Run it locally, not in CI.
//
//	go run ./cmd/social-auth tiktok
//
// For TikTok, TIKTOK_REDIRECT_URI must exactly match a redirect URI registered
// in the developer portal. The page it points to doesn't need to work: after
// the account owner approves, TikTok redirects there with ?code=... in the
// URL, and you paste that code back here.
package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/bwwhitaker/paper-doc-social-poller/internal/config"
	"github.com/bwwhitaker/paper-doc-social-poller/internal/platform"
	"github.com/bwwhitaker/paper-doc-social-poller/internal/store"
	"github.com/bwwhitaker/paper-doc-social-poller/internal/tiktok"
)

func main() {
	if err := run(); err != nil {
		slog.Error("auth failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: social-auth <platform>   (supported: tiktok)")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	switch os.Args[1] {
	case "tiktok":
		return authTikTok(cfg)
	default:
		return fmt.Errorf("unsupported platform %q (supported: tiktok)", os.Args[1])
	}
}

func authTikTok(cfg config.Config) error {
	if err := cfg.RequireTikTok(); err != nil {
		return err
	}
	redirect := os.Getenv("TIKTOK_REDIRECT_URI")
	if redirect == "" {
		return fmt.Errorf("missing required env var TIKTOK_REDIRECT_URI")
	}

	q := url.Values{
		"client_key":    {cfg.TikTokClientKey},
		"scope":         {"user.info.basic,user.info.stats,video.list"},
		"response_type": {"code"},
		"redirect_uri":  {redirect},
		"state":         {"paper-doc-poller"},
	}
	fmt.Println("1. Log in to the TikTok account you want to track, then open this URL and approve:")
	fmt.Println()
	fmt.Println("   https://www.tiktok.com/v2/auth/authorize/?" + q.Encode())
	fmt.Println()
	fmt.Println("2. Copy the full URL you land on (or just the code= value) and paste it here:")
	fmt.Print("> ")

	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return err
	}
	code := extractCode(strings.TrimSpace(line))

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	tok, openID, err := tiktok.NewClient(cfg.TikTokClientKey, cfg.TikTokClientSecret).
		ExchangeCode(ctx, code, redirect)
	if err != nil {
		return err
	}
	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close(context.Background())

	acct := platform.Account{Platform: "tiktok", AccountID: openID}
	if err := db.SaveToken(ctx, acct, tok); err != nil {
		return err
	}
	fmt.Println("Saved token. Add this to accounts.json to start polling it:")
	fmt.Printf("  {\"platform\": \"tiktok\", \"account_id\": %q, \"label\": \"...\"}\n", openID)
	return nil
}

// extractCode accepts either a full redirect URL or a bare code. url.Parse
// already URL-decodes the query, which TikTok requires for the code.
func extractCode(s string) string {
	if u, err := url.Parse(s); err == nil {
		if c := u.Query().Get("code"); c != "" {
			return c
		}
	}
	return s
}
