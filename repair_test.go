package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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

func TestComposeOAuthURLModePreservesNativeLocalhostCallback(t *testing.T) {
	raw := "https://login.example.test/authorize?client_id=x&redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fauth%2Fcallback&state=s"
	got, err := composeOAuthURLMode(raw, "seat@example.test", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fauth%2Fcallback") {
		t.Fatalf("native localhost callback must be preserved: %q", got)
	}
	if strings.Contains(got, "redirect_uri=http%3A%2F%2F127.0.0.1%3A1455") {
		t.Fatalf("localhost callback was rewritten: %q", got)
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

func TestBeginReviveRejectsIncompletePrivateIdentitySnapshot(t *testing.T) {
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

	if _, err := beginRevive(nil); !errors.Is(err, errRepairIdentityUnavailable) {
		t.Fatalf("incomplete private-identity snapshot should fail closed: err=%v", err)
	}
	var jobs, rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM jobs`).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM repair_rows`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if jobs != 0 || rows != 0 {
		t.Fatalf("incomplete snapshot persisted partial queue: jobs=%d rows=%d", jobs, rows)
	}
}

func TestReviveRouteSanitizesIncompletePrivateIdentityFailure(t *testing.T) {
	_, _ = setupRepairRecoveryStore(t)
	installRepairTestDeps(t,
		func() ([]account, error) {
			return []account{{Key: "missing-private-id", AuthIndex: "missing-index", Email: "owner@example.test", Physical: true, Status: "authentication_error"}}, nil
		},
		func(string, string, string) (string, error) { return "unexpected-quarantine", nil },
		func(context.Context, string, string) (nativeOAuthStart, error) {
			return nativeOAuthStart{}, errors.New("unexpected-oauth")
		},
	)
	response := routeManagement(managementRequest{
		Method: http.MethodPost,
		Path:   "/v0/management/plugins/cpa-phoenix/revive",
		Body:   []byte(`{"acknowledge":"CPA_PHOENIX_ONE_CLICK","browser_mode":"agent"}`),
	})
	if response.StatusCode != http.StatusServiceUnavailable || string(response.Body) != `{"error":"inventory_unavailable"}` {
		t.Fatalf("incomplete identity response status=%d body=%s", response.StatusCode, response.Body)
	}
}

func TestRepairRowsOverlapRequiresExactPrivateIdentityAndEmail(t *testing.T) {
	oldRow := account{AccountID: " account-a ", Email: "Owner@Example.test"}
	if !repairRowsOverlap([]account{oldRow}, []account{{AccountID: "account-a", Email: " owner@example.test "}}) {
		t.Fatal("trimmed account ID and case-insensitive email should overlap")
	}
	if repairRowsOverlap([]account{oldRow}, []account{{AccountID: "account-a", Email: "other@example.test"}}) {
		t.Fatal("same private account with a different email must not overlap")
	}
	if repairRowsOverlap([]account{oldRow}, []account{{AccountID: "account-b", Email: "owner@example.test"}}) {
		t.Fatal("same email with a different private account must not overlap")
	}
	if repairRowsOverlap([]account{{AccountID: "", Email: oldRow.Email}}, []account{{AccountID: "", Email: oldRow.Email}}) {
		t.Fatal("missing private identity must never overlap")
	}
}

func TestRepairWorkspaceNameCarriesForwardByPrivateIdentity(t *testing.T) {
	db, _ := setupRepairRecoveryStore(t)
	now := time.Now().Unix()
	if _, err := db.Exec(`INSERT INTO jobs(id,kind,state,created_at,updated_at,total,done) VALUES(?,?,?,?,?,?,?)`, "named-history", "revive", "completed", now, now, 1, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO repair_rows(job_id,ordinal,account_key,email,account_id,workspace_name,state) VALUES(?,?,?,?,?,?,?)`, "named-history", 1, "old-key", "owner@example.test", "workspace-a", "Team Alpha", "repaired"); err != nil {
		t.Fatal(err)
	}
	id, err := globalJobs.startRevive([]account{{Key: "new-key", Email: "OWNER@example.test", AccountID: "workspace-a"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	updateJob(id, "completed", "", 1)
	rows, err := loadRepairRows(id)
	if err != nil || len(rows) != 1 || rows[0].WorkspaceName != "Team Alpha" {
		t.Fatalf("workspace name was not carried forward: rows=%+v err=%v", rows, err)
	}
}

func TestReviveRetriesWhenFirstWorkspaceCallbackIsNotTheQueuedSeat(t *testing.T) {
	db, _ := setupRepairRecoveryStore(t)
	jobID := "workspace-trial-retry"
	seedRepairRecoveryState(t, db, jobID, "running", "queued", "")
	expected := account{Key: "seat-a", AuthIndex: "index-a", AccountID: "workspace-a", Email: "owner@example.test", AuthPath: "/auth/a.json", AuthDir: "/auth", Physical: true, Status: "authentication_error"}
	if _, err := db.Exec(`UPDATE repair_rows SET account_id=?,auth_index=? WHERE job_id=? AND ordinal=1`, expected.AccountID, expected.AuthIndex, jobID); err != nil {
		t.Fatal(err)
	}
	wrong := account{Key: "wrong", AuthIndex: "wrong-index", AccountID: "workspace-wrong", Email: expected.Email, AuthPath: "/auth/wrong.json", AuthDir: "/auth", Physical: true, AccessTokenValue: "wrong-token"}
	right := expected
	right.Status = "active"
	right.AccessTokenValue = "right-token"
	oldInventory, oldStart, oldPoll, oldQuota, oldWait := repairListAccounts, repairStartNativeOAuth, repairPollNativeOAuth, repairRefreshQuota, repairReplacementWaitLimit
	listCalls := 0
	oauthStarts := 0
	repairListAccounts = func() ([]account, error) {
		listCalls++
		if listCalls <= 2 {
			return []account{expected}, nil
		}
		if listCalls <= 3 {
			return []account{wrong}, nil
		}
		return []account{right}, nil
	}
	repairStartNativeOAuth = func(context.Context, string, string) (nativeOAuthStart, error) {
		oauthStarts++
		return nativeOAuthStart{URL: "https://login.example.test/authorize", State: fmt.Sprintf("trial-%d", oauthStarts)}, nil
	}
	repairPollNativeOAuth = func(context.Context, string, string) (string, error) { return "success", nil }
	repairRefreshQuota = func(a account) account {
		if a.AccessTokenValue != "" {
			a.QuotaStatusCode = http.StatusOK
			a.Quota = quotaSnapshot{Windows: []quotaWindow{{Presence: windowPresent}}}
		}
		return a
	}
	repairReplacementWaitLimit = 5 * time.Millisecond
	t.Cleanup(func() {
		repairListAccounts, repairStartNativeOAuth, repairPollNativeOAuth, repairRefreshQuota, repairReplacementWaitLimit = oldInventory, oldStart, oldPoll, oldQuota, oldWait
	})
	runtime := &reviveRuntime{jobID: jobID, queued: []account{expected}, browserMode: reviveBrowserModeAgent}
	runReviveQueue(runtime)
	job, err := readJob(jobID)
	if err != nil || job.State != "completed" || job.Done != 1 || oauthStarts != 2 {
		t.Fatalf("workspace trial did not continue to the matching callback: job=%+v starts=%d err=%v", job, oauthStarts, err)
	}
}

func TestBeginReviveSupersedesDisjointTerminalQueueAndPreservesRows(t *testing.T) {
	db, _ := setupRepairRecoveryStore(t)
	oldID := "old-revive"
	now := time.Now().Unix()
	if _, err := db.Exec(`INSERT INTO jobs(id,kind,state,created_at,updated_at,total,reason) VALUES(?,?,?,?,?,?,?)`, oldID, "revive", "failed", now, now, 1, "old_failure"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO repair_rows(job_id,ordinal,account_key,auth_id,auth_index,email,account_id,state,reason,quarantine) VALUES(?,?,?,?,?,?,?,?,?,?)`, oldID, 1, "old-key", "old-auth", "old-index", "old@example.test", "old-account", "quarantined", "old_row_reason", "opaque-quarantine"); err != nil {
		t.Fatal(err)
	}
	current := []account{
		{Key: "new-key-a", AuthID: "new-auth-a", AuthIndex: "new-index-a", AccountID: "new-account-a", Email: "new-a@example.test", AuthPath: "/auth/new-a.json", AuthDir: "/auth", Physical: true, Status: "authentication_error", AccessTokenValue: "fixture-token"},
		{Key: "new-key-b", AuthID: "new-auth-b", AuthIndex: "new-index-b", AccountID: "new-account-b", Email: "new-b@example.test", AuthPath: "/auth/new-b.json", AuthDir: "/auth", Physical: true, Status: "authentication_error", AccessTokenValue: "fixture-token"},
	}
	installRepairTestDeps(t,
		func() ([]account, error) { return current, nil },
		func(string, string, string) (string, error) { return "unexpected-quarantine", nil },
		func(context.Context, string, string) (nativeOAuthStart, error) {
			return nativeOAuthStart{}, errors.New("unexpected-oauth")
		},
	)
	oldRefreshQuota := repairRefreshQuota
	oldBind := repairBindCallbackForwarder
	repairRefreshQuota = func(a account) account {
		a.QuotaStatusCode = http.StatusUnauthorized
		return a
	}
	repairBindCallbackForwarder = func() (*callbackForwarder, error) { return nil, errors.New("callback_port_unavailable") }
	t.Cleanup(func() {
		repairRefreshQuota = oldRefreshQuota
		repairBindCallbackForwarder = oldBind
	})

	response, err := beginRevive(nil, reviveBrowserModeAgent)
	if err != nil || response.Total != len(current) || response.JobID == oldID {
		t.Fatalf("disjoint current inventory did not create a fresh queue: response=%+v err=%v", response, err)
	}
	waitForRepairJobState(t, response.JobID, "failed")
	oldJob, err := readJob(oldID)
	if err != nil || oldJob.State != "superseded" || oldJob.Reason != "current_inventory_replaced_queue" {
		t.Fatalf("old queue was not durably superseded: job=%+v err=%v", oldJob, err)
	}
	oldRows, err := loadRepairRows(oldID)
	if err != nil || len(oldRows) != 1 || oldRows[0].RepairState != "quarantined" || oldRows[0].Quarantine != "opaque-quarantine" {
		t.Fatalf("superseding changed historical row/quarantine: rows=%+v err=%v", oldRows, err)
	}
	newRows, err := loadRepairRows(response.JobID)
	if err != nil || len(newRows) != len(current) || newRows[0].AccountID != current[0].AccountID || newRows[1].AccountID != current[1].AccountID {
		t.Fatalf("fresh queue did not preserve exact current identities: rows=%+v err=%v", newRows, err)
	}
	queue := incompleteRepairQueue()
	if len(queue) != len(current) {
		t.Fatalf("visible queue should project only fresh rows: queue=%v", queue)
	}
	for _, row := range queue {
		if row["email"] == "old@example.test" {
			t.Fatalf("superseded row leaked into visible queue: %v", queue)
		}
	}
}

func TestBeginReviveOverlappingTerminalQueueResumesWithoutSuperseding(t *testing.T) {
	db, _ := setupRepairRecoveryStore(t)
	oldID := "overlap-revive"
	now := time.Now().Unix()
	if _, err := db.Exec(`INSERT INTO jobs(id,kind,state,created_at,updated_at,total,reason) VALUES(?,?,?,?,?,?,?)`, oldID, "revive", "failed", now, now, 1, "old_failure"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO repair_rows(job_id,ordinal,account_key,auth_id,auth_index,email,account_id,state,quarantine) VALUES(?,?,?,?,?,?,?,?,?)`, oldID, 1, "old-key", "old-auth", "old-index", "owner@example.test", "account-a", "quarantined", "opaque-quarantine"); err != nil {
		t.Fatal(err)
	}
	current := account{Key: "new-key", AuthID: "new-auth", AuthIndex: "new-index", AccountID: "account-a", Email: " OWNER@example.test ", AuthPath: "/auth/current.json", AuthDir: "/auth", Physical: true, Status: "authentication_error", AccessTokenValue: "fixture-token"}
	installRepairTestDeps(t,
		func() ([]account, error) { return []account{current}, nil },
		func(string, string, string) (string, error) { return "unexpected-quarantine", nil },
		func(context.Context, string, string) (nativeOAuthStart, error) {
			return nativeOAuthStart{}, errors.New("unexpected-oauth")
		},
	)
	oldRefreshQuota := repairRefreshQuota
	oldBind := repairBindCallbackForwarder
	repairRefreshQuota = func(a account) account { a.QuotaStatusCode = http.StatusUnauthorized; return a }
	repairBindCallbackForwarder = func() (*callbackForwarder, error) { return nil, errors.New("callback_port_unavailable") }
	t.Cleanup(func() {
		repairRefreshQuota = oldRefreshQuota
		repairBindCallbackForwarder = oldBind
	})

	response, err := beginRevive(nil, reviveBrowserModeAgent)
	if err != nil || response.JobID != oldID {
		t.Fatalf("exact overlap should resume old queue: response=%+v err=%v", response, err)
	}
	waitForRepairJobState(t, oldID, "failed")
	job, err := readJob(oldID)
	if err != nil || job.State == "superseded" || job.Reason != "callback_port_unavailable" {
		t.Fatalf("overlap incorrectly superseded or lost failure state: job=%+v err=%v", job, err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE kind='revive'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("overlap created an unexpected second Revive job: count=%d", count)
	}
}

func TestBeginReviveWithNoCurrentInvalidWorkResumesTerminalQueue(t *testing.T) {
	db, _ := setupRepairRecoveryStore(t)
	oldID := "no-current-work"
	now := time.Now().Unix()
	if _, err := db.Exec(`INSERT INTO jobs(id,kind,state,created_at,updated_at,total,reason) VALUES(?,?,?,?,?,?,?)`, oldID, "revive", "failed", now, now, 1, "old_failure"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO repair_rows(job_id,ordinal,account_key,auth_id,auth_index,email,account_id,state,quarantine) VALUES(?,?,?,?,?,?,?,?,?)`, oldID, 1, "old-key", "old-auth", "old-index", "owner@example.test", "account-a", "quarantined", "opaque-quarantine"); err != nil {
		t.Fatal(err)
	}
	current := account{Key: "new-key", AuthID: "new-auth", AuthIndex: "new-index", AccountID: "account-a", Email: "owner@example.test", AuthPath: "/auth/current.json", AuthDir: "/auth", Physical: true, Status: "active", AccessTokenValue: "fixture-token"}
	installRepairTestDeps(t,
		func() ([]account, error) { return []account{current}, nil },
		func(string, string, string) (string, error) { return "unexpected-quarantine", nil },
		func(context.Context, string, string) (nativeOAuthStart, error) {
			return nativeOAuthStart{}, errors.New("unexpected-oauth")
		},
	)
	oldRefreshQuota := repairRefreshQuota
	repairRefreshQuota = func(a account) account { a.QuotaStatusCode = http.StatusOK; return a }
	t.Cleanup(func() { repairRefreshQuota = oldRefreshQuota })

	response, err := beginRevive(nil, reviveBrowserModeAgent)
	if err != nil || response.JobID != oldID {
		t.Fatalf("no fresh invalid work should resume old queue: response=%+v err=%v", response, err)
	}
	job := waitForRepairJobState(t, oldID, "completed")
	if job.Done != 1 {
		t.Fatalf("resumed healthy replacement did not complete old queue: %+v", job)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE kind='revive'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("no-work resume created an unexpected second Revive job: count=%d", count)
	}
}

func TestBeginReviveNeverProbesOrSupersedesActiveJob(t *testing.T) {
	_, _ = setupRepairRecoveryStore(t)
	id, err := globalJobs.start("revive", 1)
	if err != nil {
		t.Fatal(err)
	}
	probed := false
	oldInventory := repairListAccounts
	repairListAccounts = func() ([]account, error) {
		probed = true
		return nil, errors.New("active job must block inventory probe")
	}
	t.Cleanup(func() { repairListAccounts = oldInventory })
	if _, err := beginRevive(nil, reviveBrowserModeAgent); !errors.Is(err, errIgniteJobActive) {
		t.Fatalf("active job did not retain job lock: id=%q err=%v", id, err)
	}
	if probed {
		t.Fatal("active job triggered a fresh inventory probe")
	}
}

func TestSupersededReviveIsExcludedFromQueueAfterStoreReopen(t *testing.T) {
	db, store := setupRepairRecoveryStore(t)
	now := time.Now().Unix()
	if _, err := db.Exec(`INSERT INTO jobs(id,kind,state,created_at,updated_at,total,reason) VALUES(?,?,?,?,?,?,?)`, "superseded-latest", "revive", "superseded", now, now, 1, "current_inventory_replaced_queue"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO repair_rows(job_id,ordinal,account_key,email,account_id,state,quarantine) VALUES(?,?,?,?,?,?,?)`, "superseded-latest", 1, "old-key", "old@example.test", "old-account", "quarantined", "opaque-quarantine"); err != nil {
		t.Fatal(err)
	}
	if queue := incompleteRepairQueue(); len(queue) != 0 {
		t.Fatalf("superseded rows leaked from in-process projection: %v", queue)
	}
	closeStore()
	if _, err := openStore(); err != nil {
		t.Fatal(err)
	}
	if queue := incompleteRepairQueue(); len(queue) != 0 {
		t.Fatalf("superseded rows leaked after reopening %s: %v", store, queue)
	}
	if _, ok := resumeRevive(nil); ok {
		t.Fatal("superseded job remained resumable after reopening the store")
	}
}

func TestIncompleteRepairQueueProjectsOlderSkippedHistoryAfterLatestCompletes(t *testing.T) {
	db, _ := setupRepairRecoveryStore(t)
	now := time.Now().Unix()
	if _, err := db.Exec(`INSERT INTO jobs(id,kind,state,created_at,updated_at,total,done) VALUES(?,?,?,?,?,?,?)`, "old-skipped", "revive", "failed", now, now, 1, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO repair_rows(job_id,ordinal,account_key,email,account_id,state,quarantine) VALUES(?,?,?,?,?,?,?)`, "old-skipped", 1, "old-key", "old@example.test", "old-account", "skipped", "opaque-old"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO jobs(id,kind,state,created_at,updated_at,total,done) VALUES(?,?,?,?,?,?,?)`, "new-completed", "revive", "completed", now+1, now+1, 1, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO repair_rows(job_id,ordinal,account_key,email,account_id,state) VALUES(?,?,?,?,?,'repaired')`, "new-completed", 1, "new-key", "new@example.test", "new-account"); err != nil {
		t.Fatal(err)
	}
	queue := incompleteRepairQueue()
	if len(queue) != 1 || queue[0]["email"] != "old@example.test" || queue[0]["state"] != "skipped" || queue[0]["quarantined"] != true {
		t.Fatalf("older skipped history was not projected after latest completion: %v", queue)
	}
}

func TestIncompleteRepairQueuePrefersLatestPendingQueueOverOlderSkippedHistory(t *testing.T) {
	db, _ := setupRepairRecoveryStore(t)
	now := time.Now().Unix()
	if _, err := db.Exec(`INSERT INTO jobs(id,kind,state,created_at,updated_at,total,done) VALUES(?,?,?,?,?,?,?)`, "old-skipped-priority", "revive", "failed", now, now, 1, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO repair_rows(job_id,ordinal,account_key,email,account_id,state,quarantine) VALUES(?,?,?,?,?,?,?)`, "old-skipped-priority", 1, "old-key", "old@example.test", "old-account", "skipped", "opaque-old"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO jobs(id,kind,state,created_at,updated_at,total,done) VALUES(?,?,?,?,?,?,?)`, "new-pending", "revive", "failed", now+1, now+1, 1, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO repair_rows(job_id,ordinal,account_key,email,account_id,state,quarantine) VALUES(?,?,?,?,?,?,?)`, "new-pending", 1, "new-key", "new@example.test", "new-account", "failed", "opaque-new"); err != nil {
		t.Fatal(err)
	}
	queue := incompleteRepairQueue()
	if len(queue) != 2 || queue[0]["email"] != "new@example.test" || queue[0]["state"] != "failed" || queue[1]["email"] != "old@example.test" || queue[1]["state"] != "skipped" {
		t.Fatalf("all latest unresolved identities were not projected in order: %v", queue)
	}
}

func TestIncompleteRepairQueueSuppressesOlderIdentityAfterRepair(t *testing.T) {
	db, _ := setupRepairRecoveryStore(t)
	now := time.Now().Unix()
	if _, err := db.Exec(`INSERT INTO jobs(id,kind,state,created_at,updated_at,total,done) VALUES(?,?,?,?,?,?,?)`, "old-timeout", "revive", "failed", now, now, 1, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO repair_rows(job_id,ordinal,account_key,email,account_id,state,reason) VALUES(?,?,?,?,?,?,?)`, "old-timeout", 1, "old-key", "Owner@Example.test", "workspace-a", "skipped", "oauth_timeout"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO jobs(id,kind,state,created_at,updated_at,total,done) VALUES(?,?,?,?,?,?,?)`, "new-repaired", "revive", "completed", now+1, now+1, 1, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO repair_rows(job_id,ordinal,account_key,email,account_id,state,reason) VALUES(?,?,?,?,?,?,?)`, "new-repaired", 1, "new-key", "owner@example.test", "workspace-a", "repaired", ""); err != nil {
		t.Fatal(err)
	}
	if queue := incompleteRepairQueue(); len(queue) != 0 {
		t.Fatalf("older unresolved outcome leaked after exact identity was repaired: %v", queue)
	}
}

func TestLatestJobProjectionBreaksSecondResolutionTiesByCreationOrder(t *testing.T) {
	db, _ := setupRepairRecoveryStore(t)
	now := time.Now().Unix()
	for _, job := range []struct {
		id    string
		state string
	}{
		{id: "older-job", state: "superseded"},
		{id: "newer-job", state: "running"},
	} {
		if _, err := db.Exec(`INSERT INTO jobs(id,kind,state,created_at,updated_at,total) VALUES(?,?,?,?,?,?)`, job.id, "revive", job.state, now, now, 1); err != nil {
			t.Fatal(err)
		}
	}
	projection := latestJobProjection()
	if projection == nil || projection["id"] != "newer-job" {
		t.Fatalf("latest job projection did not use insertion tie-break: %+v", projection)
	}
}

func TestRepairJobSummaryAndQueueExposeUnresolvedReason(t *testing.T) {
	db, _ := setupRepairRecoveryStore(t)
	now := time.Now().Unix()
	if _, err := db.Exec(`INSERT INTO jobs(id,kind,state,created_at,updated_at,total,done) VALUES(?,?,?,?,?,?,?)`, "summary-job", "revive", "failed", now, now, 3, 1); err != nil {
		t.Fatal(err)
	}
	rows := []struct {
		ordinal int
		state   string
		reason  string
		quar    string
	}{
		{1, "repaired", "", ""},
		{2, "skipped", "provider_auth_error", ""},
		{3, "skipped", "oauth_timeout", "q-3"},
	}
	for _, row := range rows {
		if _, err := db.Exec(`INSERT INTO repair_rows(job_id,ordinal,email,account_id,account_key,state,reason,quarantine) VALUES(?,?,?,?,?,?,?,?)`, "summary-job", row.ordinal, fmt.Sprintf("seat-%d@example.test", row.ordinal), fmt.Sprintf("account-%d", row.ordinal), fmt.Sprintf("key-%d", row.ordinal), row.state, row.reason, row.quar); err != nil {
			t.Fatal(err)
		}
	}
	summary := repairJobSummary("summary-job")
	if summary["repaired"] != 1 || summary["skipped"] != 2 || summary["unresolved"] != 2 {
		t.Fatalf("unexpected repair summary: %#v", summary)
	}
	queue := incompleteRepairQueue()
	if len(queue) != 2 || queue[0]["reason"] != "provider_auth_error" || queue[1]["recoverable"] != true {
		t.Fatalf("queue did not expose unresolved reasons: %#v", queue)
	}
}

func TestAgentTimeoutWithoutQuarantineRemainsRecoverable(t *testing.T) {
	db, _ := setupRepairRecoveryStore(t)
	now := time.Now().Unix()
	if _, err := db.Exec(`INSERT INTO jobs(id,kind,state,created_at,updated_at,total,done) VALUES(?,?,?,?,?,?,?)`, "agent-timeout", "revive", "failed", now, now, 1, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO repair_rows(job_id,ordinal,email,account_id,account_key,state,reason,quarantine) VALUES(?,?,?,?,?,?,?,?)`, "agent-timeout", 1, "agent@example.test", "agent-account", "agent-key", "skipped", "oauth_timeout", ""); err != nil {
		t.Fatal(err)
	}
	queue := incompleteRepairQueue()
	if len(queue) != 1 || queue[0]["recoverable"] != true || queue[0]["quarantined"] != false {
		t.Fatalf("agent timeout should be retryable without quarantine: %v", queue)
	}
	snapshot, ok, err := latestRecoverableReviveJob()
	if err != nil || !ok || snapshot.ID != "agent-timeout" {
		t.Fatalf("recoverable historical timeout was not selected: snapshot=%+v ok=%v err=%v", snapshot, ok, err)
	}
}

func TestResetReviveQueueDismissesOnlyTerminalRecoverableJobs(t *testing.T) {
	db, _ := setupRepairRecoveryStore(t)
	now := time.Now().Unix()
	if _, err := db.Exec(`INSERT INTO jobs(id,kind,state,created_at,updated_at,total,done) VALUES(?,?,?,?,?,?,?)`, "resettable", "revive", "failed", now, now, 2, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO repair_rows(job_id,ordinal,email,account_id,account_key,state,reason) VALUES(?,?,?,?,?,?,?)`, "resettable", 1, "done@example.test", "done-account", "done-key", "repaired", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO repair_rows(job_id,ordinal,email,account_id,account_key,state,reason) VALUES(?,?,?,?,?,?,?)`, "resettable", 2, "skipped@example.test", "skipped-account", "skipped-key", "skipped", "oauth_timeout"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO jobs(id,kind,state,created_at,updated_at,total,done) VALUES(?,?,?,?,?,?,?)`, "blocked", "revive", "failed", now+1, now+1, 2, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO repair_rows(job_id,ordinal,email,account_id,account_key,state,reason) VALUES(?,?,?,?,?,?,?)`, "blocked", 1, "blocked@example.test", "blocked-account", "blocked-key", "skipped", "provider_auth_error"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO repair_rows(job_id,ordinal,email,account_id,account_key,state,reason) VALUES(?,?,?,?,?,?,'provider_auth_error')`, "blocked", 2, "blocked-two@example.test", "blocked-account-2", "blocked-key-2", "skipped"); err != nil {
		t.Fatal(err)
	}
	jobs, rows, err := resetReviveQueue()
	if err != nil || jobs != 1 || rows != 1 {
		t.Fatalf("unexpected reset result: jobs=%d rows=%d err=%v", jobs, rows, err)
	}
	job, err := readJob("resettable")
	if err != nil || job.State != "superseded" || job.Reason != "owner_reset" {
		t.Fatalf("resettable job was not dismissed safely: job=%+v err=%v", job, err)
	}
	job, err = readJob("blocked")
	if err != nil || job.State != "failed" {
		t.Fatalf("job with a queued row was reset: job=%+v err=%v", job, err)
	}
	queue := incompleteRepairQueue()
	if len(queue) != 2 || queue[0]["email"] != "blocked@example.test" || queue[1]["email"] != "blocked-two@example.test" {
		t.Fatalf("reset hid the wrong rows: queue=%v", queue)
	}
	var skipped int
	if err := db.QueryRow(`SELECT COUNT(*) FROM repair_rows WHERE job_id='resettable' AND state='skipped'`).Scan(&skipped); err != nil || skipped != 1 {
		t.Fatalf("reset deleted the audit row: skipped=%d err=%v", skipped, err)
	}
}

func TestResetReviveQueueRefusesActiveJobs(t *testing.T) {
	setupRepairRecoveryStore(t)
	id, err := globalJobs.start("revive", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer globalJobs.stopIf(id)
	if _, _, err := resetReviveQueue(); !errors.Is(err, errIgniteJobActive) {
		t.Fatalf("reset should not interrupt active repair: %v", err)
	}
}

func TestAutoDismissRecoverableSkippedJobLeavesProviderAuthQueueVisible(t *testing.T) {
	db, _ := setupRepairRecoveryStore(t)
	now := time.Now().Unix()
	if _, err := db.Exec(`INSERT INTO jobs(id,kind,state,created_at,updated_at,total,done) VALUES(?,?,?,?,?,?,?)`, "auto-reset", "revive", "running", now, now, 2, 1); err != nil {
		t.Fatal(err)
	}
	for ordinal, reason := range map[int]string{1: "oauth_timeout", 2: "workspace_missing"} {
		if _, err := db.Exec(`INSERT INTO repair_rows(job_id,ordinal,email,account_id,account_key,state,reason) VALUES(?,?,?,?,?,?,?)`, "auto-reset", ordinal, fmt.Sprintf("seat-%d@example.test", ordinal), fmt.Sprintf("account-%d", ordinal), fmt.Sprintf("key-%d", ordinal), "skipped", reason); err != nil {
			t.Fatal(err)
		}
	}
	if !autoDismissRecoverableSkippedJob("auto-reset") {
		t.Fatal("recoverable skipped rows should be auto-dismissable")
	}
	if _, err := db.Exec(`UPDATE jobs SET state='superseded',reason='auto_reset_recoverable_skips' WHERE id='auto-reset'`); err != nil {
		t.Fatal(err)
	}
	if queue := incompleteRepairQueue(); len(queue) != 0 {
		t.Fatalf("auto-dismissed recoverable queue remained visible: %v", queue)
	}
	if _, err := db.Exec(`INSERT INTO jobs(id,kind,state,created_at,updated_at,total,done) VALUES(?,?,?,?,?,?,?)`, "provider-auth", "revive", "running", now+1, now+1, 1, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO repair_rows(job_id,ordinal,email,account_id,account_key,state,reason) VALUES(?,?,?,?,?,?,?)`, "provider-auth", 1, "provider@example.test", "provider-account", "provider-key", "skipped", "provider_auth_error"); err != nil {
		t.Fatal(err)
	}
	if autoDismissRecoverableSkippedJob("provider-auth") {
		t.Fatal("provider-auth skips must remain unresolved")
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

func TestAgentReviveContinuesAfterTransientFailureWithoutQuarantine(t *testing.T) {
	db, _ := setupRepairRecoveryStore(t)
	jobID := "repair-agent-transient-continue"
	now := time.Now().Unix()
	if _, err := db.Exec(`INSERT INTO jobs(id,kind,state,created_at,updated_at,total,done) VALUES(?,?,?,?,?,?,?)`, jobID, "revive", "running", now, now, 2, 0); err != nil {
		t.Fatal(err)
	}
	first := account{Key: "seat-one", AuthID: "auth-one", AuthIndex: "index-one", AccountID: "workspace-one", Email: "one@example.test", AuthPath: "/auth/one.json", AuthDir: "/auth", Physical: true, Status: "authentication_error"}
	second := account{Key: "seat-two", AuthID: "auth-two", AuthIndex: "index-two", AccountID: "workspace-two", Email: "two@example.test", AuthPath: "/auth/two.json", AuthDir: "/auth", Physical: true, Status: "authentication_error"}
	for i, row := range []account{first, second} {
		if _, err := db.Exec(`INSERT INTO repair_rows(job_id,ordinal,account_key,auth_id,auth_index,email,account_id,state) VALUES(?,?,?,?,?,?,?,'queued')`, jobID, i+1, row.Key, row.AuthID, row.AuthIndex, row.Email, row.AccountID); err != nil {
			t.Fatal(err)
		}
	}
	secondReplacement := second
	secondReplacement.Status = "active"
	secondReplacement.AccessTokenValue = "replacement-token"
	secondReplacement.AccountID = "" // host.auth.list omits the private workspace ID.

	oldList, oldQuarantine, oldBind := repairListAccounts, repairSafeQuarantine, repairBindCallbackForwarder
	oldCapture := repairCaptureIdentity
	oldStart, oldStartMode, oldPoll, oldRefresh := repairStartNativeOAuth, repairStartNativeOAuthMode, repairPollNativeOAuth, repairRefreshQuota
	oldNativeRequest := nativeRequestFn
	t.Cleanup(func() {
		repairListAccounts, repairSafeQuarantine, repairBindCallbackForwarder = oldList, oldQuarantine, oldBind
		repairCaptureIdentity = oldCapture
		repairStartNativeOAuth, repairStartNativeOAuthMode, repairPollNativeOAuth, repairRefreshQuota = oldStart, oldStartMode, oldPoll, oldRefresh
		nativeRequestFn = oldNativeRequest
	})

	quarantineCalls := 0
	repairCaptureIdentity = func(a account) account {
		if a.Key == second.Key {
			a.AccountID = second.AccountID
		}
		return a
	}
	oauthStarts := 0
	repairListAccounts = func() ([]account, error) {
		if oauthStarts < 2 {
			return []account{first, second}, nil
		}
		return []account{first, secondReplacement}, nil
	}
	repairSafeQuarantine = func(string, string, string) (string, error) {
		quarantineCalls++
		return "unexpected-quarantine", nil
	}
	repairBindCallbackForwarder = func() (*callbackForwarder, error) { return &callbackForwarder{}, nil }
	startOAuth := func(context.Context, string, string, bool) (nativeOAuthStart, error) {
		oauthStarts++
		return nativeOAuthStart{URL: "https://login.example.test/authorize", State: "state-" + strconv.Itoa(oauthStarts)}, nil
	}
	repairStartNativeOAuthMode = startOAuth
	repairStartNativeOAuth = func(ctx context.Context, authHeader, email string) (nativeOAuthStart, error) {
		return startOAuth(ctx, authHeader, email, false)
	}
	repairPollNativeOAuth = func(_ context.Context, _ string, state string) (string, error) {
		if state == "state-1" {
			return "", errors.New("oauth_poll_failed")
		}
		return "success", nil
	}
	repairRefreshQuota = func(a account) account {
		if a.AccessTokenValue != "" {
			a.QuotaStatusCode = http.StatusOK
			a.Quota = quotaSnapshot{Windows: []quotaWindow{{Presence: windowPresent}}}
		} else {
			a.QuotaStatusCode = http.StatusUnauthorized
		}
		return a
	}
	nativeRequestFn = func(_ context.Context, _ string, _ string, _ string, _ url.Values) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	}

	runReviveQueue(&reviveRuntime{jobID: jobID, queued: []account{first, second}, browserMode: reviveBrowserModeAgent})
	job, err := readJob(jobID)
	if err != nil || job.State != "failed" || job.Done != 1 || job.Reason != "oauth_poll_failed" {
		t.Fatalf("transient failure did not allow later repair: job=%+v err=%v", job, err)
	}
	rows, err := loadRepairRows(jobID)
	if err != nil || len(rows) != 2 || rows[0].RepairState != "failed" || rows[1].RepairState != "repaired" {
		t.Fatalf("unexpected row states after transient continuation: rows=%+v err=%v", rows, err)
	}
	if rows[0].Quarantine != "" || quarantineCalls != 0 {
		t.Fatalf("agent transient failure quarantined the row: rows=%+v calls=%d", rows, quarantineCalls)
	}
	// Agent mode must not activate or scan a desktop mail client.
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
			if mode == reviveBrowserModeAgent {
				return []account{firstReplacement, second}, nil
			}
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

func TestReviveQueueStopsPromptlyOnProviderAuthError(t *testing.T) {
	db, _ := setupRepairRecoveryStore(t)
	jobID := "repair-provider-auth-error"
	seedRepairRecoveryState(t, db, jobID, "running", "queued", "")
	expected := account{Key: "repair-seat", AccountID: "account-id", Email: "opaque-seat", AuthPath: "/auth/seat.json", AuthDir: "/auth", Physical: true, Status: "authentication_error"}

	inventoryCalls := 0
	installRepairTestDeps(t,
		func() ([]account, error) {
			inventoryCalls++
			if inventoryCalls == 1 {
				return []account{expected}, nil
			}
			return nil, nil
		},
		func(string, string, string) (string, error) { return "quarantine-marker", nil },
		func(context.Context, string, string) (nativeOAuthStart, error) {
			return nativeOAuthStart{URL: "https://login.example.test/authorize", State: "provider-error-state"}, nil
		},
	)
	repairRefreshQuota = func(a account) account { return a }
	pollCalls := 0
	repairPollNativeOAuth = func(context.Context, string, string) (string, error) {
		pollCalls++
		if pollCalls == 1 {
			return "wait", nil
		}
		return "", errors.New("test_poll_sentinel")
	}
	browser := &fakeLoginBrowser{run: func(_ context.Context, _ string, _ string, _ string, _ time.Time, report func(string)) error {
		report("provider_auth_error")
		return errors.New("provider_auth_error")
	}}
	repairNewLoginBrowser = func(context.Context) (loginBrowserController, error) { return browser, nil }

	oldNativeRequest := nativeRequestFn
	cancelCalls := 0
	nativeRequestFn = func(_ context.Context, method, path, _ string, query url.Values) (*http.Response, error) {
		if method == http.MethodDelete && path == "/v0/management/oauth-session" && query.Get("state") == "provider-error-state" {
			cancelCalls++
		}
		return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	t.Cleanup(func() { nativeRequestFn = oldNativeRequest })

	started := time.Now()
	runReviveQueue(&reviveRuntime{jobID: jobID, queued: []account{expected}})
	if elapsed := time.Since(started); elapsed >= 3*time.Second {
		t.Fatalf("provider authentication failure waited too long: %v", elapsed)
	}
	job, err := readJob(jobID)
	if err != nil || job.State != "failed" || job.Reason != "provider_auth_error" || job.Done != 0 {
		t.Fatalf("provider authentication failure was not terminal and sanitized: job=%+v err=%v", job, err)
	}
	rows, err := loadRepairRows(jobID)
	if err != nil || len(rows) != 1 || rows[0].RepairState != "skipped" || rows[0].Quarantine != "quarantine-marker" {
		t.Fatalf("provider authentication failure was not auto-skipped with quarantine preserved: rows=%+v err=%v", rows, err)
	}
	if cancelCalls != 1 {
		t.Fatalf("provider authentication failure cancelled native OAuth %d times", cancelCalls)
	}
	if sanitizeAutomationStatus("provider_auth_error") != "provider_auth_error" || sanitizeAutomationStatus("Authentication Error: private detail") != "browser_automation_failed" {
		t.Fatal("provider status sanitization leaked raw provider text")
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
	old := repairDetectCode
	calls := 0
	repairDetectCode = func(email string, cutoff time.Time) (thunderbirdCode, error) {
		calls++
		if email != "fixture@example.test" || !cutoff.Equal(requested) {
			t.Fatal("wrong mailbox attempt")
		}
		// IMAP Date headers have second-level precision. Phoenix must accept the
		// same bounded tolerance as Agent Autofill when the request starts just
		// after the message's recorded second.
		return thunderbirdCode{Code: "123456", ReceivedAt: requested.Add(-time.Second), Recipient: email}, nil
	}
	t.Cleanup(func() {
		repairDetectCode = old
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
	attempt := poll["attempt"].(string)
	if err := json.Unmarshal(reviveCode(id, attempt).Body, &code); err != nil {
		t.Fatal(err)
	}
	if code["attempt"] != attempt || code["code"] != "123456" {
		t.Fatal("code attempt mismatch")
	}
	if err := json.Unmarshal(reviveCode(id, attempt).Body, &code); err != nil {
		t.Fatal(err)
	}
	if code["attempt"] != attempt || code["code"] != "123456" || calls != 1 {
		t.Fatalf("positive code was not cached: code=%v calls=%d", code, calls)
	}
	missingAttempt := routeManagement(managementRequest{Method: http.MethodPost, Path: "/v0/management/plugins/cpa-phoenix/revive/code", Query: map[string][]string{"id": {id}}})
	if missingAttempt.StatusCode != http.StatusBadRequest || !strings.Contains(string(missingAttempt.Body), `"attempt_required"`) {
		t.Fatalf("missing attempt status=%d body=%s", missingAttempt.StatusCode, missingAttempt.Body)
	}
	routed := routeManagement(managementRequest{Method: http.MethodPost, Path: "/v0/management/plugins/cpa-phoenix/revive/code", Query: map[string][]string{"id": {id}, "attempt": {attempt}}})
	if routed.StatusCode != http.StatusOK || !strings.Contains(string(routed.Body), `"code":"123456"`) || calls != 1 {
		t.Fatalf("route did not forward the current attempt or reuse cache: status=%d body=%s calls=%d", routed.StatusCode, routed.Body, calls)
	}
	if err := json.Unmarshal(reviveCode(id, "previous").Body, &code); err != nil {
		t.Fatal(err)
	}
	if code["error"] != "stale_attempt" || calls != 1 {
		t.Fatal("stale attempt read mailbox")
	}
}

func TestReviveCodeNegativeResultIsCachedUntilExpiry(t *testing.T) {
	db, _ := setupRepairRecoveryStore(t)
	id := "agent-negative-cache"
	seedRepairRecoveryState(t, db, id, "running", "awaiting_user", "")
	requested := time.Now().Add(-time.Second)
	runtime := &reviveRuntime{jobID: id, browserMode: reviveBrowserModeAgent, verificationRequestedAt: requested, queued: []account{{Email: "fixture@example.test"}}}
	reviveRuntimeState.Lock()
	reviveRuntimeState.jobs[id] = runtime
	reviveRuntimeState.Unlock()
	old := repairDetectCode
	calls := 0
	repairDetectCode = func(string, time.Time) (thunderbirdCode, error) {
		calls++
		return thunderbirdCode{}, errAgentAutofillNoCode
	}
	t.Cleanup(func() {
		repairDetectCode = old
		reviveRuntimeState.Lock()
		delete(reviveRuntimeState.jobs, id)
		reviveRuntimeState.Unlock()
	})
	attempt := formatReviveAttempt(requested)
	first := reviveCode(id, attempt)
	second := reviveCode(id, attempt)
	if first.StatusCode != http.StatusOK || second.StatusCode != http.StatusOK || calls != 1 || !strings.Contains(string(second.Body), `"verification_code_not_found"`) {
		t.Fatalf("negative result was not cached: first=%s second=%s calls=%d", first.Body, second.Body, calls)
	}
	runtime.mu.Lock()
	runtime.codeProbe.checkedAt = time.Now().Add(-reviveCodeNegativeTTL - time.Millisecond)
	runtime.mu.Unlock()
	third := reviveCode(id, attempt)
	if third.StatusCode != http.StatusOK || calls != 2 {
		t.Fatalf("expired negative result did not refresh: body=%s calls=%d", third.Body, calls)
	}
}

func TestReviveSkipRejectsStaleAttemptAndAutomaticMode(t *testing.T) {
	db, _ := setupRepairRecoveryStore(t)
	jobID := "skip-contract"
	seedRepairRecoveryState(t, db, jobID, "awaiting_user", "awaiting_user", "opaque-quarantine")
	requested := time.Now().UTC()
	runtime := &reviveRuntime{
		jobID:                   jobID,
		browserMode:             reviveBrowserModeAgent,
		current:                 0,
		oauthURL:                "https://login.example.test/authorize",
		oauthState:              "native-state",
		verificationRequestedAt: requested,
		queued:                  []account{{AccountID: "account-id", Email: "opaque-seat", RepairState: "awaiting_user", Quarantine: "opaque-quarantine"}},
	}
	reviveRuntimeState.Lock()
	reviveRuntimeState.jobs[jobID] = runtime
	reviveRuntimeState.Unlock()
	t.Cleanup(func() {
		reviveRuntimeState.Lock()
		delete(reviveRuntimeState.jobs, jobID)
		reviveRuntimeState.Unlock()
	})

	oldNativeRequest := nativeRequestFn
	cancelCalls := 0
	nativeRequestFn = func(_ context.Context, method, path, _ string, query url.Values) (*http.Response, error) {
		if method == http.MethodDelete && path == "/v0/management/oauth-session" && query.Get("state") == "native-state" {
			cancelCalls++
		}
		return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	t.Cleanup(func() { nativeRequestFn = oldNativeRequest })

	staleBody, _ := json.Marshal(map[string]string{"job_id": jobID, "attempt": "old-attempt", "reason": reviveSkipProviderAuthReason})
	response := routeManagement(managementRequest{Method: http.MethodPost, Path: "/v0/management/plugins/cpa-phoenix/revive/skip", Body: staleBody})
	if response.StatusCode != http.StatusConflict || !strings.Contains(string(response.Body), `"stale_attempt"`) {
		t.Fatalf("stale skip status=%d body=%s", response.StatusCode, response.Body)
	}
	rows, err := loadRepairRows(jobID)
	if err != nil || len(rows) != 1 || rows[0].RepairState != "awaiting_user" || rows[0].Quarantine != "opaque-quarantine" {
		t.Fatalf("stale skip changed durable row: rows=%+v err=%v", rows, err)
	}
	if cancelCalls != 0 {
		t.Fatalf("stale skip cancelled native OAuth %d times", cancelCalls)
	}

	runtime.mu.Lock()
	runtime.browserMode = reviveBrowserModeAutomatic
	attempt := formatReviveAttempt(runtime.verificationRequestedAt)
	runtime.mu.Unlock()
	validBody, _ := json.Marshal(map[string]string{"job_id": jobID, "attempt": attempt, "reason": reviveSkipProviderAuthReason})
	response = routeManagement(managementRequest{Method: http.MethodPost, Path: "/v0/management/plugins/cpa-phoenix/revive/skip", Body: validBody})
	if response.StatusCode != http.StatusConflict || !strings.Contains(string(response.Body), `"agent_mode_required"`) {
		t.Fatalf("automatic-mode skip status=%d body=%s", response.StatusCode, response.Body)
	}
	rows, err = loadRepairRows(jobID)
	if err != nil || len(rows) != 1 || rows[0].RepairState != "awaiting_user" || rows[0].Quarantine != "opaque-quarantine" {
		t.Fatalf("automatic-mode skip changed durable row: rows=%+v err=%v", rows, err)
	}
}

func TestReviveSkipAdvancesQueueAndPreservesQuarantine(t *testing.T) {
	db, _ := setupRepairRecoveryStore(t)
	jobID := "skip-advance"
	now := time.Now().Unix()
	if _, err := db.Exec(`INSERT INTO jobs(id,kind,state,created_at,updated_at,total,done) VALUES(?,?,?,?,?,?,?)`, jobID, "revive", "running", now, now, 2, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO repair_rows(job_id,ordinal,account_key,email,account_id,state,quarantine) VALUES(?,?,?,?,?,?,?)`, jobID, 1, "seat-one", "one@example.test", "account-one", "quarantined", "quarantine-one"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO repair_rows(job_id,ordinal,account_key,email,account_id,state,quarantine) VALUES(?,?,?,?,?,?,?)`, jobID, 2, "seat-two", "two@example.test", "account-two", "quarantined", "quarantine-two"); err != nil {
		t.Fatal(err)
	}
	expectedOne := account{Key: "seat-one", AccountID: "account-one", Email: "one@example.test", Physical: true, Status: "authentication_error", RepairState: "quarantined", Quarantine: "quarantine-one"}
	expectedTwo := account{Key: "seat-two", AccountID: "account-two", Email: "two@example.test", Physical: true, Status: "authentication_error", RepairState: "quarantined", Quarantine: "quarantine-two"}
	installRepairTestDeps(t,
		func() ([]account, error) { return []account{expectedOne, expectedTwo}, nil },
		func(string, string, string) (string, error) { return "unexpected-quarantine", nil },
		func(context.Context, string, string) (nativeOAuthStart, error) {
			t.Fatalf("skipping the first row must advance to the next row without starting another OAuth")
			return nativeOAuthStart{}, nil
		},
	)
	firstPoll := make(chan struct{})
	var pollOnce sync.Once
	secondPoll := 0
	repairPollNativeOAuth = func(ctx context.Context, _ string, state string) (string, error) {
		if state != "native-state" {
			secondPoll++
			return "", errors.New("unexpected_second_oauth")
		}
		pollOnce.Do(func() { close(firstPoll) })
		<-ctx.Done()
		return "", ctx.Err()
	}
	repairRefreshQuota = func(a account) account {
		if a.AccountID == expectedTwo.AccountID {
			a.QuotaStatusCode = http.StatusOK
		}
		return a
	}
	repairStartNativeOAuth = func(context.Context, string, string) (nativeOAuthStart, error) {
		return nativeOAuthStart{URL: "https://login.example.test/authorize", State: "native-state"}, nil
	}
	oldNativeRequest := nativeRequestFn
	cancelCalls := 0
	nativeRequestFn = func(_ context.Context, method, path, _ string, query url.Values) (*http.Response, error) {
		if method == http.MethodDelete && path == "/v0/management/oauth-session" && query.Get("state") == "native-state" {
			cancelCalls++
		}
		return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	t.Cleanup(func() { nativeRequestFn = oldNativeRequest })

	runtime := &reviveRuntime{jobID: jobID, browserMode: reviveBrowserModeAgent, queued: []account{expectedOne, expectedTwo}}
	reviveRuntimeState.Lock()
	reviveRuntimeState.jobs[jobID] = runtime
	reviveRuntimeState.Unlock()
	go runReviveQueue(runtime)
	select {
	case <-firstPoll:
	case <-time.After(2 * time.Second):
		t.Fatal("first OAuth attempt did not reach native poll")
	}
	deadline := time.Now().Add(2 * time.Second)
	var attempt string
	for time.Now().Before(deadline) {
		runtime.mu.Lock()
		attempt = formatReviveAttempt(runtime.verificationRequestedAt)
		activeState := runtime.oauthState
		runtime.mu.Unlock()
		if activeState == "native-state" && attempt != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if attempt == "" {
		t.Fatal("first OAuth attempt did not publish an attempt token")
	}
	body, _ := json.Marshal(map[string]string{"job_id": jobID, "attempt": attempt, "reason": reviveSkipProviderAuthReason})
	response := routeManagement(managementRequest{Method: http.MethodPost, Path: "/v0/management/plugins/cpa-phoenix/revive/skip", Body: body})
	if response.StatusCode != http.StatusOK || !strings.Contains(string(response.Body), `"skipped"`) {
		t.Fatalf("skip status=%d body=%s", response.StatusCode, response.Body)
	}
	job := waitForRepairJobState(t, jobID, "failed")
	if job.Reason != reviveSkipProviderAuthReason || job.Done != 1 {
		t.Fatalf("skip queue terminal state=%+v", job)
	}
	rows, err := loadRepairRows(jobID)
	if err != nil || len(rows) != 2 {
		t.Fatalf("load skipped queue rows=%+v err=%v", rows, err)
	}
	if rows[0].RepairState != "skipped" || rows[0].Quarantine != "quarantine-one" || rows[1].RepairState != "repaired" || rows[1].Quarantine != "quarantine-two" {
		t.Fatalf("skip did not preserve/advance exact rows: rows=%+v", rows)
	}
	if cancelCalls != 1 || secondPoll != 0 {
		t.Fatalf("skip cancelled wrong native attempt or retried OAuth: cancels=%d secondPoll=%d", cancelCalls, secondPoll)
	}
	queue := incompleteRepairQueue()
	if len(queue) != 1 || queue[0]["state"] != "skipped" || queue[0]["quarantined"] != true {
		t.Fatalf("skipped queue projection lost unresolved quarantine: %v", queue)
	}
}

func TestReviveSkipsPreservedQuarantineWhenInventoryIsEmpty(t *testing.T) {
	db, _ := setupRepairRecoveryStore(t)
	jobID := "skip-retry-empty-inventory"
	seedRepairRecoveryState(t, db, jobID, "failed", "skipped", "quarantine-marker")
	expected := account{Key: "repair-seat", AccountID: "account-id", Email: "opaque-seat", Physical: true, Status: "authentication_error", RepairState: "skipped", Quarantine: "quarantine-marker"}
	inventoryCalls := 0
	oauthStarts := 0
	installRepairTestDeps(t,
		func() ([]account, error) {
			inventoryCalls++
			if inventoryCalls == 1 {
				// The seat disappeared from the live inventory after it was
				// quarantined. The preserved row remains the retry authority.
				return nil, nil
			}
			return []account{expected}, nil
		},
		func(string, string, string) (string, error) {
			t.Fatalf("retrying a quarantined row must not quarantine again")
			return "", nil
		},
		func(context.Context, string, string) (nativeOAuthStart, error) {
			oauthStarts++
			return nativeOAuthStart{URL: "https://login.example.test/authorize", State: "retry-state"}, nil
		},
	)
	repairRefreshQuota = func(a account) account { return a }
	repairPollNativeOAuth = func(context.Context, string, string) (string, error) {
		return "", errors.New("retry_poll_failed")
	}
	oldNativeRequest := nativeRequestFn
	nativeRequestFn = func(_ context.Context, method, path, _ string, query url.Values) (*http.Response, error) {
		if method != http.MethodDelete || path != "/v0/management/oauth-session" || query.Get("state") != "retry-state" {
			t.Fatalf("unexpected native request during skipped-row retry: method=%q path=%q query=%v", method, path, query)
		}
		return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	t.Cleanup(func() { nativeRequestFn = oldNativeRequest })

	response, err := beginRevive(nil, reviveBrowserModeAgent)
	if !errors.Is(err, errNoActionableAccounts) || response.JobID != "" || oauthStarts != 0 {
		t.Fatalf("ordinary Revive retried a preserved skipped row: response=%+v err=%v oauthStarts=%d", response, err, oauthStarts)
	}
	rows, err := loadRepairRows(jobID)
	if err != nil || len(rows) != 1 || rows[0].RepairState != "skipped" || rows[0].Quarantine != "quarantine-marker" {
		t.Fatalf("ordinary Revive changed skipped row quarantine/state: rows=%+v err=%v", rows, err)
	}
}

func TestExplicitReviveStartsFreshDisjointQueueWithoutSupersedingSkippedHistory(t *testing.T) {
	db, _ := setupRepairRecoveryStore(t)
	oldID := "skip-history-disjoint"
	seedRepairRecoveryState(t, db, oldID, "failed", "skipped", "quarantine-old")
	current := account{Key: "current-seat", AccountID: "current-account", Email: "current@example.test", Physical: true, Status: "authentication_error"}
	installRepairTestDeps(t,
		func() ([]account, error) { return []account{current}, nil },
		func(string, string, string) (string, error) {
			t.Fatalf("disjoint fresh queue must not touch the historical quarantine")
			return "", nil
		},
		func(context.Context, string, string) (nativeOAuthStart, error) {
			t.Fatalf("current row has no exact auth path and should fail before OAuth")
			return nativeOAuthStart{}, nil
		},
	)
	response, err := beginRevive(nil, reviveBrowserModeAgent)
	if err != nil || response.JobID == "" || response.JobID == oldID {
		t.Fatalf("disjoint actionable inventory did not start a fresh queue: response=%+v err=%v", response, err)
	}
	waitForRepairJobState(t, response.JobID, "failed")
	oldJob, err := readJob(oldID)
	if err != nil || oldJob.State != "failed" {
		t.Fatalf("historical skipped job changed state: job=%+v err=%v", oldJob, err)
	}
	oldRows, err := loadRepairRows(oldID)
	if err != nil || len(oldRows) != 1 || oldRows[0].RepairState != "skipped" || oldRows[0].Quarantine != "quarantine-old" {
		t.Fatalf("historical skipped quarantine was not preserved: rows=%+v err=%v", oldRows, err)
	}
}

func TestReviveDoesNotReopenOlderSkippedHistory(t *testing.T) {
	db, _ := setupRepairRecoveryStore(t)
	oldID := "older-skipped-history"
	seedRepairRecoveryState(t, db, oldID, "failed", "skipped", "quarantine-old")
	newID := "newer-completed-queue"
	now := time.Now().Unix()
	if _, err := db.Exec(`INSERT INTO jobs(id,kind,state,created_at,updated_at,total,done) VALUES(?,?,?,?,?,?,?)`, newID, "revive", "completed", now+1, now+1, 1, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO repair_rows(job_id,ordinal,account_key,email,account_id,state) VALUES(?,?,?,?,?,'repaired')`, newID, 1, "new-seat", "new@example.test", "new-account"); err != nil {
		t.Fatal(err)
	}
	expected := account{Key: "repair-seat", AccountID: "account-id", Email: "opaque-seat", Physical: true, Status: "authentication_error", RepairState: "skipped", Quarantine: "quarantine-old"}
	inventoryCalls := 0
	oauthStarts := 0
	installRepairTestDeps(t,
		func() ([]account, error) {
			inventoryCalls++
			if inventoryCalls == 1 {
				return nil, nil // no current actionable rows; only historical skip remains
			}
			return []account{expected}, nil
		},
		func(string, string, string) (string, error) {
			t.Fatalf("historical skipped retry must not quarantine again")
			return "", nil
		},
		func(context.Context, string, string) (nativeOAuthStart, error) {
			oauthStarts++
			return nativeOAuthStart{URL: "https://login.example.test/authorize", State: "historical-retry-state"}, nil
		},
	)
	repairRefreshQuota = func(a account) account { return a }
	repairPollNativeOAuth = func(context.Context, string, string) (string, error) {
		return "", errors.New("historical_retry_poll_failed")
	}
	oldNativeRequest := nativeRequestFn
	nativeRequestFn = func(_ context.Context, method, path, _ string, query url.Values) (*http.Response, error) {
		if method != http.MethodDelete || path != "/v0/management/oauth-session" || query.Get("state") != "historical-retry-state" {
			t.Fatalf("unexpected native request during historical retry: method=%q path=%q query=%v", method, path, query)
		}
		return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	t.Cleanup(func() { nativeRequestFn = oldNativeRequest })

	response, err := beginRevive(nil, reviveBrowserModeAgent)
	if !errors.Is(err, errNoActionableAccounts) || response.JobID != "" || oauthStarts != 0 {
		t.Fatalf("ordinary Revive reopened older skipped history: response=%+v err=%v oauthStarts=%d", response, err, oauthStarts)
	}
	rows, err := loadRepairRows(oldID)
	if err != nil || len(rows) != 1 || rows[0].RepairState != "skipped" || rows[0].Quarantine != "quarantine-old" {
		t.Fatalf("ordinary Revive changed older skipped quarantine/state: rows=%+v err=%v", rows, err)
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

func TestReviveAutoSkipStateOnlySkipsProviderAndTimeout(t *testing.T) {
	for _, state := range []string{reviveSkipProviderAuthReason, "oauth_timeout"} {
		if !reviveAutoSkipState(state) {
			t.Fatalf("%q must auto-skip so one blocked attempt cannot stop later rows", state)
		}
	}
	for _, state := range []string{"oauth_poll_failed", "replacement_not_queued", "manual_captcha_required"} {
		if reviveAutoSkipState(state) {
			t.Fatalf("%q must remain resumable or require intervention", state)
		}
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
