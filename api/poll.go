// Package handler is the Vercel serverless function that runs one poll.
// Vercel serves api/poll.go at /api/poll and calls the exported Handler.
package handler

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/bwwhitaker/paper-doc-social-poller/app"
)

// Handler requires POST with "Authorization: Bearer $POLL_SECRET". Optional
// query params: dry_run=1, account=platform:account_id.
func Handler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	secret := os.Getenv("POLL_SECRET")
	if secret == "" {
		// Fail closed: never run unauthenticated just because the env is unset.
		slog.Error("POLL_SECRET is not set")
		http.Error(w, "not configured", http.StatusInternalServerError)
		return
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	// ConstantTimeCompare avoids leaking the secret through response timing.
	if subtle.ConstantTimeCompare([]byte(got), []byte(secret)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	accountsFile := os.Getenv("ACCOUNTS_FILE")
	if accountsFile == "" {
		accountsFile = "accounts.json"
	}
	q := r.URL.Query()
	opts := app.Options{
		DryRun:       q.Get("dry_run") == "1" || q.Get("dry_run") == "true",
		Only:         q.Get("account"),
		AccountsFile: accountsFile,
	}

	if err := app.Run(r.Context(), opts); err != nil {
		// Details go to the logs, not the response: the caller is pg_cron.
		slog.Error("poll failed", "err", err)
		http.Error(w, "poll failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}
