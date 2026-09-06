package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHostAuthGetWrapperCredentialIsEphemeral(t *testing.T) {
	raw := json.RawMessage(`{"auth_index":"seat-a","name":"codex-a.json","path":"/state/codex-a.json","json":{"access_token":"fixture-token","account_id":"fixture-account"}}`)
	token, accountID := credentialFromRaw(raw)
	if token != "fixture-token" || accountID != "fixture-account" {
		t.Fatalf("wrapper credential=%q/%q", token, accountID)
	}
}

func TestStableSeatLabelSeparatesSameEmailSeats(t *testing.T) {
	first := account{Email: "same@example.test", AccountID: "opaque-account-a"}
	second := account{Email: first.Email, AccountID: "opaque-account-b"}
	if got := stableSeatLabel(first); got == stableSeatLabel(second) {
		t.Fatalf("same-email seats collapsed to one label: %q", got)
	}
	if stableSeatLabel(first) != stableSeatLabel(first) || stableSeatLabel(first) == first.Email {
		t.Fatalf("seat label must be stable and opaque: %q", stableSeatLabel(first))
	}
	keyFirst := account{Email: first.Email, Key: "opaque-key-a"}
	keySecond := account{Email: first.Email, Key: "opaque-key-b"}
	if stableSeatLabel(keyFirst) == stableSeatLabel(keySecond) {
		t.Fatal("same-email fallback keys collapsed to one label")
	}
}

func TestCodexQuotaUsagePayloadRequiresExplicitWindows(t *testing.T) {
	now := int64(1700000000)
	body := []byte(`{"rate_limit":{"primary_window":{"used_percent":0,"reset_at":1700003600,"limit_window_seconds":3600,"reset_after_seconds":3600},"secondary_window":null}}`)
	quota := parseCodexQuotaPayload(body, now)
	if !freshQuota(quota, time.Unix(now, 0)) {
		t.Fatalf("explicit fresh/absent payload should be eligible: %+v", quota)
	}
	unknown := parseCodexQuotaPayload([]byte(`{"rate_limit":{}}`), now)
	if freshQuota(unknown, time.Unix(now, 0)) {
		t.Fatal("omitted windows must remain unknown")
	}
}

func TestPhysicalInventoryRequiresConfiguredTopLevelPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CPA_AUTH_DIR", dir)
	file := filepath.Join(dir, "codex-a.json")
	if err := os.WriteFile(file, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	entry := authFileEntry{ID: "id-a", AuthIndex: "seat-a", Name: file, Path: file, Source: "file", Provider: "codex"}
	path, authDir, name, physical := exactPhysicalAuthPath(entry)
	if !physical || path == "" || authDir != dir || name != "codex-a.json" {
		t.Fatalf("physical path=%q dir=%q name=%q physical=%v", path, authDir, name, physical)
	}
	entry.Path = filepath.Join(dir, "nested", "codex-a.json")
	if _, _, _, physical := exactPhysicalAuthPath(entry); physical {
		t.Fatal("nested auth path must not be treated as top-level")
	}
	_ = os.Chmod(dir, 0700)
}

func TestQuotaAuthStatusClassification(t *testing.T) {
	active := account{Physical: true, Status: "active", QuotaStatusCode: http.StatusUnauthorized}
	if !invalidAccount(active, 0) {
		t.Fatal("active account with quota HTTP 401 must be actionable")
	}
	for _, status := range []int{http.StatusTooManyRequests, http.StatusForbidden, 0} {
		a := account{Physical: true, Status: "active", QuotaStatusCode: status}
		if invalidAccount(a, 0) {
			t.Fatalf("quota status %d must not be actionable", status)
		}
	}
}

func TestInvalidQueueSelectsQuota401Only(t *testing.T) {
	accounts := []account{
		{Key: "unauthorized", Physical: true, Status: "active", QuotaStatusCode: http.StatusUnauthorized},
		{Key: "rate-limited", Physical: true, Status: "active", QuotaStatusCode: http.StatusTooManyRequests},
		{Key: "forbidden", Physical: true, Status: "active", QuotaStatusCode: http.StatusForbidden},
	}
	rows := invalidAccounts(accounts, map[string]int{})
	if len(rows) != 1 || rows[0].Key != "unauthorized" {
		t.Fatalf("quota-auth queue=%v, want only unauthorized", rows)
	}
}

func TestRuntimeRecentRequestAggregatesDoNotBecomeHTTPStatus(t *testing.T) {
	var runtime authRuntimeResponse
	if err := json.Unmarshal([]byte(`{"auth":{"status":"active"},"recent_requests":{"time":401,"success":1,"failed":0}}`), &runtime); err != nil {
		t.Fatal(err)
	}
	active := account{Physical: true}
	if invalidAuthRuntimeRecord(active, runtime.Auth) {
		t.Fatal("recent_requests aggregate must not classify an active account as 401")
	}
	if !invalidAuthRuntimeRecord(active, authFileEntry{Status: "authentication_error"}) {
		t.Fatal("explicit runtime auth marker must remain actionable")
	}
}

func TestResolveRepairAccountFallsBackToExactPrivateAccountID(t *testing.T) {
	expected := account{Key: "old-host-key", AccountID: "account-a", Email: "same@example.test"}
	candidate := account{Key: "drifted-host-key", AccountID: expected.AccountID, Email: expected.Email, Physical: true}
	resolved, ok := resolveRepairAccount([]account{candidate}, expected)
	if !ok || resolved.Key != candidate.Key {
		t.Fatalf("host-key drift did not resolve exact account: %+v ok=%v", resolved, ok)
	}
}

func TestResolveRepairAccountRejectsAmbiguousOrEmailOnlyMatches(t *testing.T) {
	expected := account{Key: "old-host-key", AccountID: "account-a", Email: "same@example.test"}
	teamA := account{Key: "team-a", AccountID: expected.AccountID, Email: expected.Email, Physical: true}
	teamADuplicate := account{Key: "team-a-duplicate", AccountID: expected.AccountID, Email: expected.Email, Physical: true}
	if resolved, ok := resolveRepairAccount([]account{teamA, teamADuplicate}, expected); ok || resolved.Key != "" {
		t.Fatalf("ambiguous exact-account drift must stop safely: %+v ok=%v", resolved, ok)
	}
	wrongTeam := account{Key: "team-b", AccountID: "account-b", Email: expected.Email, Physical: true}
	if resolved, ok := resolveRepairAccount([]account{wrongTeam}, expected); ok || resolved.Key != "" {
		t.Fatalf("same-email wrong team must never resolve: %+v ok=%v", resolved, ok)
	}
	noPrivateID := account{Key: "team-c", Email: expected.Email, Physical: true}
	if resolved, ok := resolveRepairAccount([]account{noPrivateID}, expected); ok || resolved.Key != "" {
		t.Fatalf("email-only candidate must never resolve: %+v ok=%v", resolved, ok)
	}
	staleKeyWrongTeam := account{Key: expected.Key, AccountID: "account-b", Email: expected.Email, Physical: true}
	if resolved, ok := resolveRepairAccount([]account{staleKeyWrongTeam}, expected); ok || resolved.Key != "" {
		t.Fatalf("stale same-key wrong team must never resolve: %+v ok=%v", resolved, ok)
	}
	emptyIdentity := account{Key: expected.Key, Email: expected.Email}
	if resolved, ok := resolveRepairAccount([]account{emptyIdentity}, emptyIdentity); ok || resolved.Key != "" {
		t.Fatalf("host key without persisted private identity must never resolve: %+v ok=%v", resolved, ok)
	}
	wrongEmail := account{Key: "team-different-email", AccountID: expected.AccountID, Email: "other@example.test", Physical: true}
	if resolved, ok := resolveRepairAccount([]account{wrongEmail}, expected); ok || resolved.Key != "" {
		t.Fatalf("same private ID with a different email must never resolve: %+v ok=%v", resolved, ok)
	}
}
