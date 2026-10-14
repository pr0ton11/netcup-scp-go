package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"golang.org/x/oauth2"
)

// fakeIssuer serves the SCP token endpoints. The returned context routes requests for
// www.servercontrolpanel.de to it.
type fakeIssuer struct {
	mu       sync.Mutex
	requests []url.Values
	handle   func(path string, form url.Values) (int, map[string]any)
}

func (f *fakeIssuer) start(t *testing.T) context.Context {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		f.mu.Lock()
		f.requests = append(f.requests, r.PostForm)
		f.mu.Unlock()
		status, body := f.handle(r.URL.Path, r.PostForm)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	target, _ := url.Parse(srv.URL)
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		r = r.Clone(r.Context())
		r.URL.Scheme, r.URL.Host = target.Scheme, target.Host
		return http.DefaultTransport.RoundTrip(r)
	})}
	return context.WithValue(context.Background(), oauth2.HTTPClient, client)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

const tokenPath = "/realms/scp/protocol/openid-connect/token"

func TestTokenSourceRefreshesAndRotates(t *testing.T) {
	f := &fakeIssuer{handle: func(path string, form url.Values) (int, map[string]any) {
		if path != tokenPath || form.Get("grant_type") != "refresh_token" || form.Get("client_id") != ClientID {
			return http.StatusBadRequest, map[string]any{"error": "invalid_request"}
		}
		return http.StatusOK, map[string]any{
			"access_token": "access-1", "token_type": "Bearer", "expires_in": 300,
			"refresh_token": "refresh-2",
		}
	}}
	ctx := f.start(t)

	var rotated []string
	ts := NewTokenSource(ctx, "refresh-1", func(rt string) error {
		rotated = append(rotated, rt)
		return nil
	})
	for range 3 {
		tok, err := ts.Token()
		if err != nil {
			t.Fatal(err)
		}
		if tok.AccessToken != "access-1" {
			t.Fatalf("access token %q", tok.AccessToken)
		}
	}
	if len(f.requests) != 1 {
		t.Errorf("got %d token requests, want 1 (access token is cached)", len(f.requests))
	}
	if got := f.requests[0].Get("refresh_token"); got != "refresh-1" {
		t.Errorf("refreshed with %q, want refresh-1", got)
	}
	if len(rotated) != 1 || rotated[0] != "refresh-2" {
		t.Errorf("rotated = %v, want [refresh-2]", rotated)
	}
}

func TestTokenSourceRejectedRefreshToken(t *testing.T) {
	f := &fakeIssuer{handle: func(string, url.Values) (int, map[string]any) {
		return http.StatusBadRequest, map[string]any{"error": "invalid_grant", "error_description": "Offline user session not found"}
	}}
	ctx := f.start(t)
	if _, err := NewTokenSource(ctx, "revoked", nil).Token(); err == nil {
		t.Fatal("expected error for a revoked refresh token")
	}
}

func TestLogin(t *testing.T) {
	polls := 0
	f := &fakeIssuer{handle: func(path string, form url.Values) (int, map[string]any) {
		switch path {
		case "/realms/scp/protocol/openid-connect/auth/device":
			if form.Get("client_id") != ClientID || form.Get("scope") != "offline_access openid" {
				return http.StatusBadRequest, map[string]any{"error": "invalid_request"}
			}
			return http.StatusOK, map[string]any{
				"device_code": "dev", "user_code": "ABCD-EFGH",
				"verification_uri":          Issuer + "/device",
				"verification_uri_complete": Issuer + "/device?user_code=ABCD-EFGH",
				"expires_in":                600, "interval": 0,
			}
		case tokenPath:
			if form.Get("device_code") != "dev" {
				return http.StatusBadRequest, map[string]any{"error": "invalid_grant"}
			}
			if polls++; polls < 2 {
				return http.StatusBadRequest, map[string]any{"error": "authorization_pending"}
			}
			return http.StatusOK, map[string]any{
				"access_token": "access", "token_type": "Bearer", "expires_in": 300, "refresh_token": "offline",
			}
		}
		return http.StatusNotFound, nil
	}}
	ctx := f.start(t)

	var code string
	tok, err := Login(ctx, func(da *oauth2.DeviceAuthResponse) { code = da.UserCode })
	if err != nil {
		t.Fatal(err)
	}
	if code != "ABCD-EFGH" || tok.RefreshToken != "offline" {
		t.Fatalf("code %q, refresh token %q", code, tok.RefreshToken)
	}
}

func TestRevoke(t *testing.T) {
	f := &fakeIssuer{handle: func(path string, form url.Values) (int, map[string]any) {
		if path != "/realms/scp/protocol/openid-connect/revoke" || form.Get("token") != "offline" || form.Get("client_id") != ClientID {
			return http.StatusBadRequest, map[string]any{"error": "invalid_request"}
		}
		return http.StatusOK, nil
	}}
	if err := Revoke(f.start(t), "offline"); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshTokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := WriteRefreshToken(path, "offline"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", fi.Mode().Perm())
	}
	got, err := ReadRefreshToken(path)
	if err != nil || got != "offline" {
		t.Fatalf("got %q, %v", got, err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("temporary file left behind: %v", entries)
	}
}
