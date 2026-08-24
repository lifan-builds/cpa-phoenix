package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Native CPA keeps PKCE, exchange, and token persistence. Phoenix only
// composes the URL and polls the opaque status in memory.
var nativeManagementBase = "http://127.0.0.1:8317"

// Kept as a narrow seam for deterministic native-response boundary tests.
// Production always uses nativeRequest, which is pinned to CPA's loopback
// Management endpoint and refuses redirects.
var nativeRequestFn = nativeRequest

type nativeOAuthStart struct {
	URL   string `json:"url"`
	State string `json:"state"`
}

func nativeRequest(ctx context.Context, method, path, managementAuthorization string, query url.Values) (*http.Response, error) {
	base, err := url.Parse(nativeManagementBase)
	if err != nil || base.Scheme != "http" || base.Host != "127.0.0.1:8317" {
		return nil, errors.New("native_management_unavailable")
	}
	base.Path = path
	base.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, method, base.String(), nil)
	if err != nil {
		return nil, errors.New("native_management_unavailable")
	}
	if strings.TrimSpace(managementAuthorization) != "" {
		req.Header.Set("Authorization", managementAuthorization)
	}
	req.Header.Set("Accept", "application/json")
	// Native CPA is loopback-only. Never follow a redirect that could forward
	// the in-memory Management authorization outside the pinned endpoint.
	return (&http.Client{Timeout: 15 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}).Do(req)
}

func startNativeOAuth(ctx context.Context, managementAuthorization, email string) (nativeOAuthStart, error) {
	// Deliberately omit is_webui=true. CPA's Web UI helper binds wildcard :1455;
	// Phoenix owns the exact IPv4 loopback callback forwarder instead.
	resp, err := nativeRequestFn(ctx, http.MethodGet, "/v0/management/codex-auth-url", managementAuthorization, url.Values{"is_webui": []string{"false"}})
	if err != nil {
		return nativeOAuthStart{}, errors.New("oauth_start_failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nativeOAuthStart{}, errors.New("oauth_start_rejected")
	}
	var raw nativeOAuthStart
	if json.NewDecoder(resp.Body).Decode(&raw) != nil || strings.TrimSpace(raw.URL) == "" || strings.TrimSpace(raw.State) == "" {
		return nativeOAuthStart{}, errors.New("oauth_start_invalid")
	}
	raw.URL = decodeNativeOAuthHTMLEntityLayer(raw.URL)
	composed, err := composeOAuthURL(raw.URL, email)
	if err != nil {
		return nativeOAuthStart{}, err
	}
	raw.URL = composed
	return raw, nil
}

// CPA's native response may cross one HTML serialization layer. Decode only
// that documented ampersand entity; do not parse, reorder, or reserialize the
// signed PKCE query.
func decodeNativeOAuthHTMLEntityLayer(raw string) string {
	return strings.ReplaceAll(raw, "&amp;", "&")
}

func pollNativeOAuth(ctx context.Context, managementAuthorization, state string) (string, error) {
	state = strings.TrimSpace(state)
	if state == "" {
		return "", errors.New("oauth_state_missing")
	}
	resp, err := nativeRequestFn(ctx, http.MethodGet, "/v0/management/get-auth-status", managementAuthorization, url.Values{"state": []string{state}})
	if err != nil {
		return "", errors.New("oauth_poll_failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", errors.New("oauth_poll_rejected")
	}
	var result struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if json.NewDecoder(resp.Body).Decode(&result) != nil {
		return "", errors.New("oauth_poll_invalid")
	}
	switch strings.ToLower(strings.TrimSpace(result.Status)) {
	case "wait", "pending", "awaiting_user":
		return "wait", nil
	case "success", "completed", "ok":
		return "success", nil
	case "error", "failed", "cancelled", "canceled":
		return "error", nil
	default:
		return "error", nil
	}
}

func cancelNativeOAuth(ctx context.Context, managementAuthorization, state string) error {
	state = strings.TrimSpace(state)
	if state == "" {
		return errors.New("oauth_state_missing")
	}
	resp, err := nativeRequestFn(ctx, http.MethodDelete, "/v0/management/oauth-session", managementAuthorization, url.Values{"state": []string{state}})
	if err != nil {
		return errors.New("oauth_cancel_failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("oauth_cancel_rejected")
	}
	return nil
}
