// Package authotp trades an auth-api email-code token for the address it proved, through auth-api's
// existing S2S route GET /api/v1/s2s/otp/validate (the same one ticketing-api uses). The invitee
// asks auth-api for the code and verifies it there (POST /api/v1/auth/public-otp/send and /verify);
// projects-api never sees the code, only the one-time otp_token.
package authotp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrInvalid is returned for an unknown, expired or already used token.
var ErrInvalid = errors.New("authotp: token invalid or used")

// ErrNotConfigured is returned when the auth URL or service key is missing.
var ErrNotConfigured = errors.New("authotp: not configured")

// Client validates otp tokens with auth-api. It deliberately uses a plain HTTP client with no
// retries: auth-api deletes the token on first read, so a retried request after a lost response
// would always answer "invalid".
type Client struct {
	baseURL    string
	serviceKey string
	http       *http.Client
}

func NewClient(authURL, serviceKey string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(authURL, "/"),
		serviceKey: serviceKey,
		http:       &http.Client{Timeout: 5 * time.Second},
	}
}

// Validate returns the verified email for token. A token works once.
func (c *Client) Validate(ctx context.Context, token string) (string, error) {
	if c == nil || c.baseURL == "" || c.serviceKey == "" {
		return "", ErrNotConfigured
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", ErrInvalid
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/api/v1/s2s/otp/validate?token="+url.QueryEscape(token), nil)
	if err != nil {
		return "", fmt.Errorf("authotp: build request: %w", err)
	}
	req.Header.Set("X-API-Key", c.serviceKey)
	resp, err := c.http.Do(req)
	if err != nil {
		// A *url.Error prints the full URL, token included; keep only the cause so the one-time
		// token never reaches a log.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return "", fmt.Errorf("authotp: request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("authotp: status %d", resp.StatusCode)
	}
	var body struct {
		Valid bool   `json:"valid"`
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("authotp: decode: %w", err)
	}
	if !body.Valid || body.Email == "" {
		return "", ErrInvalid
	}
	return body.Email, nil
}
