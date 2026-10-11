package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func quotaFloorFixture(now time.Time, percent float64) quotaSnapshot {
	window := quotaFloorPrimaryWindow
	reset := now.Unix() + window
	return quotaSnapshot{ObservedAt: now.Unix(), Windows: []quotaWindow{
		{Presence: windowPresent, UsedPercent: &percent, ResetAt: &reset, LimitWindowSeconds: &window, ResetAfterSeconds: &window},
		{Presence: windowPresent, UsedPercent: &percent, ResetAt: &reset, LimitWindowSeconds: &window, ResetAfterSeconds: &window},
	}}
}

func TestQuotaFloorDecisionBoundary(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	for _, tc := range []struct {
		name    string
		percent float64
		want    string
	}{
		{"below floor", 94, "allow"},
		{"at floor", 95, "block"},
		{"above floor", 100, "block"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, _, _ := quotaFloorDecision(quotaFloorFixture(now, tc.percent), now, 95)
			if status != tc.want {
				t.Fatalf("status=%q want=%q", status, tc.want)
			}
		})
	}
}

func TestQuotaFloorDecisionRequiresFreshPrimaryWindow(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	q := quotaFloorFixture(now, 94)
	q.Windows[1].Presence = windowUnknown
	if status, _, _ := quotaFloorDecision(q, now, 95); status != "allow" {
		t.Fatalf("status=%q want allow when only secondary is missing", status)
	}
	q = quotaFloorFixture(now, 94)
	q.Windows[0].ResetAt = func() *int64 { v := now.Unix() - 1; return &v }()
	if status, _, _ := quotaFloorDecision(q, now, 95); status != "unknown" {
		t.Fatalf("status=%q want unknown when reset is stale", status)
	}
	q = quotaFloorFixture(now, 94)
	farReset := now.Unix() + 10*24*60*60
	q.Windows[0].ResetAt = &farReset
	if status, _, _ := quotaFloorDecision(q, now, 95); status != "unknown" {
		t.Fatalf("status=%q want unknown for an inconsistent equal-duration reset", status)
	}
	for _, threshold := range []float64{-1, 0, 100, 101} {
		if status, _, _ := quotaFloorDecision(quotaFloorFixture(now, 94), now, threshold); status != "unknown" {
			t.Fatalf("threshold=%v status=%q want unknown", threshold, status)
		}
	}
}

func TestQuotaFloorTargetKeepsExactPersistedIdentity(t *testing.T) {
	accounts := []account{
		{Physical: true, Email: "lifan.builds@gmail.com", AuthIndex: "second", AuthFile: "second.json"},
		{Physical: true, Email: "lifan.builds@gmail.com", AuthIndex: "first", AuthFile: "first.json"},
	}
	target, ok := quotaFloorTarget(accounts, quotaFloorConfig{TargetEmail: "lifan.builds@gmail.com", TargetAuthIndex: "first", TargetName: "first.json"})
	if !ok || target.AuthIndex != "first" || target.AuthFile != "first.json" {
		t.Fatalf("target=%+v ok=%v", target, ok)
	}
	if _, ok := quotaFloorTarget(accounts, quotaFloorConfig{TargetEmail: "lifan.builds@gmail.com", TargetAuthIndex: "missing", TargetName: "missing.json"}); ok {
		t.Fatal("missing persisted identity must fail closed")
	}
	if _, ok := quotaFloorTarget(accounts, quotaFloorConfig{TargetEmail: "lifan.builds@gmail.com"}); ok {
		t.Fatal("duplicate email-only identity must fail closed")
	}
	if _, ok := quotaFloorTarget([]account{{Physical: true, Email: "unique@example.test", AuthIndex: "only", AuthFile: "only.json"}}, quotaFloorConfig{TargetEmail: "unique@example.test"}); ok {
		t.Fatal("unique email-only identity must also fail closed")
	}
	rebound, ok := quotaFloorTarget(accounts, quotaFloorConfig{TargetEmail: "lifan.builds@gmail.com", TargetAuthIndex: "stale", TargetName: "first.json"})
	if !ok || rebound.AuthIndex != "first" {
		t.Fatalf("stale index should rebind the same unique file: target=%+v ok=%v", rebound, ok)
	}
}

func TestQuotaFloorWindowRejectsInconsistentReset(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	q := quotaFloorFixture(now, 94)
	reset := now.Unix() + 15*24*60*60
	q.Windows[0].ResetAt = &reset
	if status, _, _ := quotaFloorDecision(q, now, 95); status != "unknown" {
		t.Fatalf("status=%q want unknown for inconsistent reset metadata", status)
	}
}

func TestQuotaFloorDecisionUsesFiveHourPrimaryOnly(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	q := quotaFloorFixture(now, 20)
	secondaryUsed := 99.0
	q.Windows[1].UsedPercent = &secondaryUsed
	if status, _, _ := quotaFloorDecision(q, now, 95); status != "allow" {
		t.Fatalf("status=%q want allow when only weekly window is over threshold", status)
	}
	primaryUsed := 96.0
	q.Windows[0].UsedPercent = &primaryUsed
	if status, _, _ := quotaFloorDecision(q, now, 95); status != "block" {
		t.Fatalf("status=%q want block when five-hour window is over threshold", status)
	}
}

func TestQuotaFloorDecisionRequiresFiveHourPrimaryWindow(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	q := quotaFloorFixture(now, 20)
	oneHour := int64(time.Hour / time.Second)
	q.Windows[0].LimitWindowSeconds = &oneHour
	if status, _, _ := quotaFloorDecision(q, now, 95); status != "unknown" {
		t.Fatalf("status=%q want unknown for a non-five-hour primary window", status)
	}
}

func TestQuotaFloorResetMustAdvanceBeforeReenable(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	q := quotaFloorFixture(now, 20)
	windows := quotaWindows(q)
	cfg := quotaFloorConfig{BlockedPrimaryReset: *windows[0].ResetAt, BlockedSecondaryReset: *windows[1].ResetAt}
	if quotaFloorResetObserved(cfg, &windows[0], &windows[1]) {
		t.Fatal("same quota cycle must not permit re-enable")
	}
	advanced := *windows[0].ResetAt + 1
	windows[0].ResetAt = &advanced
	if !quotaFloorResetObserved(cfg, &windows[0], &windows[1]) {
		t.Fatal("a fresh blocking-window reset should permit re-enable")
	}
}

func TestQuotaFloorResetIgnoresWeeklyWindow(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	q := quotaFloorFixture(now, 20)
	windows := quotaWindows(q)
	cfg := quotaFloorConfig{BlockedPrimaryReset: *windows[0].ResetAt, BlockedSecondaryReset: *windows[1].ResetAt}
	advanced := *windows[1].ResetAt + 1
	windows[1].ResetAt = &advanced
	if quotaFloorResetObserved(cfg, &windows[0], &windows[1]) {
		t.Fatal("weekly reset must not re-enable before the five-hour reset")
	}
}

func quotaFloorTestStore(t *testing.T) {
	t.Helper()
	oldPath := storePath
	storePath = filepath.Join(t.TempDir(), "state.db")
	closeStore()
	t.Cleanup(func() {
		closeStore()
		storePath = oldPath
	})
}

func TestQuotaFloorManagementRejectsInvalidValues(t *testing.T) {
	quotaFloorTestStore(t)
	for _, body := range []string{
		`{"acknowledge":"CPA_PHOENIX_ONE_CLICK","target_email":"invalid"}`,
		`{"acknowledge":"CPA_PHOENIX_ONE_CLICK","threshold_percent":100}`,
		`{"acknowledge":"CPA_PHOENIX_ONE_CLICK","poll_seconds":4}`,
	} {
		response := routeManagement(managementRequest{Method: http.MethodPost, Path: "/v0/management/plugins/cpa-phoenix/quota-floor", Body: []byte(body)})
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d want 400", body, response.StatusCode)
		}
	}
}

func TestQuotaFloorFreshStoreIsOptIn(t *testing.T) {
	quotaFloorTestStore(t)
	cfg, err := readQuotaFloor()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Enabled || cfg.TargetEmail != "" || cfg.TargetName != "" || cfg.TargetAuthIndex != "" || cfg.TargetAccountID != "" {
		t.Fatalf("fresh quota-floor config=%+v want disabled and unbound", cfg)
	}
	if cfg.ThresholdPercent != quotaFloorDefaultThreshold || cfg.PollSeconds != quotaFloorDefaultPoll || cfg.LastStatus != "unknown" {
		t.Fatalf("fresh quota-floor defaults=%+v", cfg)
	}
}

func TestQuotaFloorManagementRejectsUnresolvedDirectIdentity(t *testing.T) {
	quotaFloorTestStore(t)
	oldList := quotaFloorListAccounts
	quotaFloorListAccounts = func() ([]account, error) {
		return []account{{Physical: true, Email: "seat@example.test", AuthFile: "actual.json", AuthIndex: "actual-index"}}, nil
	}
	t.Cleanup(func() { quotaFloorListAccounts = oldList })
	response := routeManagement(managementRequest{Method: http.MethodPost, Path: "/v0/management/plugins/cpa-phoenix/quota-floor", Body: []byte(`{"acknowledge":"CPA_PHOENIX_ONE_CLICK","enabled":true,"target_name":"missing.json","target_auth_index":"missing"}`)})
	if response.StatusCode != http.StatusBadRequest || !strings.Contains(string(response.Body), "invalid_target_identity") {
		t.Fatalf("status=%d body=%s want unresolved identity rejection", response.StatusCode, response.Body)
	}
}

func TestQuotaFloorDecisionRejectsStaleObservation(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	q := quotaFloorFixture(now.Add(-quotaFloorObservationMaxAge-time.Second), 1)
	if status, _, _ := quotaFloorDecision(q, now, 95); status != "unknown" {
		t.Fatalf("status=%q want unknown for stale observation", status)
	}
}

func TestQuotaFloorUnknownObservationFailsClosedWithoutRepeatedWrites(t *testing.T) {
	quotaFloorTestStore(t)
	db, err := openStore()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE quota_floor SET enabled=1,target_email='unknown@example.test',target_name='unknown.json',target_auth_index='unknown-index' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	state := account{Physical: true, Email: "unknown@example.test", AuthIndex: "unknown-index", AuthFile: "unknown.json"}
	oldList, oldRefresh, oldSave := quotaFloorListAccounts, quotaFloorRefresh, quotaFloorSave
	t.Cleanup(func() { quotaFloorListAccounts, quotaFloorRefresh, quotaFloorSave = oldList, oldRefresh, oldSave })
	quotaFloorListAccounts = func() ([]account, error) { return []account{state}, nil }
	quotaFloorRefresh = func(a account) account {
		a.Quota = quotaSnapshot{ObservedAt: now.Unix(), Windows: []quotaWindow{{Presence: windowUnknown}}}
		return a
	}
	saves := 0
	quotaFloorSave = func(a account, disabled bool) error {
		saves++
		state.Disabled = disabled
		return nil
	}
	if err := quotaFloorTickAt(now); err != nil {
		t.Fatal(err)
	}
	cfg, err := readQuotaFloor()
	if err != nil {
		t.Fatal(err)
	}
	if saves != 1 || !state.Disabled || !cfg.ControllerOwned || cfg.LastStatus != "unknown" || cfg.LastError == "" {
		t.Fatalf("saves=%d state=%+v cfg=%+v want fail-closed unknown hold", saves, state, cfg)
	}
	if err := quotaFloorTickAt(now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if saves != 1 {
		t.Fatalf("saves=%d want no repeated disable while held", saves)
	}
}

func TestQuotaFloorManagementSelectsExactSeatAndHidesPrivateIdentity(t *testing.T) {
	quotaFloorTestStore(t)
	accounts := []account{
		{Key: "seat-one", AuthIndex: "index-one", AuthFile: "one.json", Email: "same@example.test", Physical: true},
		{Key: "seat-two", AuthIndex: "index-two", AuthFile: "two.json", Email: "same@example.test", Physical: true, Disabled: true},
	}
	oldList := quotaFloorListAccounts
	quotaFloorListAccounts = func() ([]account, error) { return accounts, nil }
	t.Cleanup(func() { quotaFloorListAccounts = oldList })
	seat := stableSeatLabel(accounts[1])
	response := routeManagement(managementRequest{Method: http.MethodPost, Path: "/v0/management/plugins/cpa-phoenix/quota-floor", Body: []byte(`{"acknowledge":"CPA_PHOENIX_ONE_CLICK","enabled":true,"target_seat":"` + seat + `"}`)})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, response.Body)
	}
	if strings.Contains(string(response.Body), "target_auth_index") || strings.Contains(string(response.Body), "target_name") {
		t.Fatalf("quota-floor response leaked private identity: %s", response.Body)
	}
	var public quotaFloorConfig
	if err := json.Unmarshal(response.Body, &public); err != nil {
		t.Fatal(err)
	}
	if public.TargetSeat != seat || public.TargetStatus != "disabled" || !public.Enabled || len(public.Accounts) != 2 {
		t.Fatalf("public=%+v want selected seat and two choices", public)
	}
	if public.Accounts[1].Status != "disabled" {
		t.Fatalf("accounts=%+v want disabled status", public.Accounts)
	}
	cfg, err := readQuotaFloor()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TargetName != "two.json" || cfg.TargetAuthIndex != "index-two" || cfg.TargetEmail != "same@example.test" {
		t.Fatalf("cfg=%+v want exact selected identity", cfg)
	}

	bad := routeManagement(managementRequest{Method: http.MethodPost, Path: "/v0/management/plugins/cpa-phoenix/quota-floor", Body: []byte(`{"acknowledge":"CPA_PHOENIX_ONE_CLICK","enabled":true,"target_seat":"seat-missing"}`)})
	if bad.StatusCode != http.StatusBadRequest || !strings.Contains(string(bad.Body), "invalid_target_seat") {
		t.Fatalf("bad selection status=%d body=%s", bad.StatusCode, bad.Body)
	}

	off := routeManagement(managementRequest{Method: http.MethodPost, Path: "/v0/management/plugins/cpa-phoenix/quota-floor", Body: []byte(`{"acknowledge":"CPA_PHOENIX_ONE_CLICK","enabled":false,"target_seat":"` + seat + `"}`)})
	if off.StatusCode != http.StatusOK {
		t.Fatalf("disable status=%d body=%s", off.StatusCode, off.Body)
	}
	cfg, err = readQuotaFloor()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Enabled || cfg.TargetName != "two.json" || cfg.TargetAuthIndex != "index-two" {
		t.Fatalf("cfg after optional off=%+v want disabled with target retained", cfg)
	}
}

func TestQuotaFloorManagementDoesNotAbandonOwnedDisabledTarget(t *testing.T) {
	quotaFloorTestStore(t)
	db, err := openStore()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE quota_floor SET enabled=1,target_email='seat@example.test',target_name='seat.json',target_auth_index='seat-index',controller_owned=1,last_status='disabled' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"acknowledge":"CPA_PHOENIX_ONE_CLICK","enabled":false}`,
		`{"acknowledge":"CPA_PHOENIX_ONE_CLICK","target_seat":"seat-other"}`,
	} {
		response := routeManagement(managementRequest{Method: http.MethodPost, Path: "/v0/management/plugins/cpa-phoenix/quota-floor", Body: []byte(body)})
		if response.StatusCode != http.StatusConflict || !strings.Contains(string(response.Body), "controller_owned_target_requires_reset") {
			t.Fatalf("body=%s status=%d response=%s", body, response.StatusCode, response.Body)
		}
	}
}

func TestQuotaFloorMigratesUntouchedFiveSecondDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE quota_floor (
id INTEGER PRIMARY KEY CHECK(id=1), enabled INTEGER NOT NULL DEFAULT 1,
target_email TEXT NOT NULL DEFAULT 'lifan.builds@gmail.com', threshold_percent REAL NOT NULL DEFAULT 95,
poll_seconds INTEGER NOT NULL DEFAULT 5, target_name TEXT NOT NULL DEFAULT '', target_auth_index TEXT NOT NULL DEFAULT '',
controller_owned INTEGER NOT NULL DEFAULT 0, last_status TEXT NOT NULL DEFAULT 'unknown', last_observed_at INTEGER NOT NULL DEFAULT 0,
last_primary_percent REAL, last_secondary_percent REAL, last_primary_reset_at INTEGER NOT NULL DEFAULT 0,
last_secondary_reset_at INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '')`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO quota_floor(id) VALUES(1)`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	oldPath := storePath
	storePath = path
	closeStore()
	t.Cleanup(func() { closeStore(); storePath = oldPath })
	cfg, err := readQuotaFloor()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PollSeconds != quotaFloorDefaultPoll {
		t.Fatalf("poll_seconds=%d want %d", cfg.PollSeconds, quotaFloorDefaultPoll)
	}
}

func TestQuotaFloorTickWaitsForResetAndPropagatesWriteError(t *testing.T) {
	quotaFloorTestStore(t)
	db, err := openStore()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE quota_floor SET enabled=1,target_email='unique@example.test',target_name='unique.json',target_auth_index='index' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	accountState := account{Physical: true, Email: "unique@example.test", AuthIndex: "index", AuthFile: "unique.json"}
	accountState.Quota = quotaFloorFixture(now, 96)
	oldList, oldRefresh, oldSave, oldWrite := quotaFloorListAccounts, quotaFloorRefresh, quotaFloorSave, quotaFloorWrite
	t.Cleanup(func() {
		quotaFloorListAccounts, quotaFloorRefresh, quotaFloorSave, quotaFloorWrite = oldList, oldRefresh, oldSave, oldWrite
	})
	quotaFloorListAccounts = func() ([]account, error) { return []account{accountState}, nil }
	quotaFloorRefresh = func(a account) account { a.Quota = accountState.Quota; return a }
	saves := 0
	quotaFloorSave = func(a account, disabled bool) error { saves++; accountState.Disabled = disabled; return nil }
	if err := quotaFloorTickAt(now); err != nil {
		t.Fatal(err)
	}
	if saves != 1 {
		t.Fatalf("saves=%d want initial disable", saves)
	}
	accountState.Quota = quotaFloorFixture(now, 20)
	if err := quotaFloorTickAt(now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if saves != 1 {
		t.Fatalf("saves=%d want no re-enable before reset", saves)
	}
	accountState.Quota = quotaFloorFixture(now.Add(time.Second), 20)
	if err := quotaFloorTickAt(now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if saves != 2 || accountState.Disabled {
		t.Fatalf("saves=%d disabled=%v want re-enable after reset", saves, accountState.Disabled)
	}
	quotaFloorWrite = func(quotaFloorConfig) error { return errors.New("write failed") }
	if err := quotaFloorTickAt(now.Add(2 * time.Second)); err == nil {
		t.Fatal("quota-floor tick must propagate persistence errors")
	}
}

func TestQuotaFloorConfigJSONIncludesResetOwnership(t *testing.T) {
	raw, err := json.Marshal(quotaFloorConfig{BlockedPrimaryReset: 1, BlockedSecondaryReset: 2})
	if err != nil || string(raw) == "{}" {
		t.Fatalf("reset ownership fields not serializable: %s err=%v", raw, err)
	}
}

func TestSaveAuthDisabledVerifiesPhysicalWrite(t *testing.T) {
	oldCall := quotaFloorHostCall
	t.Cleanup(func() { quotaFloorHostCall = oldCall })
	current := `{"type":"codex","email":"same@example.test","disabled":false}`
	quotaFloorHostCall = func(method string, _ any) (json.RawMessage, error) {
		switch method {
		case "host.auth.get":
			return json.RawMessage(`{"name":"target.json","json":` + current + `}`), nil
		case "host.auth.save":
			return json.RawMessage(`{"name":"target.json"}`), nil
		default:
			return nil, errors.New("unexpected host method")
		}
	}
	if err := saveAuthDisabled(account{AuthIndex: "target-index", AuthFile: "target.json"}, true); err == nil || err.Error() != "auth_disabled_not_persisted" {
		t.Fatalf("saveAuthDisabled error=%v want auth_disabled_not_persisted", err)
	}
}

func TestSaveAuthDisabledVerifiesRuntimeAndPersistence(t *testing.T) {
	for _, tc := range []struct {
		name            string
		disabled        bool
		runtimeDisabled bool
		runtimeStatus   string
		wantError       string
	}{
		{"disable applies", true, true, "disabled", ""},
		{"enable applies", false, false, "active", ""},
		{"disk only disable", true, false, "active", "auth_disabled_not_applied"},
		{"disk only enable", false, true, "disabled", "auth_disabled_not_applied"},
		{"stale routing status", true, true, "active", "auth_disabled_not_applied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldCall := quotaFloorHostCall
			t.Cleanup(func() { quotaFloorHostCall = oldCall })
			current := json.RawMessage(`{"type":"codex","email":"same@example.test","disabled":false,"access_token":"fixture-token"}`)
			quotaFloorHostCall = func(method string, payload any) (json.RawMessage, error) {
				switch method {
				case "host.auth.get":
					return json.RawMessage(`{"name":"target.json","json":` + string(current) + `}`), nil
				case "host.auth.save":
					save := payload.(map[string]any)
					if save["name"] != "target.json" {
						t.Fatal("save redirected to a different physical target")
					}
					current = save["json"].(json.RawMessage)
					var auth struct {
						Disabled bool   `json:"disabled"`
						Token    string `json:"access_token"`
					}
					if json.Unmarshal(current, &auth) != nil || auth.Disabled != tc.disabled || auth.Token != "fixture-token" {
						t.Fatal("saved auth did not preserve credentials and requested state")
					}
					return json.RawMessage(`{"name":"target.json"}`), nil
				case "host.auth.get_runtime":
					return json.Marshal(authRuntimeResponse{Auth: authFileEntry{AuthIndex: "target-index", Name: "target.json", Disabled: tc.runtimeDisabled, Status: tc.runtimeStatus}})
				default:
					return nil, errors.New("unexpected host method")
				}
			}
			if err := saveAuthDisabled(account{AuthIndex: "target-index", AuthFile: "target.json"}, tc.disabled); (err == nil) != (tc.wantError == "") || err != nil && err.Error() != tc.wantError {
				t.Fatalf("error=%v want=%q", err, tc.wantError)
			}
		})
	}
}
