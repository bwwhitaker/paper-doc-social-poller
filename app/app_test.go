package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAccountsFallsBackToEmbedded(t *testing.T) {
	// A path that doesn't exist on disk, as on Vercel: the embedded copy with
	// the same file name must be used.
	accounts, err := loadAccounts("/nonexistent/dir/accounts.json")
	if err != nil {
		t.Fatalf("embedded fallback failed: %v", err)
	}
	if len(accounts) == 0 {
		t.Error("embedded accounts.json has no accounts")
	}
}

func TestLoadAccountsMissingEverywhere(t *testing.T) {
	if _, err := loadAccounts("/nonexistent/dir/nope.json"); err == nil {
		t.Error("expected an error for a file that is neither on disk nor embedded")
	}
}

func TestLoadAccountsPrefersDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.json")
	if err := os.WriteFile(path, []byte(`[{"platform":"tiktok","account_id":"disk-id"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	accounts, err := loadAccounts(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 || accounts[0].AccountID != "disk-id" {
		t.Errorf("got %+v, want the on-disk list", accounts)
	}
}
