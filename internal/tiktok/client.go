// Package tiktok is a minimal client for TikTok's v2 Login Kit and Display API.
package tiktok

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const baseURL = "https://open.tiktokapis.com"

type Client struct {
	clientKey    string
	clientSecret string
	http         *http.Client
	baseURL      string // overridable in tests
}

func NewClient(clientKey, clientSecret string) *Client {
	return &Client{
		clientKey:    clientKey,
		clientSecret: clientSecret,
		// http.DefaultClient has no timeout, which can hang a job forever.
		http:    &http.Client{Timeout: 30 * time.Second},
		baseURL: baseURL,
	}
}

// Token is the result of an authorization-code exchange or a refresh.
type Token struct {
	AccessToken           string
	AccessTokenExpiresAt  time.Time
	RefreshToken          string
	RefreshTokenExpiresAt time.Time
	OpenID                string
}

// tokenResponse mirrors TikTok's JSON. Struct tags map JSON keys to fields.
type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	ExpiresIn        int64  `json:"expires_in"`
	OpenID           string `json:"open_id"`
	RefreshToken     string `json:"refresh_token"`
	RefreshExpiresIn int64  `json:"refresh_expires_in"`
	// Token endpoint errors are top-level, unlike the Display API's nested ones.
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// ExchangeCode trades a one-time authorization code for the first tokens.
func (c *Client) ExchangeCode(ctx context.Context, code, redirectURI string) (Token, error) {
	return c.token(ctx, url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {redirectURI},
	})
}

// Refresh gets a new access token. TikTok may also return a NEW refresh token;
// callers must persist whatever comes back or the old one may stop working.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (Token, error) {
	return c.token(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	})
}

func (c *Client) token(ctx context.Context, form url.Values) (Token, error) {
	form.Set("client_key", c.clientKey)
	form.Set("client_secret", c.clientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v2/oauth/token/", strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var resp tokenResponse
	if err := c.do(req, &resp); err != nil {
		return Token{}, err
	}
	if resp.Error != "" || resp.AccessToken == "" {
		return Token{}, fmt.Errorf("token endpoint: %s: %s", resp.Error, resp.ErrorDescription)
	}
	now := time.Now()
	return Token{
		AccessToken:           resp.AccessToken,
		AccessTokenExpiresAt:  now.Add(time.Duration(resp.ExpiresIn) * time.Second),
		RefreshToken:          resp.RefreshToken,
		RefreshTokenExpiresAt: now.Add(time.Duration(resp.RefreshExpiresIn) * time.Second),
		OpenID:                resp.OpenID,
	}, nil
}

// apiError is the error object the Display API nests in every response.
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	LogID   string `json:"log_id"`
}

// TikTok returns code "ok" on success, so "" or "ok" both mean no error.
func (e apiError) err() error {
	if e.Code == "" || e.Code == "ok" {
		return nil
	}
	return fmt.Errorf("tiktok api: %s: %s (log_id %s)", e.Code, e.Message, e.LogID)
}

// UserStats is the account-level data we snapshot.
type UserStats struct {
	OpenID         string `json:"open_id"`
	DisplayName    string `json:"display_name"`
	FollowerCount  int64  `json:"follower_count"`
	FollowingCount int64  `json:"following_count"`
	LikesCount     int64  `json:"likes_count"`
	VideoCount     int64  `json:"video_count"`
}

func (c *Client) UserInfo(ctx context.Context, accessToken string) (UserStats, error) {
	const fields = "open_id,display_name,follower_count,following_count,likes_count,video_count"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/v2/user/info/?fields="+fields, nil)
	if err != nil {
		return UserStats{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	var resp struct {
		Data struct {
			User UserStats `json:"user"`
		} `json:"data"`
		Error apiError `json:"error"`
	}
	if err := c.do(req, &resp); err != nil {
		return UserStats{}, err
	}
	return resp.Data.User, resp.Error.err()
}

// Video holds both the stable fields and the counts that change over time.
type Video struct {
	ID               string `json:"id"`
	Title            string `json:"title"`
	VideoDescription string `json:"video_description"`
	Duration         int    `json:"duration"`
	CreateTime       int64  `json:"create_time"` // unix seconds
	ShareURL         string `json:"share_url"`
	ViewCount        int64  `json:"view_count"`
	LikeCount        int64  `json:"like_count"`
	CommentCount     int64  `json:"comment_count"`
	ShareCount       int64  `json:"share_count"`
}

// ListVideos returns every public video, following the cursor until has_more
// is false.
func (c *Client) ListVideos(ctx context.Context, accessToken string) ([]Video, error) {
	const fields = "id,title,video_description,duration,create_time,share_url," +
		"view_count,like_count,comment_count,share_count"

	var all []Video
	var cursor int64 // 0 means "start from the newest"
	for {
		body := map[string]any{"max_count": 20}
		if cursor != 0 {
			body["cursor"] = cursor
		}
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			c.baseURL+"/v2/video/list/?fields="+fields, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")

		var resp struct {
			Data struct {
				Videos  []Video `json:"videos"`
				Cursor  int64   `json:"cursor"`
				HasMore bool    `json:"has_more"`
			} `json:"data"`
			Error apiError `json:"error"`
		}
		if err := c.do(req, &resp); err != nil {
			return nil, err
		}
		if err := resp.Error.err(); err != nil {
			return nil, err
		}
		all = append(all, resp.Data.Videos...)
		if !resp.Data.HasMore {
			return all, nil
		}
		cursor = resp.Data.Cursor
	}
}

// do sends the request and decodes the JSON body into out. TikTok sometimes
// returns useful error JSON on non-200 statuses, so we decode first and only
// fall back to the raw body if that fails.
func (c *Client) do(req *http.Request, out any) error {
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close() // defer runs when the function returns

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("http %d, undecodable body %q: %w", res.StatusCode, truncate(raw), err)
	}
	return nil
}

func truncate(b []byte) string {
	if len(b) > 200 {
		b = b[:200]
	}
	return string(b)
}
