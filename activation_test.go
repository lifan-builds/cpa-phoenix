package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDefaultUpstreamRequestUsesProductionContractAndNarrowRetry(t *testing.T) {
	var bodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method=%q want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer fixture-token" {
			t.Errorf("authorization=%q", got)
		}
		if got := r.Header.Get("Chatgpt-Account-Id"); got != "fixture-account" {
			t.Errorf("account id=%q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("content type=%q", got)
		}
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, body)
		if bytes.Contains(body, []byte("fixture-token")) || bytes.Contains(body, []byte("fixture-account")) {
			t.Errorf("credential leaked into request body: %s", body)
		}
		if r.Header.Get("Accept") != "text/event-stream" || r.Header.Get("Originator") != "codex-tui" || r.Header.Get("Connection") != "Keep-Alive" {
			t.Errorf("production headers missing: accept=%q originator=%q connection=%q", r.Header.Get("Accept"), r.Header.Get("Originator"), r.Header.Get("Connection"))
		}
		if len(bodies) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":"unknown_parameter","param":"stream"}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	old := codexCompactURLForTest
	codexCompactURLForTest = server.URL
	defer func() { codexCompactURLForTest = old }()
	a := account{AccessTokenValue: "fixture-token", AccountID: "fixture-account"}
	status, err := defaultUpstreamRequest(context.Background(), a, probeRequest)
	if err != nil || status != http.StatusOK || len(bodies) != 2 {
		t.Fatalf("status=%d err=%v calls=%d", status, err, len(bodies))
	}
	var first, second map[string]any
	if err := json.Unmarshal(bodies[0], &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(bodies[1], &second); err != nil {
		t.Fatal(err)
	}
	if first["stream"] != true || first["store"] != false {
		t.Fatalf("first body=%s", bodies[0])
	}
	if _, ok := second["stream"]; ok {
		t.Fatalf("compatibility body retained stream: %s", bodies[1])
	}
	if !bytes.Equal(bodies[1], minimalRequest) {
		t.Fatalf("minimal body=%s want=%s", bodies[1], minimalRequest)
	}
}

func TestDefaultUpstreamRequestDoesNotRetryUnrelatedUnknownParameter(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"unknown_parameter","param":"temperature"}}`))
	}))
	defer server.Close()
	old := codexCompactURLForTest
	codexCompactURLForTest = server.URL
	defer func() { codexCompactURLForTest = old }()
	status, err := defaultUpstreamRequest(context.Background(), account{AccessTokenValue: "fixture-token"}, probeRequest)
	if err != nil || status != http.StatusBadRequest || calls != 1 {
		t.Fatalf("status=%d err=%v calls=%d; unrelated unknown parameter must not retry", status, err, calls)
	}
}

func TestFreshQuotaRequiresEveryReportedWindow(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	zero := float64(0)
	full := int64(3600)
	reset := now.Unix() + full
	q := quotaSnapshot{Windows: []quotaWindow{{Presence: windowPresent, UsedPercent: &zero, ResetAt: &reset, LimitWindowSeconds: &full, ResetAfterSeconds: &full}}}
	if !freshQuota(q, now) {
		t.Fatal("fresh window should be eligible")
	}
	used := float64(1)
	q.Windows[0].UsedPercent = &used
	if freshQuota(q, now) {
		t.Fatal("used window must not be eligible")
	}
	q.Windows = append(q.Windows, quotaWindow{Presence: windowUnknown})
	if freshQuota(q, now) {
		t.Fatal("unknown reported window must block eligibility")
	}
}

func TestActiveFreshCycleCreatesOneStableSuccessor(t *testing.T) {
	storePath = t.TempDir() + "/state.db"
	closeStore()
	t.Cleanup(closeStore)
	full := int64(3600)
	now := time.Now().Unix() + 2
	activeReset := now + 1800
	used := float64(1)
	active := quotaSnapshot{ObservedAt: now, Windows: []quotaWindow{{Presence: windowPresent, UsedPercent: &used, ResetAt: &activeReset, LimitWindowSeconds: &full, ResetAfterSeconds: &full}}}
	zero := float64(0)
	freshReset := now + 100 + full
	fresh := quotaSnapshot{ObservedAt: now + 100, Windows: []quotaWindow{{Presence: windowPresent, UsedPercent: &zero, ResetAt: &freshReset, LimitWindowSeconds: &full, ResetAfterSeconds: &full}}}
	key := cycleKey(fresh)
	if ok, err := reserveCycle("successor-seat", key); err != nil || !ok {
		t.Fatalf("reserve predecessor: ok=%v err=%v", ok, err)
	}
	if err := observeCycle("successor-seat", key, active); err != nil {
		t.Fatal(err)
	}
	if record, err := readCycle("successor-seat", key); err != nil || record.ActiveObserved == 0 {
		t.Fatalf("active observation record=%+v err=%v", record, err)
	}
	a := account{Key: "successor-seat", Physical: true, Quota: fresh}
	successor, err := activationCycleKey(a)
	if err != nil || successor == key {
		t.Fatalf("successor=%q key=%q err=%v", successor, key, err)
	}
	repeated, err := activationCycleKey(a)
	if err != nil || repeated != successor {
		t.Fatalf("repeated successor=%q want=%q err=%v", repeated, successor, err)
	}
	if blocked, err := cycleBlocked("successor-seat", successor); err != nil || blocked {
		t.Fatalf("successor should be reservable: blocked=%v err=%v", blocked, err)
	}
}

func TestStoreRestartReleasesStaleJobsWithoutResend(t *testing.T) {
	storePath = t.TempDir() + "/state.db"
	closeStore()
	db, err := openStore()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	if _, err := db.Exec(`INSERT INTO jobs(id,kind,state,created_at,updated_at) VALUES('ignite-stale','ignite','running',?,?),('revive-stale','revive','awaiting_user',?,?)`, now, now, now, now); err != nil {
		t.Fatal(err)
	}
	closeStore()
	if _, err := openStore(); err != nil {
		t.Fatal(err)
	}
	ignite, err := readJob("ignite-stale")
	if err != nil || ignite.State != "completed" || ignite.Reason != "interrupted" {
		t.Fatalf("ignite stale=%+v err=%v", ignite, err)
	}
	revive, err := readJob("revive-stale")
	if err != nil || revive.State != "failed" || revive.Reason != "interrupted" {
		t.Fatalf("revive stale=%+v err=%v", revive, err)
	}
}

func TestScheduledBoundaryCreatesOneStableSuccessorAfterBoundary(t *testing.T) {
	storePath = t.TempDir() + "/state.db"
	closeStore()
	t.Cleanup(closeStore)
	now := time.Now().Unix()
	boundary := now + 100
	full := int64(3600)
	zero := float64(0)
	makeQuota := func(observed int64) quotaSnapshot {
		reset := observed + full
		return quotaSnapshot{ObservedAt: observed, Windows: []quotaWindow{{Presence: windowPresent, UsedPercent: &zero, ResetAt: &reset, LimitWindowSeconds: &full, ResetAfterSeconds: &full}}}
	}
	baseQuota := makeQuota(now)
	key := cycleKey(baseQuota)
	db, err := openStore()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO activation_cycles(account_key,cycle_key,run_id,status,reserved_at,updated_at,next_cycle_after) VALUES(?,?,?,'sent_unknown',?,?,?)`, "boundary-seat", key, "run", now, now, boundary); err != nil {
		t.Fatal(err)
	}
	a := account{Key: "boundary-seat", Physical: true, Quota: makeQuota(boundary)}
	before, err := activationCycleKey(a)
	if err != nil || before != key {
		t.Fatalf("at boundary key=%q want=%q err=%v", before, key, err)
	}
	a.Quota = makeQuota(boundary + 1)
	first, err := activationCycleKey(a)
	if err != nil || first == key {
		t.Fatalf("after boundary successor=%q base=%q err=%v", first, key, err)
	}
	repeated, err := activationCycleKey(a)
	if err != nil || repeated != first {
		t.Fatalf("repeated successor=%q want=%q err=%v", repeated, first, err)
	}
}

func TestIgniteDoesNotRetryAmbiguousSend(t *testing.T) {
	storePath = t.TempDir() + "/state.db"
	closeStore()
	calls := 0
	old := upstreamRequest
	defer func() { upstreamRequest = old }()
	upstreamRequest = func(context.Context, account, []byte) (int, error) { calls++; return 0, context.DeadlineExceeded }
	full := int64(3600)
	zero := float64(0)
	reset := int64(1700000000) + full
	q := quotaSnapshot{Windows: []quotaWindow{{Presence: windowPresent, UsedPercent: &zero, ResetAt: &reset, LimitWindowSeconds: &full, ResetAfterSeconds: &full}}}
	a := account{Key: "seat-a", AuthIndex: "a", Physical: true, AccessTokenValue: "fixture-token", Quota: q}
	r := ignite(context.Background(), []account{a}, time.Unix(1_700_000_000, 0))
	if r.Unknown != 1 || calls != 1 {
		t.Fatalf("result=%+v calls=%d", r, calls)
	}
	r = ignite(context.Background(), []account{a}, time.Unix(1_700_000_001, 0))
	if calls != 1 || r.Skipped != 1 {
		t.Fatalf("duplicate result=%+v calls=%d", r, calls)
	}
}

func TestIgniteDefiniteHTTPRejectionIsFailedBeforeSend(t *testing.T) {
	storePath = t.TempDir() + "/state.db"
	closeStore()
	old := upstreamRequest
	defer func() { upstreamRequest = old; closeStore() }()
	calls := 0
	upstreamRequest = func(context.Context, account, []byte) (int, error) { calls++; return 401, nil }
	full := int64(3600)
	zero := float64(0)
	reset := int64(1700000000) + full
	q := quotaSnapshot{Windows: []quotaWindow{{Presence: windowPresent, UsedPercent: &zero, ResetAt: &reset, LimitWindowSeconds: &full, ResetAfterSeconds: &full}}}
	a := account{Key: "seat-rejected", AuthIndex: "rejected", Physical: true, AccessTokenValue: "fixture-token", Quota: q}
	r := ignite(context.Background(), []account{a}, time.Unix(1_700_000_000, 0))
	if len(r.Outcomes) != 1 || r.Outcomes[0].Status != "failed_before_send" {
		t.Fatalf("result=%+v", r)
	}
	// A definite rejection is a known pre-send failure and is the sole
	// reusable reservation state for a later explicit workflow.
	r = ignite(context.Background(), []account{a}, time.Unix(1_700_000_001, 0))
	if len(r.Outcomes) != 1 || r.Outcomes[0].Status != "failed_before_send" || calls != 2 {
		t.Fatalf("retry result=%+v", r)
	}
}

func TestIgniteUnknownHTTPResponseIsAmbiguous(t *testing.T) {
	storePath = t.TempDir() + "/state.db"
	closeStore()
	old := upstreamRequest
	defer func() { upstreamRequest = old; closeStore() }()
	upstreamRequest = func(context.Context, account, []byte) (int, error) { return http.StatusRequestTimeout, nil }
	full := int64(3600)
	zero := float64(0)
	reset := int64(1700000000) + full
	q := quotaSnapshot{Windows: []quotaWindow{{Presence: windowPresent, UsedPercent: &zero, ResetAt: &reset, LimitWindowSeconds: &full, ResetAfterSeconds: &full}}}
	a := account{Key: "seat-ambiguous", AuthIndex: "ambiguous", Physical: true, AccessTokenValue: "fixture-token", Quota: q}
	r := ignite(context.Background(), []account{a}, time.Unix(1_700_000_000, 0))
	if len(r.Outcomes) != 1 || r.Outcomes[0].Status != "sent_unknown" {
		t.Fatalf("result=%+v", r)
	}
}

func TestIgniteSkipsUnavailableAccounts(t *testing.T) {
	storePath = t.TempDir() + "/state.db"
	closeStore()
	t.Cleanup(closeStore)
	calls := 0
	old := upstreamRequest
	upstreamRequest = func(context.Context, account, []byte) (int, error) {
		calls++
		return http.StatusOK, nil
	}
	t.Cleanup(func() { upstreamRequest = old })
	now := time.Unix(1_700_000_000, 0)
	zero, full := float64(0), int64(3600)
	reset := now.Unix() + full
	a := account{Key: "seat-unavailable", Physical: true, Unavailable: true, AccessTokenValue: "fixture-token", Quota: quotaSnapshot{Windows: []quotaWindow{{Presence: windowPresent, UsedPercent: &zero, ResetAt: &reset, LimitWindowSeconds: &full, ResetAfterSeconds: &full}}}}
	result := ignite(context.Background(), []account{a}, now)
	if calls != 0 || len(result.Outcomes) != 1 || result.Outcomes[0].Reason != "ineligible" {
		t.Fatalf("unavailable account must not dispatch: calls=%d result=%+v", calls, result)
	}
}

func TestIgniteSkipsNonPhysicalAccounts(t *testing.T) {
	storePath = t.TempDir() + "/state.db"
	closeStore()
	t.Cleanup(closeStore)
	calls := 0
	old := upstreamRequest
	upstreamRequest = func(context.Context, account, []byte) (int, error) {
		calls++
		return http.StatusOK, nil
	}
	t.Cleanup(func() { upstreamRequest = old })
	now := time.Unix(1_700_000_000, 0)
	zero, full := float64(0), int64(3600)
	reset := now.Unix() + full
	a := account{Key: "runtime-only", Physical: false, AccessTokenValue: "fixture-token", Quota: quotaSnapshot{Windows: []quotaWindow{{Presence: windowPresent, UsedPercent: &zero, ResetAt: &reset, LimitWindowSeconds: &full, ResetAfterSeconds: &full}}}}
	result := ignite(context.Background(), []account{a}, now)
	if calls != 0 || len(result.Outcomes) != 1 || result.Outcomes[0].Reason != "ineligible" {
		t.Fatalf("nonphysical account must not dispatch: calls=%d result=%+v", calls, result)
	}
}

func TestIgniteCancelledContextDoesNotReserveOrSend(t *testing.T) {
	storePath = t.TempDir() + "/state.db"
	closeStore()
	t.Cleanup(closeStore)
	calls := 0
	old := upstreamRequest
	upstreamRequest = func(context.Context, account, []byte) (int, error) {
		calls++
		return http.StatusOK, nil
	}
	t.Cleanup(func() { upstreamRequest = old })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	now := time.Unix(1_700_000_000, 0)
	zero, full := float64(0), int64(3600)
	reset := now.Unix() + full
	a := account{Key: "cancelled", Physical: true, AccessTokenValue: "fixture-token", Quota: quotaSnapshot{Windows: []quotaWindow{{Presence: windowPresent, UsedPercent: &zero, ResetAt: &reset, LimitWindowSeconds: &full, ResetAfterSeconds: &full}}}}
	result := ignite(ctx, []account{a}, now)
	if calls != 0 || len(result.Outcomes) != 1 || result.Outcomes[0].Reason != "cancelled" {
		t.Fatalf("cancelled job must not dispatch: calls=%d result=%+v", calls, result)
	}
	blocked, err := cycleBlocked(a.Key, cycleKey(a.Quota))
	if err != nil || blocked {
		t.Fatalf("cancelled job must not reserve a cycle: blocked=%v err=%v", blocked, err)
	}
}

func TestCycleBlockedExcludesDurableDispatchIntent(t *testing.T) {
	storePath = t.TempDir() + "/state.db"
	closeStore()
	t.Cleanup(closeStore)
	accountKey, key := "seat-scan", "fresh-cycle"
	reserved, err := reserveCycle(accountKey, key)
	if err != nil || !reserved {
		t.Fatalf("reserve cycle: reserved=%v err=%v", reserved, err)
	}
	blocked, err := cycleBlocked(accountKey, key)
	if err != nil || !blocked {
		t.Fatalf("dispatch intent should block scan: blocked=%v err=%v", blocked, err)
	}
	if err := updateCycle(accountKey, key, "sent_unknown"); err != nil {
		t.Fatal(err)
	}
	blocked, err = cycleBlocked(accountKey, key)
	if err != nil || !blocked {
		t.Fatalf("ambiguous terminal cycle should block scan: blocked=%v err=%v", blocked, err)
	}
	if err := updateCycle(accountKey, key, "failed_before_send"); err != nil {
		t.Fatal(err)
	}
	blocked, err = cycleBlocked(accountKey, key)
	if err != nil || blocked {
		t.Fatalf("definite pre-send failure should be reusable: blocked=%v err=%v", blocked, err)
	}
}

func TestScanFreshEligibleExcludesReservedCycle(t *testing.T) {
	storePath = t.TempDir() + "/state.db"
	closeStore()
	t.Cleanup(closeStore)
	now := time.Unix(1_700_000_000, 0)
	zero, full := float64(0), int64(3600)
	reset := now.Unix() + full
	a := account{Key: "seat-scan", Physical: true, AccessTokenValue: "fixture-token", Quota: quotaSnapshot{Windows: []quotaWindow{{Presence: windowPresent, UsedPercent: &zero, ResetAt: &reset, LimitWindowSeconds: &full, ResetAfterSeconds: &full}}}}
	if !scanFreshEligible(a, now) {
		t.Fatal("unreserved fresh account should be actionable")
	}
	if ok, err := reserveCycle(a.Key, cycleKey(a.Quota)); err != nil || !ok {
		t.Fatalf("reserve cycle: ok=%v err=%v", ok, err)
	}
	if scanFreshEligible(a, now) {
		t.Fatal("scan must exclude a cycle with durable dispatch intent")
	}
}

func TestActivationResultIsSanitizedForPersistedState(t *testing.T) {
	result := activationResult{Total: 1, Eligible: 1, Sent: 0, Skipped: 0, Unknown: 1, Outcomes: []activationOutcome{{Ordinal: 1, Status: "sent_unknown", Reason: "ambiguous_response"}}}
	encoded, err := jsonMarshalSanitized(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"account_key", "auth_index", "email", "auth_path", "fixture-token", "access_token"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("persisted result leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestScanFreshEligibleBlocksAllNonReusableCycleStatuses(t *testing.T) {
	storePath = t.TempDir() + "/state.db"
	closeStore()
	t.Cleanup(closeStore)
	now := time.Unix(1_700_000_000, 0)
	zero, full := float64(0), int64(3600)
	reset := now.Unix() + full
	statuses := []string{"dispatch_intent", "sent_unknown", "partial", "verified", "failed_before_send"}
	for _, status := range statuses {
		a := account{Key: "seat-" + status, Physical: true, AccessTokenValue: "fixture-token", Quota: quotaSnapshot{Windows: []quotaWindow{{Presence: windowPresent, UsedPercent: &zero, ResetAt: &reset, LimitWindowSeconds: &full, ResetAfterSeconds: &full}}}}
		if ok, err := reserveCycle(a.Key, cycleKey(a.Quota)); err != nil || !ok {
			t.Fatalf("reserve %s: ok=%v err=%v", status, ok, err)
		}
		if status != "dispatch_intent" {
			if err := updateCycle(a.Key, cycleKey(a.Quota), status); err != nil {
				t.Fatalf("update %s: %v", status, err)
			}
		}
		if status != "failed_before_send" && scanFreshEligible(a, now) {
			t.Fatalf("status %s must be excluded from fresh actionable scan", status)
		}
		if status == "failed_before_send" && !scanFreshEligible(a, now) {
			t.Fatal("failed_before_send must remain reusable")
		}
	}
}

func TestCountFreshActionableSharesIgniteCycleGate(t *testing.T) {
	storePath = t.TempDir() + "/state.db"
	closeStore()
	t.Cleanup(closeStore)
	old := upstreamRequest
	t.Cleanup(func() { upstreamRequest = old })
	upstreamRequest = func(context.Context, account, []byte) (int, error) {
		return 0, context.DeadlineExceeded
	}
	now := time.Unix(1_700_000_000, 0)
	zero, full := float64(0), int64(3600)
	reset := now.Unix() + full
	a := account{Key: "shared-gate", AuthIndex: "shared-gate", Physical: true, AccessTokenValue: "fixture-token", Quota: quotaSnapshot{Windows: []quotaWindow{{Presence: windowPresent, UsedPercent: &zero, ResetAt: &reset, LimitWindowSeconds: &full, ResetAfterSeconds: &full}}}}
	if got := countFreshActionable([]account{a}, now); got != 1 {
		t.Fatalf("initial actionable count=%d want 1", got)
	}
	result := ignite(context.Background(), []account{a}, now)
	if len(result.Outcomes) != 1 || result.Outcomes[0].Status != "sent_unknown" {
		t.Fatalf("ignite result=%+v", result)
	}
	if got := countFreshActionable([]account{a}, now); got != 0 {
		t.Fatalf("ambiguous cycle must be excluded by the same gate; count=%d", got)
	}
}

func TestReserveCycleAtomicUpsertWaitsForContendedWriter(t *testing.T) {
	storePath = t.TempDir() + "/state.db"
	closeStore()
	t.Cleanup(closeStore)
	db, err := openStore()
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	if _, err := tx.Exec(`INSERT INTO activation_cycles(account_key,cycle_key,status,reserved_at,updated_at) VALUES(?,?,?,?,?)`, "lock-holder", "lock-cycle", "dispatch_intent", now, now); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}

	const workers = 4
	started := make(chan struct{}, workers)
	results := make(chan struct {
		reserved bool
		err      error
	}, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			started <- struct{}{}
			reserved, err := reserveCycle("contended", "cycle")
			results <- struct {
				reserved bool
				err      error
			}{reserved: reserved, err: err}
		}()
	}
	for i := 0; i < workers; i++ {
		<-started
	}
	// Give every worker a chance to reach SQLite while the real write lock is
	// held. The transaction is released below; no worker may be retried by the
	// test itself.
	time.Sleep(100 * time.Millisecond)
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	close(results)
	wins := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("contended reserve failed: %v", result.err)
		}
		if result.reserved {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("atomic upsert winners=%d want exactly one", wins)
	}
	blocked, err := cycleBlocked("contended", "cycle")
	if err != nil || !blocked {
		t.Fatalf("winning dispatch intent must block later scan: blocked=%v err=%v", blocked, err)
	}
}

func TestOpenStoreClearsLegacyRepairPaths(t *testing.T) {
	storePath = t.TempDir() + "/state.db"
	closeStore()
	t.Cleanup(closeStore)
	legacyDir := t.TempDir()
	legacyPath := filepath.Join(legacyDir, "legacy.json")
	db, err := openStore()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO repair_rows(job_id,ordinal,account_key,auth_path,auth_dir,state) VALUES('legacy',1,'opaque',?,?, 'queued')`, legacyPath, legacyDir); err != nil {
		t.Fatal(err)
	}
	closeStore()
	db, err = openStore()
	if err != nil {
		t.Fatal(err)
	}
	var authPath, authDir string
	if err := db.QueryRow(`SELECT auth_path,auth_dir FROM repair_rows WHERE job_id='legacy'`).Scan(&authPath, &authDir); err != nil {
		t.Fatal(err)
	}
	if authPath != "" || authDir != "" {
		t.Fatalf("legacy physical paths survived migration: auth_path=%q auth_dir=%q", authPath, authDir)
	}
}

func TestReplacementMatchingNeverFallsBackFromExactAccountIDToEmail(t *testing.T) {
	expected := account{Key: "old-seat", AccountID: "account-a", Email: "seat@example.test"}
	before := []account{{Key: expected.Key, AccountID: expected.AccountID, Email: expected.Email, AuthPath: "old", UpdatedAt: "old"}}
	wrongID := []account{{Key: "new-seat", AccountID: "account-b", Email: expected.Email, AuthPath: "new", UpdatedAt: "new", Physical: true}}
	if replacement, ok := replacementMatches(before, wrongID, expected); ok || replacement.Key != "" {
		t.Fatalf("same-email wrong-account replacement accepted: %+v ok=%v", replacement, ok)
	}
	missingID := []account{{Key: "new-seat", Email: expected.Email, AuthPath: "new", UpdatedAt: "new", Physical: true}}
	if replacement, ok := replacementMatches(before, missingID, expected); ok || replacement.Key != "" {
		t.Fatalf("same-email replacement without exact account ID accepted: %+v ok=%v", replacement, ok)
	}
	correctID := []account{{Key: "new-seat", AccountID: expected.AccountID, Email: expected.Email, AuthPath: "new", UpdatedAt: "new", Physical: true}}
	replacement, ok := replacementMatches(before, correctID, expected)
	if !ok || replacement.Key != "new-seat" {
		t.Fatalf("exact account-ID replacement not accepted: %+v ok=%v", replacement, ok)
	}
	noExactID := account{Key: "old-seat", Email: expected.Email}
	if replacement, ok := replacementMatches([]account{{Key: noExactID.Key, Email: noExactID.Email}}, []account{{Key: "new-seat", Email: noExactID.Email, AuthPath: "new", UpdatedAt: "new", Physical: true}}, noExactID); ok || replacement.Key != "" {
		t.Fatalf("same-email replacement without predecessor identity accepted: %+v ok=%v", replacement, ok)
	}
}

func TestReplacementMatchingAcceptsChangedSameKeyAndRejectsUnchanged(t *testing.T) {
	expected := account{Key: "hashed-account-seat", AccountID: "account-a", Email: "seat@example.test"}
	before := []account{{Key: expected.Key, AccountID: expected.AccountID, Email: expected.Email, AuthPath: "/auth/codex-a.json", UpdatedAt: "before"}}
	changed := []account{{Key: expected.Key, AccountID: expected.AccountID, Email: expected.Email, AuthPath: "/auth/codex-a.json", UpdatedAt: "after", Physical: true}}
	if replacement, ok := replacementMatches(before, changed, expected); !ok || replacement.Key != expected.Key {
		t.Fatalf("changed same-key replacement must be accepted: %+v ok=%v", replacement, ok)
	}
	unchanged := []account{{Key: expected.Key, AccountID: expected.AccountID, Email: expected.Email, AuthPath: "/auth/codex-a.json", UpdatedAt: "before", Physical: true}}
	if replacement, ok := replacementMatches(before, unchanged, expected); ok || replacement.Key != "" {
		t.Fatalf("unchanged pre-existing same-key record must be rejected: %+v ok=%v", replacement, ok)
	}
	preExistingOtherKey := []account{{Key: "other-preexisting-seat", AccountID: expected.AccountID, Email: expected.Email, AuthPath: "/auth/other.json", UpdatedAt: "after", Physical: true}}
	beforeWithOtherKey := append(append([]account(nil), before...), preExistingOtherKey[0])
	if replacement, ok := replacementMatches(beforeWithOtherKey, preExistingOtherKey, expected); ok || replacement.Key != "" {
		t.Fatalf("pre-existing different-key record must be rejected: %+v ok=%v", replacement, ok)
	}
	if replacement, ok := replacementMatches(nil, changed, expected); ok || replacement.Key != "" {
		t.Fatalf("same-key record without predecessor marker must be rejected: %+v ok=%v", replacement, ok)
	}
}

func TestRevivePollOnlyReturnsTransientOAuthURLForActiveRuntime(t *testing.T) {
	storePath = t.TempDir() + "/state.db"
	closeStore()
	t.Cleanup(func() {
		reviveRuntimeState.Lock()
		delete(reviveRuntimeState.jobs, "oauth-job")
		reviveRuntimeState.Unlock()
		closeStore()
	})
	db, err := openStore()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO jobs(id,kind,state,created_at,updated_at,total) VALUES('oauth-job','revive','awaiting_user',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	reviveRuntimeState.Lock()
	reviveRuntimeState.jobs["oauth-job"] = &reviveRuntime{oauthURL: "https://login.example.test/authorize"}
	reviveRuntimeState.Unlock()
	active := revivePoll("oauth-job")
	var activeBody map[string]any
	if err := json.Unmarshal(active.Body, &activeBody); err != nil {
		t.Fatal(err)
	}
	if activeBody["oauth_url"] != "https://login.example.test/authorize" {
		t.Fatalf("active poll must expose only the current transient URL: %v", activeBody)
	}
	if _, err := db.Exec(`UPDATE jobs SET state='completed' WHERE id='oauth-job'`); err != nil {
		t.Fatal(err)
	}
	terminal := revivePoll("oauth-job")
	var terminalBody map[string]any
	if err := json.Unmarshal(terminal.Body, &terminalBody); err != nil {
		t.Fatal(err)
	}
	if _, ok := terminalBody["oauth_url"]; ok {
		t.Fatalf("terminal poll must not return OAuth URL: %v", terminalBody)
	}
	state := routeManagement(managementRequest{Method: "GET", Path: "/v0/management/plugins/cpa-phoenix/state", Query: map[string][]string{"id": {"oauth-job"}}})
	if strings.Contains(string(state.Body), "oauth_url") {
		t.Fatalf("general state endpoint must not return OAuth URL: %s", state.Body)
	}
}

func TestAnyJobActiveProjectsDurableGate(t *testing.T) {
	storePath = t.TempDir() + "/state.db"
	closeStore()
	t.Cleanup(closeStore)
	db, err := openStore()
	if err != nil {
		t.Fatal(err)
	}
	if anyJobActive() {
		t.Fatal("empty job store must not report an active job")
	}
	if _, err := db.Exec(`INSERT INTO jobs(id,kind,state,created_at,updated_at,total) VALUES('active-job','ignite','running',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if !anyJobActive() {
		t.Fatal("durable running job must keep actions disabled")
	}
	if _, err := db.Exec(`UPDATE jobs SET state='completed' WHERE id='active-job'`); err != nil {
		t.Fatal(err)
	}
	if anyJobActive() {
		t.Fatal("completed durable job must release the action gate")
	}
}
