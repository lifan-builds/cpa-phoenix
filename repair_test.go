package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeLoginBrowser struct {
	mu       sync.Mutex
	run      func(context.Context, string, string, string, time.Time, func(string)) error
	runs     int
	active   int
	closed   int
	overlaps int
}

func (b *fakeLoginBrowser) Run(ctx context.Context, oauthURL, email, accountID string, requestedAt time.Time, report func(string)) error {
	b.mu.Lock()
	b.runs++
	if b.active != 0 {
		b.overlaps++
	}
	b.active++
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		b.active--
		b.mu.Unlock()
	}()
	if b.run != nil {
		return b.run(ctx, oauthURL, email, accountID, requestedAt, report)
	}
	return nil
}

func (b *fakeLoginBrowser) Close() error {
	b.mu.Lock()
	b.closed++
	b.mu.Unlock()
	return nil
}

func TestComposeOAuthURLModePrefillsWithoutReserializingNativeFields(t *testing.T) {
	raw := "https://login.example.test/authorize?prompt=select_account&login_hint=old%40example.test&state=opaque&x=%2F"
	got, err := composeOAuthURLMode(raw, "seat@example.test", false)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://login.example.test/authorize?state=opaque&x=%2F&login_hint=seat%40example.test&prompt=login"
	if got != want {
		t.Fatalf("native fields were reserialized or prefill was wrong: got %q want %q", got, want)
	}
	if strings.Count(got, "login_hint=") != 1 || strings.Count(got, "prompt=") != 1 {
		t.Fatalf("prefill fields must occur exactly once: %q", got)
	}
	for _, invalid := range []string{"", "http://login.example.test/a", "https://user:pass@login.example.test/a", "https://login.example.test/a?state=one;two", "not a url"} {
		if _, err := composeOAuthURLMode(invalid, "", false); err == nil {
			t.Fatalf("invalid URL accepted: %q", invalid)
		}
	}
}

func TestComposeOAuthURLModePrefillEscapesPlusEmail(t *testing.T) {
	got, err := composeOAuthURLMode("https://login.example.test/authorize?client_id=x&code_challenge=a%2Bb&state=s", "plus+tag@example.test", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "login_hint=plus%2Btag%40example.test") {
		t.Fatalf("plus sign must be percent-encoded in login_hint: %q", got)
	}
	if strings.Contains(got, "amp;") || !strings.Contains(got, "code_challenge=a%2Bb&state=s") {
		t.Fatalf("native query bytes were not preserved: %q", got)
	}
}

func TestComposeOAuthURLSelectsAccountForDuplicateEmailSeats(t *testing.T) {
	raw := "https://login.example.test/authorize?client_id=x&login_hint=old%40example.test&prompt=login&state=s"
	got, err := composeOAuthURLMode(raw, "same@example.test", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "login_hint=same%40example.test") || !strings.Contains(got, "prompt=select_account") {
		t.Fatalf("duplicate-email flow must prefill the email and force account selection: %q", got)
	}
}

func TestComposeOAuthURLModeEmptyEmailLeavesValidatedNativeBytes(t *testing.T) {
	raw := "https://login.example.test/authorize?client_id=x&prompt=login&state=s&x=%2F#fragment"
	got, err := composeOAuthURLMode(raw, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if got != raw {
		t.Fatalf("empty email must leave native URL unchanged: got %q want %q", got, raw)
	}
	if _, err := composeOAuthURLMode("https://login.example.test/authorize?client_id=x&amp;state=s", "", false); err == nil {
		t.Fatal("residual amp-prefixed query key must still be rejected without email")
	}
}

func TestDecodeNativeOAuthHTMLLayerOnce(t *testing.T) {
	raw := "https://login.example.test/authorize?client_id=fixture&amp;state=fixture#nested&amp;amp;fragment"
	decoded := decodeNativeOAuthHTMLEntityLayer(raw)
	if !strings.Contains(decoded, "&state=fixture") || !strings.Contains(decoded, "#nested&amp;fragment") {
		t.Fatalf("one-layer decode lost native separators or nested entity: %q", decoded)
	}
	decodedTwice := decodeNativeOAuthHTMLEntityLayer(decoded)
	if !strings.Contains(decodedTwice, "#nested&fragment") || decodedTwice == decoded {
		t.Fatalf("nested entity must remain after exactly one decode: once=%q twice=%q", decoded, decodedTwice)
	}
}

func TestStartNativeOAuthDecodesResponseBoundaryWithoutReserializing(t *testing.T) {
	oldRequest := nativeRequestFn
	t.Cleanup(func() { nativeRequestFn = oldRequest })
	nativeRequestFn = func(_ context.Context, method, path, _ string, query url.Values) (*http.Response, error) {
		if method != http.MethodGet || path != "/v0/management/codex-auth-url" || query.Get("is_webui") != "false" {
			t.Fatalf("unexpected native OAuth request: method=%q path=%q query=%v", method, path, query)
		}
		body := `{"url":"https://login.example.test/authorize?client_id=fixture-client&amp;code_challenge=fixture-challenge%2Bvalue&amp;code_challenge_method=S256&amp;redirect_uri=http%3A%2F%2F127.0.0.1%3A1455%2Fauth%2Fcallback&amp;state=fixture-state&amp;prompt=login#nested&amp;amp;fragment","state":"fixture-state"}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	}

	started, err := startNativeOAuth(context.Background(), "Bearer fixture-management-key", "fixture@example.test")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://login.example.test/authorize?client_id=fixture-client&code_challenge=fixture-challenge%2Bvalue&code_challenge_method=S256&redirect_uri=http%3A%2F%2F127.0.0.1%3A1455%2Fauth%2Fcallback&state=fixture-state&login_hint=fixture%40example.test&prompt=login#nested&amp;fragment"
	if started.URL != want {
		t.Fatalf("native OAuth bytes changed at response boundary: got %q want %q", started.URL, want)
	}
	parsed, err := url.Parse(started.URL)
	if err != nil {
		t.Fatal(err)
	}
	values, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"client_id", "code_challenge", "code_challenge_method", "redirect_uri", "state", "prompt"} {
		if _, ok := values[key]; !ok {
			t.Fatalf("native query key %q missing after one response-layer decode: %v", key, values)
		}
	}
	for key := range values {
		if strings.HasPrefix(key, "amp;") {
			t.Fatalf("entity-prefixed query key leaked past response boundary: %q", key)
		}
	}
	if values.Get("login_hint") != "fixture@example.test" {
		t.Fatalf("guided prefill missing or wrong: %v", values)
	}
	if values.Get("prompt") != "login" || len(values["prompt"]) != 1 || len(values["login_hint"]) != 1 {
		t.Fatalf("prefill fields must be exactly once: %v", values)
	}
	if parsed.Fragment != "nested&amp;fragment" {
		t.Fatalf("nested entity was decoded more than once: %q", parsed.Fragment)
	}
}

func TestComposeOAuthURLRejectsResidualEntityPrefixedQueryKeys(t *testing.T) {
	raw := decodeNativeOAuthHTMLEntityLayer("https://login.example.test/authorize?client_id=fixture&amp;amp;state=fixture")
	if !strings.Contains(raw, "&amp;state=fixture") {
		t.Fatalf("nested entity was decoded more than once before validation: %q", raw)
	}
	if _, err := composeOAuthURLMode(raw, "", false); err == nil {
		t.Fatal("entity-prefixed query key must be rejected before navigation")
	}
}

func TestInvalidMarkersAndPhysicalBoundary(t *testing.T) {
	a := account{Key: "a", Physical: true, Status: "authentication_error"}
	if !invalidAccount(a, 0) {
		t.Fatal("marker should classify")
	}
	unavailable := account{Key: "unavailable", Physical: true, Unavailable: true, Status: "auth_unavailable"}
	if !invalidAccount(unavailable, 0) {
		t.Fatal("auth_unavailable marker must remain actionable")
	}
	b := account{Key: "b", Physical: false, Status: "authentication_error"}
	if invalidAccount(b, http.StatusUnauthorized) {
		t.Fatal("runtime-only entry must not enter repair queue")
	}
}
func TestSafeQuarantineOnlyAuthDirectory(t *testing.T) {
	root := t.TempDir()
	auth := filepath.Join(root, "auth")
	q := filepath.Join(root, "q")
	if err := os.Mkdir(auth, 0700); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(auth, "codex-a.json")
	if err := os.WriteFile(src, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	dest, err := safeQuarantine(src, auth, q)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("source must be moved")
	}
	if _, err := safeQuarantine(dest, auth, q); err == nil {
		t.Fatal("outside auth directory must be rejected")
	}
}

func TestSafeQuarantineRejectsSymlinkedRoot(t *testing.T) {
	root := t.TempDir()
	auth := filepath.Join(root, "auth")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(auth, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0700); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(auth, "codex-a.json")
	if err := os.WriteFile(src, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(root, "quarantine")
	if err := os.Symlink(outside, linked); err != nil {
		t.Fatal(err)
	}
	if _, err := safeQuarantine(src, auth, linked); err == nil {
		t.Fatal("symlinked quarantine root must be rejected")
	}
}

func TestReviveQueueDriftSkipsNowValidRow(t *testing.T) {
	// A row can become healthy after the initial queue snapshot. The
	// pre-quarantine revalidation must reject it without treating a generic
	// active record as an auth repair target.
	current := account{Key: "drifted-seat", Physical: true, Status: "active", QuotaStatusCode: http.StatusOK}
	if invalidAuthDecision(current) {
		t.Fatal("now-valid queued row must not remain actionable")
	}
}

func TestDefinitiveHealthyQuotaProbeOverridesStaleRuntimeMarker(t *testing.T) {
	a := account{Physical: true, QuotaStatusCode: http.StatusOK, Status: "authentication_error"}
	if !definitiveHealthy(a) {
		t.Fatal("direct successful quota proof must override a stale runtime auth marker")
	}
}

func TestCaptureRepairRowsSkipsMissingPrivateAccountID(t *testing.T) {
	rows := captureRepairRows([]account{{Key: "missing-private-id", AuthIndex: "fixture-index", Physical: true}})
	if len(rows) != 0 {
		t.Fatalf("missing private account identity must be skipped, got %+v", rows)
	}
}

func TestBeginReviveDoesNotPersistMissingPrivateIdentity(t *testing.T) {
	db, _ := setupRepairRecoveryStore(t)
	installRepairTestDeps(t,
		func() ([]account, error) {
			return []account{
				{Key: "missing-private-id", AuthIndex: "missing-index", Email: "same@example.test", Physical: true, Status: "authentication_error"},
				{Key: "known-private-id", AuthIndex: "known-index", AccountID: "account-known", Email: "same@example.test", AuthPath: "/auth/known.json", AuthDir: "/auth", Physical: true, Status: "authentication_error"},
			}, nil
		},
		func(string, string, string) (string, error) { return "quarantine-marker", nil },
		func(context.Context, string, string) (nativeOAuthStart, error) {
			return nativeOAuthStart{}, errors.New("should_not_start_before_port_gate")
		},
	)
	oldBind := repairBindCallbackForwarder
	repairBindCallbackForwarder = func() (*callbackForwarder, error) { return nil, errors.New("callback_port_unavailable") }
	t.Cleanup(func() { repairBindCallbackForwarder = oldBind })

	response, err := beginRevive(nil)
	if err != nil || response.Total != 1 {
		t.Fatalf("beginRevive should queue only the exact-identity row: response=%+v err=%v", response, err)
	}
	job := waitForRepairJobState(t, response.JobID, "failed")
	if job.Reason != "callback_port_unavailable" {
		t.Fatalf("unexpected filtered queue terminal reason: %+v", job)
	}
	rows, err := loadRepairRows(response.JobID)
	if err != nil || len(rows) != 1 || rows[0].AccountID != "account-known" {
		t.Fatalf("missing private identity leaked into durable queue: rows=%+v err=%v", rows, err)
	}
	if _, err := db.Exec(`DELETE FROM jobs WHERE id=?`, response.JobID); err != nil {
		t.Fatal(err)
	}
}

func TestQuarantinedResumeWithMissingPrivateIdentityStopsBeforeOAuth(t *testing.T) {
	db, _ := setupRepairRecoveryStore(t)
	jobID := "repair-missing-resume-identity"
	seedRepairRecoveryState(t, db, jobID, "failed", "quarantined", "quarantine-marker")
	var oauthStarts int
	installRepairTestDeps(t,
		func() ([]account, error) {
			return []account{{Key: "repair-seat", AuthIndex: "auth-index", Email: "same@example.test", Physical: true}}, nil
		},
		func(string, string, string) (string, error) {
			t.Fatal("missing identity must not quarantine again")
			return "", nil
		},
		func(context.Context, string, string) (nativeOAuthStart, error) {
			oauthStarts++
			return nativeOAuthStart{}, nil
		},
	)
	if got := processReviveRow(context.Background(), &reviveRuntime{jobID: jobID}, 1, account{Key: "repair-seat", AccountID: "", RepairState: "quarantined", Quarantine: "quarantine-marker", Physical: true}); got != "missing_identity" {
		t.Fatalf("missing identity resume reason=%q", got)
	}
	if oauthStarts != 0 {
		t.Fatalf("missing identity started OAuth %d times", oauthStarts)
	}
	rows, err := loadRepairRows(jobID)
	if err != nil || len(rows) != 1 || rows[0].Quarantine != "quarantine-marker" {
		t.Fatalf("missing identity resume changed quarantine state: rows=%+v err=%v", rows, err)
	}
	if _, err := db.Exec(`DELETE FROM jobs WHERE id=?`, jobID); err != nil {
		t.Fatal(err)
	}
}

func TestReviveQueueMarksExactAlreadyAuthorizedDriftedRowRepaired(t *testing.T) {
	db, _ := setupRepairRecoveryStore(t)
	jobID := "repair-already-authorized"
	seedRepairRecoveryState(t, db, jobID, "running", "queued", "")
	expected := account{Key: "old-host-key", AccountID: "account-a", Email: "same@example.test", Physical: true}
	current := account{Key: "new-host-key", AccountID: expected.AccountID, Email: expected.Email, AuthPath: "/auth/codex-a.json", AuthDir: "/auth", Physical: true, Status: "authentication_error"}
	installRepairTestDeps(t,
		func() ([]account, error) { return []account{current}, nil },
		func(string, string, string) (string, error) {
			t.Fatal("healthy drift must not quarantine")
			return "", nil
		},
		func(context.Context, string, string) (nativeOAuthStart, error) {
			t.Fatal("healthy drift must not start OAuth")
			return nativeOAuthStart{}, nil
		},
	)
	oldRefreshQuota := repairRefreshQuota
	repairRefreshQuota = func(a account) account {
		a.QuotaStatusCode = http.StatusOK
		return a
	}
	t.Cleanup(func() { repairRefreshQuota = oldRefreshQuota })
	if got := processReviveRow(context.Background(), &reviveRuntime{jobID: jobID}, 1, expected); got != "repaired" {
		t.Fatalf("already-authorized exact drift should advance as repaired: %q", got)
	}
	rows, err := loadRepairRows(jobID)
	if err != nil || len(rows) != 1 || rows[0].RepairState != "repaired" {
		t.Fatalf("healthy drift row was not durably marked repaired: rows=%+v err=%v", rows, err)
	}
}

func TestReviveQueueReconcilesHealthyReplacementAfterQuarantinedMismatch(t *testing.T) {
	db, _ := setupRepairRecoveryStore(t)
	jobID := "repair-quarantined-healthy"
	seedRepairRecoveryState(t, db, jobID, "failed", "failed", "quarantine-marker")
	expected := account{Key: "old-host-key", AuthIndex: "old-auth-index", AccountID: "account-a", Email: "same@example.test", Physical: true, RepairState: "failed", Quarantine: "quarantine-marker"}
	replacement := account{Key: "new-host-key", AuthIndex: "new-auth-index", AccountID: expected.AccountID, Email: expected.Email, AuthPath: "/auth/codex-a.json", AuthDir: "/auth", Physical: true, Status: "active"}
	installRepairTestDeps(t,
		func() ([]account, error) { return []account{replacement}, nil },
		func(string, string, string) (string, error) {
			t.Fatal("quarantined healthy replacement must not quarantine again")
			return "", nil
		},
		func(context.Context, string, string) (nativeOAuthStart, error) {
			t.Fatal("quarantined healthy replacement must not start OAuth again")
			return nativeOAuthStart{}, nil
		},
	)
	oldRefreshQuota := repairRefreshQuota
	repairRefreshQuota = func(a account) account {
		a.QuotaStatusCode = http.StatusOK
		a.Quota = quotaSnapshot{Windows: []quotaWindow{{Presence: windowPresent}}}
		return a
	}
	t.Cleanup(func() { repairRefreshQuota = oldRefreshQuota })

	if got := processReviveRow(context.Background(), &reviveRuntime{jobID: jobID}, 1, expected); got != "repaired" {
		t.Fatalf("quarantined healthy replacement should reconcile as repaired: %q", got)
	}
	rows, err := loadRepairRows(jobID)
	if err != nil || len(rows) != 1 || rows[0].RepairState != "repaired" {
		t.Fatalf("quarantined healthy replacement row was not durably repaired: rows=%+v err=%v", rows, err)
	}
}

func TestValidatedReplacementWaitsForCPAInventoryPropagation(t *testing.T) {
	expected := account{Key: "repair-seat", AuthIndex: "auth-index", AccountID: "account-a", Physical: true, UpdatedAt: "before"}
	replacement := expected
	replacement.UpdatedAt = "after"
	replacement.AccessTokenValue = "replacement-token"

	oldInventory := repairListAccounts
	oldRefreshQuota := repairRefreshQuota
	oldWaitLimit := repairReplacementWaitLimit
	oldPollDelay := repairReplacementPollDelay
	var inventoryCalls int
	repairListAccounts = func() ([]account, error) {
		inventoryCalls++
		if inventoryCalls == 1 {
			return nil, nil
		}
		return []account{replacement}, nil
	}
	repairRefreshQuota = func(a account) account {
		a.QuotaStatusCode = http.StatusOK
		a.Quota = quotaSnapshot{Windows: []quotaWindow{{Presence: windowPresent}}}
		return a
	}
	repairReplacementWaitLimit = 50 * time.Millisecond
	repairReplacementPollDelay = time.Millisecond
	t.Cleanup(func() {
		repairListAccounts = oldInventory
		repairRefreshQuota = oldRefreshQuota
		repairReplacementWaitLimit = oldWaitLimit
		repairReplacementPollDelay = oldPollDelay
	})

	if _, err := validatedReplacement(context.Background(), []account{expected}, expected); err != nil {
		t.Fatalf("replacement propagation should validate: %v", err)
	}
	if inventoryCalls < 2 {
		t.Fatalf("replacement validation did not wait for inventory propagation: calls=%d", inventoryCalls)
	}
}

func TestValidatedReplacementAcceptsDifferentWorkspaceWithSameEmail(t *testing.T) {
	expected := account{Key: "old-seat", AccountID: "workspace-a", Email: "same@example.test", Physical: true}
	replacement := account{Key: "new-seat", AccountID: "workspace-b", Email: "same@example.test", Physical: true, AccessTokenValue: "token"}
	oldInventory := repairListAccounts
	oldRefreshQuota := repairRefreshQuota
	oldWaitLimit := repairReplacementWaitLimit
	repairListAccounts = func() ([]account, error) { return []account{replacement}, nil }
	repairRefreshQuota = func(a account) account {
		a.QuotaStatusCode = http.StatusOK
		a.Quota = quotaSnapshot{Windows: []quotaWindow{{Presence: windowPresent}}}
		return a
	}
	repairReplacementWaitLimit = 50 * time.Millisecond
	t.Cleanup(func() {
		repairListAccounts = oldInventory
		repairRefreshQuota = oldRefreshQuota
		repairReplacementWaitLimit = oldWaitLimit
	})
	if _, err := validatedReplacement(context.Background(), []account{expected}, expected); err != nil {
		t.Fatalf("same-email replacement with a different workspace should validate: %v", err)
	}
}

func TestReviveQueueRoutesSameEmailOAuthToTheWorkspaceActuallyReturned(t *testing.T) {
	t.Run("automatic", func(t *testing.T) { testReviveWorkspaceQueue(t, reviveBrowserModeAutomatic) })
	t.Run("agent", func(t *testing.T) { testReviveWorkspaceQueue(t, reviveBrowserModeAgent) })
}

func testReviveWorkspaceQueue(t *testing.T, mode string) {
	db, _ := setupRepairRecoveryStore(t)
	jobID := "repair-same-email-workspaces"
	now := time.Now().Unix()
	if _, err := db.Exec(`INSERT INTO jobs(id,kind,state,created_at,updated_at,total,done) VALUES(?,?,?,?,?,?,?)`, jobID, "revive", "running", now, now, 2, 0); err != nil {
		t.Fatal(err)
	}

	first := account{Key: "seat-a", AuthID: "auth-a", AuthIndex: "index-a", AccountID: "workspace-a", Email: "same@example.test", AuthPath: "/auth/a.json", AuthDir: "/auth", Physical: true, Status: "authentication_error", PhysicalModTime: 1}
	second := account{Key: "seat-b", AuthID: "auth-b", AuthIndex: "index-b", AccountID: "workspace-b", Email: first.Email, AuthPath: "/auth/b.json", AuthDir: "/auth", Physical: true, Status: "authentication_error", PhysicalModTime: 1}
	for i, row := range []account{first, second} {
		if _, err := db.Exec(`INSERT INTO repair_rows(job_id,ordinal,account_key,auth_id,auth_index,email,account_id,state) VALUES(?,?,?,?,?,?,?,'queued')`, jobID, i+1, row.Key, row.AuthID, row.AuthIndex, row.Email, row.AccountID); err != nil {
			t.Fatal(err)
		}
	}

	secondReplacement := second
	secondReplacement.Status = "active"
	secondReplacement.PhysicalModTime = 2
	secondReplacement.AccessTokenValue = "token-b"
	firstReplacement := first
	firstReplacement.Status = "active"
	firstReplacement.PhysicalModTime = 2
	firstReplacement.AccessTokenValue = "token-a"

	quarantined := false
	oauthAttempts := 0
	oldInventory := repairListAccounts
	oldQuarantine := repairSafeQuarantine
	oldBind := repairBindCallbackForwarder
	oldStartMode := repairStartNativeOAuthMode
	oldPoll := repairPollNativeOAuth
	oldRefreshQuota := repairRefreshQuota
	oldNewBrowser := repairNewLoginBrowser
	browser := &fakeLoginBrowser{run: func(ctx context.Context, oauthURL, email, accountID string, requestedAt time.Time, report func(string)) error {
		if oauthURL == "" || email != first.Email || requestedAt.IsZero() {
			t.Errorf("automatic login received incomplete attempt data: url=%q email=%q requested=%v", oauthURL, email, requestedAt)
		}
		if accountID != first.AccountID && accountID != second.AccountID {
			t.Errorf("automatic login received unexpected workspace %q", accountID)
		}
		report("email_code_requested")
		report("verification_code_submitted")
		<-ctx.Done()
		return ctx.Err()
	}}
	repairNewLoginBrowser = func(context.Context) (loginBrowserController, error) {
		if mode == reviveBrowserModeAgent {
			t.Fatal("agent mode launched a browser")
		}
		return browser, nil
	}
	repairListAccounts = func() ([]account, error) {
		switch oauthAttempts {
		case 0:
			if quarantined {
				return []account{second}, nil
			}
			return []account{first, second}, nil
		case 1:
			return []account{secondReplacement}, nil
		default:
			return []account{secondReplacement, firstReplacement}, nil
		}
	}
	repairSafeQuarantine = func(string, string, string) (string, error) {
		quarantined = true
		return "quarantine-a.json", nil
	}
	repairBindCallbackForwarder = func() (*callbackForwarder, error) { return &callbackForwarder{}, nil }
	repairStartNativeOAuthMode = func(context.Context, string, string, bool) (nativeOAuthStart, error) {
		oauthAttempts++
		return nativeOAuthStart{URL: "https://login.example.test/authorize", State: "state-" + strconv.Itoa(oauthAttempts)}, nil
	}
	repairPollNativeOAuth = func(context.Context, string, string) (string, error) { return "success", nil }
	repairRefreshQuota = func(a account) account {
		if a.AccessTokenValue == "" {
			a.QuotaStatusCode = http.StatusUnauthorized
			return a
		}
		a.QuotaStatusCode = http.StatusOK
		a.Quota = quotaSnapshot{Windows: []quotaWindow{{Presence: windowPresent}}}
		return a
	}
	t.Cleanup(func() {
		repairListAccounts = oldInventory
		repairSafeQuarantine = oldQuarantine
		repairBindCallbackForwarder = oldBind
		repairStartNativeOAuthMode = oldStartMode
		repairPollNativeOAuth = oldPoll
		repairRefreshQuota = oldRefreshQuota
		repairNewLoginBrowser = oldNewBrowser
	})

	runtime := &reviveRuntime{jobID: jobID, queued: []account{first, second}, browserMode: mode}
	runReviveQueue(runtime)
	job, err := readJob(jobID)
	if err != nil || job.State != "completed" || job.Done != 2 {
		t.Fatalf("job did not complete both workspace rows: job=%+v err=%v", job, err)
	}
	rows, err := loadRepairRows(jobID)
	if err != nil || len(rows) != 2 || rows[0].RepairState != "repaired" || rows[1].RepairState != "repaired" {
		t.Fatalf("workspace rows were not routed and repaired: rows=%+v err=%v", rows, err)
	}
	if oauthAttempts != 2 {
		t.Fatalf("expected a second login for the remaining workspace, got %d attempts", oauthAttempts)
	}
	browser.mu.Lock()
	runs, closed, overlaps, active := browser.runs, browser.closed, browser.overlaps, browser.active
	browser.mu.Unlock()
	wantRuns, wantClosed := 2, 1
	if mode == reviveBrowserModeAgent {
		wantRuns, wantClosed = 0, 0
	}
	if runs != wantRuns || closed != wantClosed || overlaps != 0 || active != 0 {
		t.Fatalf("browser queue lifecycle: runs=%d closed=%d overlaps=%d active=%d", runs, closed, overlaps, active)
	}
}

func TestReviveQueueAcceptsChangedSameKeyReplacementEndToEnd(t *testing.T) {
	db, _ := setupRepairRecoveryStore(t)
	jobID := "repair-same-key-replacement"
	seedRepairRecoveryState(t, db, jobID, "running", "queued", "")
	expected := account{Key: "repair-seat", AuthID: "auth-id", AuthIndex: "auth-index", AccountID: "account-a", Email: "same@example.test", AuthPath: "/auth/codex-a.json", AuthDir: "/auth", Physical: true, Status: "authentication_error", UpdatedAt: "before"}
	if _, err := db.Exec(`UPDATE repair_rows SET account_id=? WHERE job_id=? AND ordinal=1`, expected.AccountID, jobID); err != nil {
		t.Fatal(err)
	}
	changed := expected
	changed.Status = "active"
	changed.UpdatedAt = "after"
	changed.AccessTokenValue = "replacement-token"

	var inventoryCalls, quarantineCalls, oauthStarts int
	installRepairTestDeps(t,
		func() ([]account, error) {
			inventoryCalls++
			switch inventoryCalls {
			case 1:
				return []account{expected}, nil
			case 2:
				return nil, nil // old same-key record is gone after quarantine
			default:
				return []account{changed}, nil
			}
		},
		func(string, string, string) (string, error) {
			quarantineCalls++
			return "quarantine-marker", nil
		},
		func(context.Context, string, string) (nativeOAuthStart, error) {
			oauthStarts++
			return nativeOAuthStart{URL: "https://login.example.test/authorize?state=fixture", State: "fixture-state"}, nil
		},
	)
	oldPoll := repairPollNativeOAuth
	oldRefreshQuota := repairRefreshQuota
	repairPollNativeOAuth = func(context.Context, string, string) (string, error) { return "success", nil }
	repairRefreshQuota = func(a account) account {
		if a.AccessTokenValue != "" {
			a.QuotaStatusCode = http.StatusOK
			a.Quota = quotaSnapshot{Windows: []quotaWindow{{Presence: windowPresent}}}
			return a
		}
		a.QuotaStatusCode = http.StatusUnauthorized
		return a
	}
	t.Cleanup(func() {
		repairPollNativeOAuth = oldPoll
		repairRefreshQuota = oldRefreshQuota
	})

	if got := processReviveRow(context.Background(), &reviveRuntime{jobID: jobID, queued: []account{expected}, browser: &fakeLoginBrowser{}}, 1, expected); got != "repaired" {
		t.Fatalf("changed same-key replacement did not complete end-to-end: %q", got)
	}
	if inventoryCalls != 3 || quarantineCalls != 1 || oauthStarts != 1 {
		t.Fatalf("unexpected repair sequence: inventory=%d quarantine=%d oauth=%d", inventoryCalls, quarantineCalls, oauthStarts)
	}
	rows, err := loadRepairRows(jobID)
	if err != nil || len(rows) != 1 || rows[0].RepairState != "repaired" || rows[0].Quarantine != "quarantine-marker" {
		t.Fatalf("same-key replacement row state=%+v err=%v", rows, err)
	}
}

func TestForeignReviveCancelCannotStopIgnite(t *testing.T) {
	storePath = t.TempDir() + "/state.db"
	closeStore()
	t.Cleanup(func() {
		globalJobs.stop()
		closeStore()
	})
	igniteID, err := globalJobs.start("ignite", 1)
	if err != nil {
		t.Fatal(err)
	}
	response := handleManagement(managementRequest{Method: http.MethodPost, Path: "/v0/management/plugins/cpa-phoenix/revive/cancel", Query: map[string][]string{"id": {"foreign-revive"}}})
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("foreign cancel status=%d body=%s", response.StatusCode, response.Body)
	}
	globalJobs.mu.Lock()
	active := globalJobs.active
	globalJobs.mu.Unlock()
	if active != igniteID {
		t.Fatalf("foreign revive cancel stopped Ignite: active=%q want=%q", active, igniteID)
	}
	job, err := readJob(igniteID)
	if err != nil || job.State != "running" {
		t.Fatalf("Ignite state changed after foreign cancel: job=%+v err=%v", job, err)
	}
	for _, queryID := range []string{"", "   ", igniteID, "unknown-revive"} {
		response := handleManagement(managementRequest{Method: http.MethodPost, Path: "/v0/management/plugins/cpa-phoenix/revive/cancel", Query: map[string][]string{"id": {queryID}}})
		if response.StatusCode == http.StatusOK {
			t.Fatalf("foreign/empty cancel unexpectedly succeeded for %q: %s", queryID, response.Body)
		}
		globalJobs.mu.Lock()
		active = globalJobs.active
		globalJobs.mu.Unlock()
		if active != igniteID {
			t.Fatalf("cancel %q changed Ignite ownership: active=%q want=%q", queryID, active, igniteID)
		}
	}
}

func TestValidReviveCancelStopsOnlyThatRevive(t *testing.T) {
	storePath = t.TempDir() + "/state.db"
	closeStore()
	t.Cleanup(func() {
		globalJobs.stop()
		closeStore()
	})
	for _, formatID := range []func(string) string{
		func(id string) string { return id },
		func(id string) string { return "  " + id + "  " },
	} {
		reviveID, err := globalJobs.start("revive", 1)
		if err != nil {
			t.Fatal(err)
		}
		response := handleManagement(managementRequest{Method: http.MethodPost, Path: "/v0/management/plugins/cpa-phoenix/revive/cancel", Query: map[string][]string{"id": {formatID(reviveID)}}})
		if response.StatusCode != http.StatusOK || !strings.Contains(string(response.Body), `"cancelled"`) {
			t.Fatalf("valid revive cancel status=%d body=%s", response.StatusCode, response.Body)
		}
		globalJobs.mu.Lock()
		active := globalJobs.active
		globalJobs.mu.Unlock()
		if active != "" {
			t.Fatalf("valid revive cancel left active job=%q", active)
		}
		job, err := readJob(reviveID)
		if err != nil || job.State != "cancelled" {
			t.Fatalf("revive state=%+v err=%v", job, err)
		}
	}
}

func setupRepairRecoveryStore(t *testing.T) (*sql.DB, string) {
	t.Helper()
	oldPath := storePath
	storePath = filepath.Join(t.TempDir(), "state.db")
	closeStore()
	globalJobs.stop()
	db, err := openStore()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		globalJobs.stop()
		closeStore()
		storePath = oldPath
	})
	return db, storePath
}

func seedRepairRecoveryState(t *testing.T, db *sql.DB, jobID, jobState string, rowState, quarantine string) {
	t.Helper()
	now := time.Now().Unix()
	if _, err := db.Exec(`INSERT INTO jobs(id,kind,state,created_at,updated_at,total,done) VALUES(?,?,?,?,?,?,?)`, jobID, "revive", jobState, now, now, 1, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO repair_rows(job_id,ordinal,account_key,auth_id,auth_index,email,account_id,state,quarantine) VALUES(?,?,?,?,?,?,?,?,?)`, jobID, 1, "repair-seat", "auth-id", "auth-index", "opaque-seat", "account-id", rowState, quarantine); err != nil {
		t.Fatal(err)
	}
}

func installRepairTestDeps(t *testing.T, inventory func() ([]account, error), quarantine func(string, string, string) (string, error), start func(context.Context, string, string) (nativeOAuthStart, error)) {
	t.Helper()
	oldInventory := repairListAccounts
	oldQuarantine := repairSafeQuarantine
	oldBind := repairBindCallbackForwarder
	oldStart := repairStartNativeOAuth
	oldPoll := repairPollNativeOAuth
	oldRefreshQuota := repairRefreshQuota
	oldNewBrowser := repairNewLoginBrowser
	repairListAccounts = inventory
	repairSafeQuarantine = quarantine
	repairBindCallbackForwarder = func() (*callbackForwarder, error) { return &callbackForwarder{}, nil }
	repairStartNativeOAuth = start
	repairNewLoginBrowser = func(context.Context) (loginBrowserController, error) { return &fakeLoginBrowser{}, nil }
	t.Cleanup(func() {
		repairListAccounts = oldInventory
		repairSafeQuarantine = oldQuarantine
		repairBindCallbackForwarder = oldBind
		repairStartNativeOAuth = oldStart
		repairPollNativeOAuth = oldPoll
		repairRefreshQuota = oldRefreshQuota
		repairNewLoginBrowser = oldNewBrowser
	})
}

func TestReviveBrowserUnavailableFailsBeforeQuarantine(t *testing.T) {
	db, _ := setupRepairRecoveryStore(t)
	jobID := "repair-browser-unavailable"
	seedRepairRecoveryState(t, db, jobID, "running", "queued", "")
	expected := account{Key: "repair-seat", AccountID: "account-a", Email: "seat@example.test", AuthPath: "/auth/seat.json", AuthDir: "/auth", Physical: true, Status: "authentication_error"}
	var quarantineCalls int
	installRepairTestDeps(t,
		func() ([]account, error) { return []account{expected}, nil },
		func(string, string, string) (string, error) { quarantineCalls++; return "unexpected", nil },
		func(context.Context, string, string) (nativeOAuthStart, error) {
			return nativeOAuthStart{}, errors.New("unexpected_oauth")
		},
	)
	repairNewLoginBrowser = func(context.Context) (loginBrowserController, error) {
		return nil, errors.New("private detail must be sanitized")
	}
	runReviveQueue(&reviveRuntime{jobID: jobID, queued: []account{expected}})
	job, err := readJob(jobID)
	if err != nil || job.State != "failed" || job.Reason != "browser_unavailable" {
		t.Fatalf("browser startup failure was not sanitized: job=%+v err=%v", job, err)
	}
	if quarantineCalls != 0 {
		t.Fatalf("browser startup failure quarantined %d files", quarantineCalls)
	}
	rows, err := loadRepairRows(jobID)
	if err != nil || len(rows) != 1 || rows[0].RepairState != "failed" || rows[0].Quarantine != "" {
		t.Fatalf("browser startup failure left unsafe row state: rows=%+v err=%v", rows, err)
	}
	poll := revivePoll(jobID)
	var body map[string]any
	if err := json.Unmarshal(poll.Body, &body); err != nil {
		t.Fatal(err)
	}
	if body["automatic"] != true || body["reason"] != "browser_unavailable" {
		t.Fatalf("terminal automatic failure was not readable after runtime cleanup: %v", body)
	}
}

func TestRevivePollExposesTransientAutomationStatusWithoutOAuthURL(t *testing.T) {
	db, _ := setupRepairRecoveryStore(t)
	jobID := "repair-automatic-status"
	seedRepairRecoveryState(t, db, jobID, "running", "queued", "")
	runtime := &reviveRuntime{jobID: jobID, automationStatus: "verification_code_waiting"}
	reviveRuntimeState.Lock()
	reviveRuntimeState.jobs[jobID] = runtime
	reviveRuntimeState.Unlock()
	t.Cleanup(func() {
		reviveRuntimeState.Lock()
		delete(reviveRuntimeState.jobs, jobID)
		reviveRuntimeState.Unlock()
	})

	poll := revivePoll(jobID)
	var body map[string]any
	if err := json.Unmarshal(poll.Body, &body); err != nil {
		t.Fatal(err)
	}
	if body["automatic"] != true || body["automation_status"] != "verification_code_waiting" {
		t.Fatalf("transient automatic status missing without OAuth URL: %v", body)
	}
	if _, ok := body["oauth_url"]; ok {
		t.Fatalf("poll invented an OAuth URL: %v", body)
	}
}

func waitForRepairJobState(t *testing.T, id, want string) stateResponse {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, err := readJob(id)
		if err == nil && job.State == want {
			return job
		}
		time.Sleep(5 * time.Millisecond)
	}
	job, err := readJob(id)
	t.Fatalf("job %q did not reach %q: job=%+v err=%v", id, want, job, err)
	return stateResponse{}
}

func TestAgentPollAndCodeShareAttempt(t *testing.T) {
	db, _ := setupRepairRecoveryStore(t)
	id := "agent-attempt"
	seedRepairRecoveryState(t, db, id, "running", "awaiting_user", "")
	requested := time.Now()
	r := &reviveRuntime{jobID: id, browserMode: reviveBrowserModeAgent, oauthURL: "https://auth.openai.com/oauth/authorize", verificationRequestedAt: requested, queued: []account{{Email: "fixture@example.test"}}}
	reviveRuntimeState.Lock()
	reviveRuntimeState.jobs[id] = r
	reviveRuntimeState.Unlock()
	old := repairDetectThunderbirdCode
	calls := 0
	repairDetectThunderbirdCode = func(email string, cutoff time.Time) (thunderbirdCode, error) {
		calls++
		if email != "fixture@example.test" || !cutoff.Equal(requested) {
			t.Fatal("wrong mailbox attempt")
		}
		return thunderbirdCode{Code: "123456", ReceivedAt: requested}, nil
	}
	t.Cleanup(func() {
		repairDetectThunderbirdCode = old
		reviveRuntimeState.Lock()
		delete(reviveRuntimeState.jobs, id)
		reviveRuntimeState.Unlock()
	})
	var poll, code map[string]any
	if err := json.Unmarshal(revivePoll(id).Body, &poll); err != nil {
		t.Fatal(err)
	}
	if poll["automatic"] != false || poll["attempt"] != formatReviveAttempt(requested) {
		t.Fatal("missing agent attempt")
	}
	if err := json.Unmarshal(reviveCode(id, poll["attempt"].(string)).Body, &code); err != nil {
		t.Fatal(err)
	}
	if code["attempt"] != poll["attempt"] || code["code"] != "123456" {
		t.Fatal("code attempt mismatch")
	}
	if err := json.Unmarshal(reviveCode(id, "previous").Body, &code); err != nil {
		t.Fatal(err)
	}
	if code["error"] != "stale_attempt" || calls != 1 {
		t.Fatal("stale attempt read mailbox")
	}
}

func TestReviveFailurePreservesQuarantineAcrossResume(t *testing.T) {
	db, _ := setupRepairRecoveryStore(t)
	jobID := "repair-preserve"
	marker := "quarantine-marker"
	seedRepairRecoveryState(t, db, jobID, "failed", "quarantined", marker)

	rows, err := loadRepairRows(jobID)
	if err != nil || len(rows) != 1 || rows[0].Quarantine != marker {
		t.Fatalf("initial quarantine marker was not loaded: rows=%+v err=%v", rows, err)
	}
	if err := updateRepairRow(jobID, 1, "failed", "oauth_start_failed"); err != nil {
		t.Fatal(err)
	}
	rows, err = loadRepairRows(jobID)
	if err != nil || len(rows) != 1 || rows[0].Quarantine != marker {
		t.Fatalf("failure cleared quarantine marker: rows=%+v err=%v", rows, err)
	}

	authDir := t.TempDir()
	predecessor := filepath.Join(authDir, "codex-old.json")
	if err := os.WriteFile(predecessor, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	expected := rows[0]
	// The persisted path is intentionally empty. Supplying a stale path only
	// makes an accidental second quarantine observable; the marker must be the
	// sole resume decision.
	expected.AuthPath, expected.AuthDir, expected.Physical = predecessor, authDir, true
	expected.Provider = "codex"
	var quarantineCalls int
	installRepairTestDeps(t,
		func() ([]account, error) { return []account{expected}, nil },
		func(string, string, string) (string, error) {
			quarantineCalls++
			return "", errors.New("safe_quarantine_must_not_run")
		},
		func(context.Context, string, string) (nativeOAuthStart, error) {
			return nativeOAuthStart{}, errors.New("oauth_start_failed")
		},
	)

	for attempt := 0; attempt < 2; attempt++ {
		runReviveQueue(&reviveRuntime{jobID: jobID, queued: []account{expected}})
		rows, err = loadRepairRows(jobID)
		if err != nil || len(rows) != 1 || rows[0].RepairState != "failed" || rows[0].Quarantine != marker {
			t.Fatalf("attempt %d lost durable recovery state: rows=%+v err=%v", attempt+1, rows, err)
		}
	}
	if quarantineCalls != 0 {
		t.Fatalf("already-quarantined row invoked safeQuarantine %d times", quarantineCalls)
	}
	if _, err := os.Stat(predecessor); err != nil {
		t.Fatalf("already-quarantined resume touched predecessor path: %v", err)
	}
}

func TestResumeLegacyAwaitingUserWithoutQuarantineMarker(t *testing.T) {
	db, _ := setupRepairRecoveryStore(t)
	jobID := "legacy-revive"
	seedRepairRecoveryState(t, db, jobID, "oauth_timeout", "awaiting_user", "")
	// Reopening applies the additive migration used by a real restart. Legacy
	// awaiting_user state is authoritative evidence that quarantine completed,
	// so it receives an opaque marker before resume ever resolves an auth path.
	closeStore()
	db, err := openStore()
	if err != nil {
		t.Fatal(err)
	}
	rows, err := loadRepairRows(jobID)
	if err != nil || len(rows) != 1 || rows[0].Quarantine != legacyQuarantineMarker {
		t.Fatalf("legacy marker migration failed: rows=%+v err=%v", rows, err)
	}
	var quarantineCalls int
	var oauthStarts int

	legacy := account{Key: "repair-seat", AuthID: "auth-id", AuthIndex: "auth-index", Email: "opaque-seat", AccountID: "account-id", Provider: "codex", Physical: true}
	installRepairTestDeps(t,
		func() ([]account, error) { return []account{legacy}, nil },
		func(string, string, string) (string, error) {
			quarantineCalls++
			return "", errors.New("safe_quarantine_must_not_run")
		},
		func(context.Context, string, string) (nativeOAuthStart, error) {
			oauthStarts++
			return nativeOAuthStart{}, errors.New("oauth_start_failed")
		},
	)

	for attempt := 0; attempt < 2; attempt++ {
		response, ok := resumeRevive(nil)
		if !ok || response.JobID != jobID || response.Total != 1 {
			t.Fatalf("legacy timeout state was not resumed on attempt %d: response=%+v ok=%v", attempt+1, response, ok)
		}
		job := waitForRepairJobState(t, jobID, "failed")
		if job.Reason != "oauth_start_failed" {
			t.Fatalf("unexpected resumed failure reason on attempt %d: %+v", attempt+1, job)
		}
		rows, err = loadRepairRows(jobID)
		if err != nil || len(rows) != 1 || rows[0].RepairState != "failed" || rows[0].Quarantine != legacyQuarantineMarker {
			t.Fatalf("legacy row recovery state changed on attempt %d: rows=%+v err=%v", attempt+1, rows, err)
		}
	}
	if oauthStarts != 2 {
		t.Fatalf("expected two failed OAuth attempts, got %d", oauthStarts)
	}
	if quarantineCalls != 0 {
		t.Fatalf("legacy awaiting_user row invoked safeQuarantine %d times", quarantineCalls)
	}
}
