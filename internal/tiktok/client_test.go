package tiktok

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bwwhitaker/paper-doc-social-poller/internal/platform"
)

// newTestClient points a Client at a fake TikTok server. httptest.NewServer
// starts a real HTTP server on localhost, so the whole request path is
// exercised without touching the network. t.Cleanup closes it when the test
// ends.
func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := NewClient("key", "secret")
	c.baseURL = srv.URL
	return c
}

func writeJSON(t *testing.T, w http.ResponseWriter, v string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte(v)); err != nil {
		t.Errorf("write response: %v", err)
	}
}

func TestExchangeCode(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v2/oauth/token/" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("content type = %q", ct)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		want := map[string]string{
			"client_key":    "key",
			"client_secret": "secret",
			"grant_type":    "authorization_code",
			"code":          "abc 123", // round-trips through form encoding
			"redirect_uri":  "https://example.com/cb",
		}
		for k, v := range want {
			if got := r.PostForm.Get(k); got != v {
				t.Errorf("form %s = %q, want %q", k, got, v)
			}
		}
		writeJSON(t, w, `{"access_token":"at","expires_in":86400,"open_id":"oid",
			"refresh_token":"rt","refresh_expires_in":31536000}`)
	})

	before := time.Now()
	tok, openID, err := c.ExchangeCode(context.Background(), "abc 123", "https://example.com/cb")
	if err != nil {
		t.Fatal(err)
	}
	if openID != "oid" || tok.AccessToken != "at" || tok.RefreshToken != "rt" {
		t.Errorf("got tok=%+v openID=%q", tok, openID)
	}
	assertExpiry(t, "access", tok.AccessTokenExpiresAt, before, 86400*time.Second)
	assertExpiry(t, "refresh", tok.RefreshTokenExpiresAt, before, 31536000*time.Second)
}

// assertExpiry checks an expiry is `ttl` after `from`, allowing for the time
// the test itself takes.
func assertExpiry(t *testing.T, name string, got, from time.Time, ttl time.Duration) {
	t.Helper()
	lo, hi := from.Add(ttl), time.Now().Add(ttl)
	if got.Before(lo) || got.After(hi) {
		t.Errorf("%s expiry %s not in [%s, %s]", name, got, lo, hi)
	}
}

func TestRefreshReturnsRotatedToken(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.PostForm.Get("grant_type") != "refresh_token" || r.PostForm.Get("refresh_token") != "old-rt" {
			t.Errorf("unexpected form: %v", r.PostForm)
		}
		writeJSON(t, w, `{"access_token":"new-at","expires_in":60,"open_id":"oid",
			"refresh_token":"new-rt","refresh_expires_in":120}`)
	})

	tok, err := c.Refresh(context.Background(), "old-rt")
	if err != nil {
		t.Fatal(err)
	}
	// The poller depends on getting the NEW refresh token back to persist it.
	if tok.AccessToken != "new-at" || tok.RefreshToken != "new-rt" {
		t.Errorf("got %+v", tok)
	}
}

func TestTokenEndpointError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(t, w, `{"error":"invalid_grant","error_description":"Refresh token is expired"}`)
	})

	_, err := c.Refresh(context.Background(), "rt")
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"invalid_grant", "Refresh token is expired"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

func TestUserInfo(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v2/user/info/" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q", got)
		}
		fields := r.URL.Query().Get("fields")
		for _, f := range []string{"follower_count", "following_count", "likes_count", "video_count"} {
			if !strings.Contains(fields, f) {
				t.Errorf("fields %q missing %s", fields, f)
			}
		}
		writeJSON(t, w, `{"data":{"user":{"open_id":"oid","display_name":"Doc",
			"follower_count":1200,"following_count":30,"likes_count":9000,"video_count":42}},
			"error":{"code":"ok","message":"","log_id":"l1"}}`)
	})

	u, err := c.UserInfo(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	want := UserStats{OpenID: "oid", DisplayName: "Doc", FollowerCount: 1200,
		FollowingCount: 30, LikesCount: 9000, VideoCount: 42}
	if u != want {
		t.Errorf("got %+v, want %+v", u, want)
	}
}

func TestUserInfoAPIError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		writeJSON(t, w, `{"data":{},"error":{"code":"access_token_invalid",
			"message":"The access token is invalid","log_id":"log-9"}}`)
	})

	_, err := c.UserInfo(context.Background(), "bad")
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"access_token_invalid", "log-9"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

func TestListVideosPaginates(t *testing.T) {
	var cursors []int64 // cursor sent on each request, in order
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v2/video/list/" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q", got)
		}
		if f := r.URL.Query().Get("fields"); !strings.Contains(f, "view_count") {
			t.Errorf("fields %q missing view_count", f)
		}

		var body struct {
			Cursor   int64 `json:"cursor"`
			MaxCount int   `json:"max_count"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.MaxCount != 20 {
			t.Errorf("max_count = %d, want 20", body.MaxCount)
		}
		cursors = append(cursors, body.Cursor)

		switch body.Cursor {
		case 0:
			writeJSON(t, w, `{"data":{"videos":[{"id":"v1","view_count":10},{"id":"v2","view_count":20}],
				"cursor":1700000000000,"has_more":true},"error":{"code":"ok"}}`)
		case 1700000000000:
			writeJSON(t, w, `{"data":{"videos":[{"id":"v3","view_count":30}],
				"cursor":1600000000000,"has_more":false},"error":{"code":"ok"}}`)
		default:
			t.Errorf("unexpected cursor %d", body.Cursor)
		}
	})

	videos, err := c.ListVideos(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	if len(videos) != 3 || videos[0].ID != "v1" || videos[2].ID != "v3" || videos[2].ViewCount != 30 {
		t.Errorf("got %+v", videos)
	}
	if len(cursors) != 2 || cursors[0] != 0 || cursors[1] != 1700000000000 {
		t.Errorf("cursors sent = %v", cursors)
	}
}

func TestListVideosAPIErrorOnLaterPage(t *testing.T) {
	calls := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			writeJSON(t, w, `{"data":{"videos":[{"id":"v1"}],"cursor":5,"has_more":true},"error":{"code":"ok"}}`)
			return
		}
		writeJSON(t, w, `{"data":{},"error":{"code":"rate_limit_exceeded","message":"slow down","log_id":"l2"}}`)
	})

	videos, err := c.ListVideos(context.Background(), "tok")
	if err == nil || !strings.Contains(err.Error(), "rate_limit_exceeded") {
		t.Fatalf("err = %v, want rate_limit_exceeded", err)
	}
	// A partial page must not be returned as if it were complete: the poller
	// would then store an incomplete snapshot.
	if videos != nil {
		t.Errorf("got %d videos alongside the error, want none", len(videos))
	}
}

func TestUndecodableBody(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>Bad Gateway</html>"))
	})

	_, err := c.UserInfo(context.Background(), "tok")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "502") || !strings.Contains(err.Error(), "Bad Gateway") {
		t.Errorf("error %q should include status and body", err)
	}
}

func TestContextCancellation(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be reached with a cancelled context")
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := c.UserInfo(ctx, "tok"); err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestProviderFetchMapsFields(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/user/info/":
			writeJSON(t, w, `{"data":{"user":{"open_id":"oid","display_name":"Doc",
				"follower_count":100,"following_count":5,"likes_count":900,"video_count":2}},
				"error":{"code":"ok"}}`)
		case "/v2/video/list/":
			writeJSON(t, w, `{"data":{"videos":[
				{"id":"v1","title":"T1 ","video_description":"D1","duration":30,
				 "create_time":1700000000,"share_url":"https://tiktok.example/v1",
				 "view_count":1,"like_count":2,"comment_count":3,"share_count":4},
				{"id":"v2","create_time":0}],
				"cursor":0,"has_more":false},"error":{"code":"ok"}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})

	var p platform.Provider = NewProvider(c) // compile-time check: *Provider satisfies the interface
	if p.Name() != "tiktok" {
		t.Errorf("Name = %q", p.Name())
	}

	snap, err := p.Fetch(context.Background(), platform.Token{AccessToken: "tok"})
	if err != nil {
		t.Fatal(err)
	}

	s := snap.Stats
	if s.DisplayName != "Doc" || s.Followers != 100 || s.PostCount != 2 {
		t.Errorf("stats = %+v", s)
	}
	if s.Following == nil || *s.Following != 5 || s.Likes == nil || *s.Likes != 900 {
		t.Errorf("following/likes pointers = %v / %v", s.Following, s.Likes)
	}

	if len(snap.Posts) != 2 {
		t.Fatalf("got %d posts", len(snap.Posts))
	}
	p1 := snap.Posts[0]
	if p1.ID != "v1" || p1.Title != "T1" || p1.Description != "D1" || p1.DurationSeconds != 30 ||
		p1.URL != "https://tiktok.example/v1" ||
		p1.Views != 1 || p1.Likes != 2 || p1.Comments != 3 || p1.Shares != 4 {
		t.Errorf("post 1 = %+v", p1)
	}
	if want := time.Unix(1700000000, 0).UTC(); !p1.PublishedAt.Equal(want) {
		t.Errorf("PublishedAt = %s, want %s", p1.PublishedAt, want)
	}
	// create_time 0 means unknown; it must stay the zero time so the store
	// writes NULL instead of 1970-01-01.
	if !snap.Posts[1].PublishedAt.IsZero() {
		t.Errorf("post 2 PublishedAt = %s, want zero", snap.Posts[1].PublishedAt)
	}
}

func TestProviderRefreshUsesRefreshToken(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.PostForm.Get("refresh_token"); got != "the-refresh" {
			t.Errorf("refresh_token = %q", got)
		}
		writeJSON(t, w, `{"access_token":"a2","expires_in":60,"refresh_token":"r2","refresh_expires_in":120}`)
	})

	tok, err := NewProvider(c).Refresh(context.Background(),
		platform.Token{AccessToken: "old-access", RefreshToken: "the-refresh"})
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "a2" || tok.RefreshToken != "r2" {
		t.Errorf("got %+v", tok)
	}
}
