// Package auth gets and refreshes access tokens for the netcup SCP REST API.
//
// The SCP API uses OpenID Connect with the public client "scp". A person logs in once with
// the device authorization flow ([Login]) and gets an offline refresh token. A service keeps
// that refresh token and uses [NewTokenSource] to get short-lived access tokens from it:
//
//	ts := auth.NewTokenSource(ctx, refreshToken, nil)
//	client, err := scp.NewClientWithResponses(auth.Server, scp.WithHTTPClient(oauth2.NewClient(ctx, ts)))
//
// Offline tokens can be revoked with [Revoke], or in the SCP account console under
// "Account security" > "Device activity" (https://www.servercontrolpanel.de/realms/scp/account).
package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"golang.org/x/oauth2"
)

const (
	// Server is the base URL of the SCP REST API, to pass to scp.NewClient.
	Server = "https://www.servercontrolpanel.de/scp-core"

	// Issuer is the OpenID Connect issuer of the SCP.
	Issuer = "https://www.servercontrolpanel.de/realms/scp"

	// ClientID is the public OpenID Connect client for the SCP REST API.
	ClientID = "scp"

	// RevokeURL is the token revocation endpoint (RFC 7009).
	RevokeURL = Issuer + "/protocol/openid-connect/revoke"
)

// Scopes are requested at login. "offline_access" makes the refresh token an offline token,
// which does not end with a browser session.
var Scopes = []string{"offline_access", "openid"}

// Endpoint holds the OAuth 2.0 endpoints of the SCP.
var Endpoint = oauth2.Endpoint{
	AuthURL:       Issuer + "/protocol/openid-connect/auth",
	DeviceAuthURL: Issuer + "/protocol/openid-connect/auth/device",
	TokenURL:      Issuer + "/protocol/openid-connect/token",
	AuthStyle:     oauth2.AuthStyleInParams,
}

// Config returns the OAuth 2.0 configuration for the SCP REST API.
func Config() *oauth2.Config {
	return &oauth2.Config{
		ClientID: ClientID,
		Endpoint: Endpoint,
		Scopes:   Scopes,
	}
}

// Login runs the device authorization flow. It calls prompt with the URL and code the person
// has to open and confirm, then waits until they did, the code expired, or ctx ends.
// The returned token carries the offline refresh token in RefreshToken.
func Login(ctx context.Context, prompt func(*oauth2.DeviceAuthResponse)) (*oauth2.Token, error) {
	cfg := Config()
	da, err := cfg.DeviceAuth(ctx)
	if err != nil {
		return nil, fmt.Errorf("start device login: %w", err)
	}
	prompt(da)
	tok, err := cfg.DeviceAccessToken(ctx, da)
	if err != nil {
		return nil, fmt.Errorf("device login: %w", err)
	}
	if tok.RefreshToken == "" {
		return nil, errors.New("device login: no refresh token in response")
	}
	return tok, nil
}

// NewTokenSource returns a token source that gets access tokens from refreshToken and caches
// them until shortly before they expire. It is safe for concurrent use.
//
// If the server returns a new refresh token, onRotate is called with it, so it can be stored
// in place of the old one. onRotate may be nil. An error from onRotate fails the Token call;
// the new token is still used for later calls.
func NewTokenSource(ctx context.Context, refreshToken string, onRotate func(refreshToken string) error) oauth2.TokenSource {
	base := Config().TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken})
	return &rotatingSource{base: base, refresh: refreshToken, onRotate: onRotate}
}

type rotatingSource struct {
	base     oauth2.TokenSource
	onRotate func(string) error

	mu      sync.Mutex
	refresh string
}

func (s *rotatingSource) Token() (*oauth2.Token, error) {
	tok, err := s.base.Token()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if tok.RefreshToken != "" && tok.RefreshToken != s.refresh {
		s.refresh = tok.RefreshToken
		if s.onRotate != nil {
			if err := s.onRotate(tok.RefreshToken); err != nil {
				return nil, fmt.Errorf("store rotated refresh token: %w", err)
			}
		}
	}
	return tok, nil
}

// Revoke invalidates refreshToken, which ends the offline session it belongs to.
func Revoke(ctx context.Context, refreshToken string) error {
	form := url.Values{
		"client_id":       {ClientID},
		"token":           {refreshToken},
		"token_type_hint": {"refresh_token"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, RevokeURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		return fmt.Errorf("revoke: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("revoke: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

// httpClient returns the client set in ctx with oauth2.HTTPClient, like the oauth2 package does.
func httpClient(ctx context.Context) *http.Client {
	if c, ok := ctx.Value(oauth2.HTTPClient).(*http.Client); ok {
		return c
	}
	return http.DefaultClient
}
