package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// These are the bounded markers used by the login-butler. Generic service
// failures do not enter the repair queue.
var invalidMarkers = []string{
	"auth_unavailable", "authentication_error", "unauthorized", "invalidated",
	"revoked", "reused_token", "reused token", "invalid_token", "invalid token",
	"missing access token",
}

var errNoActionableAccounts = errors.New("no_actionable_accounts")

var errRepairIdentityUnavailable = errors.New("repair_identity_unavailable")

var errInvalidBrowserMode = errors.New("invalid_browser_mode")

var errNoRecoverableSkippedAccounts = errors.New("no_recoverable_skipped_accounts")

var errRecoverySeatMismatch = errors.New("recovery_seat_mismatch")

var errNoResettableRepairQueue = errors.New("no_resettable_repair_queue")

const (
	reviveBrowserModeAutomatic = "automatic"
	reviveBrowserModeAgent     = "agent"
	maxWorkspaceTrials         = 16
)

// These narrow seams keep the recovery state machine deterministic in offline
// tests. Production uses the real host inventory, quarantine, callback, and
// native OAuth implementations; no alternate runtime behavior is registered.
var (
	repairListAccounts          = listAccounts
	repairSafeQuarantine        = safeQuarantine
	repairBindCallbackForwarder = bindCallbackForwarder
	repairStartNativeOAuth      = startNativeOAuth
	repairStartNativeOAuthMode  = startNativeOAuthMode
	repairPollNativeOAuth       = pollNativeOAuth
	repairRefreshQuota          = refreshQuota
	// Agent Autofill is the only read-only source for repair codes. The browser
	// and queue still inject the detector so tests can prove freshness and
	// recipient scoping without touching a real mailbox.
	repairDetectCode           = detectRepairCode
	repairCaptureIdentity      = captureRepairIdentity
	repairReplacementWaitLimit = 5 * time.Second
	repairReplacementPollDelay = 100 * time.Millisecond
	repairNewLoginBrowser      = func(ctx context.Context) (loginBrowserController, error) { return newLoginBrowser(ctx) }
)

// loginBrowserController is owned for the lifetime of one Revive queue. Each
// OAuth row gets a cancellable Run call, while CPA's native poll remains the
// authority for whether login and replacement validation succeeded.
type loginBrowserController interface {
	Close() error
	Run(context.Context, string, string, string, time.Time, func(string)) error
}

// workspaceTrialLoginBrowser is implemented by Phoenix's managed browser.
// Agent mode keeps navigation with the user, so its controller continues to
// use Run while the queue retries a mismatched workspace on the next OAuth
// attempt.
type workspaceTrialLoginBrowser interface {
	RunWorkspaceTrial(context.Context, string, string, string, string, int, time.Time, func(string)) error
}

func invalidAccount(a account, statusCode int) bool {
	if !a.Physical {
		return false
	}
	if statusCode == http.StatusUnauthorized {
		return true
	}
	if a.QuotaStatusCode == http.StatusUnauthorized {
		return true
	}
	value := strings.ToLower(a.Status + " " + a.StatusMessage)
	for _, marker := range invalidMarkers {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func invalidAccounts(accounts []account, statuses map[string]int) []account {
	out := make([]account, 0)
	for _, a := range accounts {
		if invalidAccount(a, statuses[a.Key]) {
			out = append(out, a)
		}
	}
	return out
}

type callbackForwarder struct {
	ln        net.Listener
	server    *http.Server
	closeOnce sync.Once
	stateMu   sync.RWMutex
	state     string
}

var nativeCallbackTarget = "http://127.0.0.1:8317/codex/callback"

func bindCallbackForwarder() (*callbackForwarder, error) {
	// Keep CPA's registered OAuth redirect URI byte-for-byte intact. The native
	// flow uses localhost, and the provider rejects a rewritten 127.0.0.1 URI.
	// Listening on localhost lets the platform select the same loopback family
	// that the browser uses for that native callback.
	ln, err := net.Listen("tcp", "localhost:1455")
	if err != nil {
		return nil, errors.New("callback_port_unavailable")
	}
	f := &callbackForwarder{ln: ln}
	f.server = &http.Server{Handler: http.HandlerFunc(f.handle)}
	go func() {
		if err := f.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			// The queue observes callback completion through native status; no
			// response body or listener error is persisted or exposed.
		}
	}()
	return f, nil
}

func (f *callbackForwarder) setState(state string) {
	f.stateMu.Lock()
	f.state = strings.TrimSpace(state)
	f.stateMu.Unlock()
}

func (f *callbackForwarder) expectedState() string {
	f.stateMu.RLock()
	defer f.stateMu.RUnlock()
	return f.state
}

func (f *callbackForwarder) handle(w http.ResponseWriter, r *http.Request) {
	if accepted := forwardCallbackWithState(w, r, f.expectedState()); accepted {
		// The OAuth callback is one-shot. Close the listener after forwarding the
		// code/error so duplicate callbacks cannot be accepted while CPA polls.
		go f.Close()
	}
}

func (f *callbackForwarder) Close() {
	if f == nil {
		return
	}
	f.closeOnce.Do(func() {
		if f.server != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = f.server.Shutdown(ctx)
			cancel()
			_ = f.server.Close()
		}
		if f.ln != nil {
			_ = f.ln.Close()
		}
	})
}

func forwardCallback(w http.ResponseWriter, r *http.Request) {
	_ = forwardCallbackWithState(w, r, "")
}

func forwardCallbackWithState(w http.ResponseWriter, r *http.Request, expectedState string) bool {
	if r.Method != http.MethodGet || r.URL.Path != "/auth/callback" {
		http.NotFound(w, r)
		return false
	}
	query := r.URL.Query()
	state := strings.TrimSpace(query.Get("state"))
	if state == "" || expectedState == "" || state != expectedState || (query.Get("code") == "" && query.Get("error") == "") {
		http.Error(w, "callback_rejected", http.StatusBadRequest)
		return false
	}
	target, err := url.Parse(nativeCallbackTarget)
	if err != nil || target.Scheme != "http" || target.Host != "127.0.0.1:8317" || target.Path != "/codex/callback" {
		http.Error(w, "callback_failed", http.StatusBadGateway)
		return false
	}
	target.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target.String(), nil)
	if err != nil {
		http.Error(w, "callback_failed", http.StatusBadGateway)
		return false
	}
	// The callback query contains a one-time OAuth code. Keep forwarding pinned
	// to CPA's loopback callback and never follow a redirect to another origin.
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "callback_failed", http.StatusBadGateway)
		return false
	}
	defer resp.Body.Close()
	// CPA returns a small completion page. Relay its headers/body so the
	// browser does not appear to hang on the callback URL after sign-in.
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 1<<20))
	return true
}

// composeOAuthURLMode keeps native PKCE/query bytes intact while optionally
// forcing the provider's account chooser. Duplicate local seats can share an
// email address, so a login hint would otherwise make it too easy to select
// the wrong workspace.
func composeOAuthURLMode(raw, email string, selectAccount bool) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return "", errors.New("oauth_url_invalid")
	}
	// Validate and edit only query-field boundaries. Reconstructing through
	// url.Values would reorder fields and normalize PKCE/redirect escapes, so
	// preserve every retained segment byte-for-byte (apart from the narrowly
	// normalized loopback callback below) and append the two Butler prefill
	// fields at the end.
	fragmentAt := strings.IndexByte(raw, '#')
	withoutFragment := raw
	fragment := ""
	if fragmentAt >= 0 {
		withoutFragment, fragment = raw[:fragmentAt], raw[fragmentAt:]
	}
	queryAt := strings.IndexByte(withoutFragment, '?')
	prefix, rawQuery := withoutFragment, ""
	if queryAt >= 0 {
		prefix, rawQuery = withoutFragment[:queryAt], withoutFragment[queryAt+1:]
	}
	if _, queryErr := url.ParseQuery(rawQuery); queryErr != nil {
		return "", errors.New("oauth_url_invalid")
	}
	kept := make([]string, 0)
	if rawQuery != "" {
		for _, segment := range strings.Split(rawQuery, "&") {
			if segment == "" {
				kept = append(kept, segment)
				continue
			}
			keyRaw := segment
			if equalsAt := strings.IndexByte(segment, '='); equalsAt >= 0 {
				keyRaw = segment[:equalsAt]
			}
			key, keyErr := url.QueryUnescape(keyRaw)
			if keyErr != nil || strings.HasPrefix(key, "amp;") {
				return "", errors.New("oauth_url_invalid")
			}
			// Validate escaped values without using their decoded form in the
			// output; this catches malformed native URLs while preserving bytes.
			valueRaw := ""
			if equalsAt := strings.IndexByte(segment, '='); equalsAt >= 0 {
				valueRaw = segment[equalsAt+1:]
			}
			if _, valueErr := url.QueryUnescape(valueRaw); valueErr != nil {
				return "", errors.New("oauth_url_invalid")
			}
			if key == "login_hint" || key == "prompt" {
				continue
			}
			// Do not rewrite redirect_uri. OAuth providers validate the exact
			// registered URI; CPA's native flow advertises localhost and Phoenix
			// now listens on that same loopback name.
			kept = append(kept, segment)
		}
	}
	// Butler leaves CPA's native URL untouched when no queued email is
	// available. Keep that behavior rather than emitting an empty hint or
	// rewriting an existing native prompt, but only after the same residual
	// entity/malformed-query validation above.
	if strings.TrimSpace(email) == "" {
		return raw, nil
	}
	prefill := []string{"login_hint=" + url.QueryEscape(strings.TrimSpace(email)), "prompt=login"}
	if selectAccount {
		// Keep the chooser for duplicate-email seats, but prefill the email so a
		// fresh sign-in does not make the operator type it again.
		prefill[1] = "prompt=select_account"
	}
	parts := append(kept, prefill...)
	return prefix + "?" + strings.Join(parts, "&") + fragment, nil
}

func safeQuarantine(src, authDir, root string) (string, error) {
	srcAbs, err := filepath.Abs(src)
	if err != nil {
		return "", errors.New("unsafe_auth_path")
	}
	dirAbs, err := filepath.Abs(authDir)
	if err != nil {
		return "", errors.New("unsafe_auth_path")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", errors.New("unsafe_quarantine_path")
	}
	relRoot, relErr := filepath.Rel(dirAbs, rootAbs)
	rootInsideAuth := relErr == nil && (relRoot == "." || (relRoot != ".." && !strings.HasPrefix(relRoot, ".."+string(filepath.Separator))))
	if filepath.Dir(srcAbs) != dirAbs || filepath.Ext(srcAbs) == "" || !strings.EqualFold(filepath.Ext(srcAbs), ".json") || rootInsideAuth {
		return "", errors.New("unsafe_auth_path")
	}
	if pathHasSymlink(rootAbs) {
		return "", errors.New("unsafe_quarantine_path")
	}
	info, err := os.Lstat(srcAbs)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("unsafe_auth_path")
	}
	if err := os.MkdirAll(rootAbs, 0700); err != nil {
		return "", errors.New("quarantine_unavailable")
	}
	resolvedRoot, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", errors.New("unsafe_quarantine_path")
	}
	resolvedDir, err := filepath.EvalSymlinks(dirAbs)
	if err != nil {
		return "", errors.New("unsafe_auth_path")
	}
	if rel, relErr := filepath.Rel(resolvedDir, resolvedRoot); relErr == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))) {
		return "", errors.New("unsafe_quarantine_path")
	}
	if err := os.Chmod(rootAbs, 0700); err != nil {
		return "", errors.New("quarantine_unavailable")
	}
	target := filepath.Join(rootAbs, fmt.Sprintf("%d-%s", time.Now().UnixNano(), filepath.Base(srcAbs)))
	if err := os.Rename(srcAbs, target); err != nil {
		return "", errors.New("quarantine_failed")
	}
	if err := os.Chmod(target, 0600); err != nil {
		return target, errors.New("quarantine_permissions")
	}
	return target, nil
}

func pathHasSymlink(path string) bool {
	path, err := filepath.Abs(path)
	if err != nil {
		return true
	}
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil && info.Mode()&os.ModeSymlink != 0 && current != "/var" && current != "/tmp" {
			return true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return false
		}
	}
}

type reviveResponse struct {
	JobID        string `json:"job_id"`
	State        string `json:"state"`
	Total        int    `json:"total"`
	AwaitingUser bool   `json:"awaiting_user,omitempty"`
}

type reviveCodeProbe struct {
	attempt   string
	recipient string
	positive  *thunderbirdCode
	negative  string
	checkedAt time.Time
	inFlight  bool
}

// Dashboard polls once per second; keep a negative result alive across the
// next normal poll so one empty mailbox cannot trigger a scan every tick.
const reviveCodeNegativeTTL = 1500 * time.Millisecond

type reviveRuntime struct {
	mu          sync.Mutex
	jobID       string
	authHeader  string
	browserMode string
	forwarder   *callbackForwarder
	current     int
	startAt     int
	state       string
	oauthURL    string
	oauthState  string
	browser     loginBrowserController
	// automationStatus is a transient, sanitized controller status. It is never
	// written to SQLite or logs and intentionally remains visible between OAuth
	// URL publication and native replacement validation.
	automationStatus      string
	selectedWorkspaceName string
	// verificationRequestedAt is runtime-only and gates Agent Autofill matches so
	// a code from an earlier OAuth attempt can never be reused.
	verificationRequestedAt time.Time
	codeProbe               reviveCodeProbe
	queued                  []account
	started                 bool
	cancel                  context.CancelFunc
	// attemptCancel scopes cancellation to the current native OAuth attempt.
	// The queue's cancel function remains reserved for the whole Revive job.
	attemptCancel         context.CancelFunc
	skipRequested         bool
	skipReason            string
	nativeOAuthCancelOnce sync.Once
}

func normalizeReviveBrowserMode(mode string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", reviveBrowserModeAutomatic:
		return reviveBrowserModeAutomatic, nil
	case reviveBrowserModeAgent:
		return reviveBrowserModeAgent, nil
	default:
		return "", errInvalidBrowserMode
	}
}

func (runtime *reviveRuntime) agentBrowserMode() bool {
	return runtime != nil && runtime.browserMode == reviveBrowserModeAgent
}

var reviveRuntimeState = struct {
	sync.Mutex
	jobs map[string]*reviveRuntime
}{jobs: make(map[string]*reviveRuntime)}

func stopAllRevive() {
	reviveRuntimeState.Lock()
	jobs := make([]*reviveRuntime, 0, len(reviveRuntimeState.jobs))
	for _, runtime := range reviveRuntimeState.jobs {
		jobs = append(jobs, runtime)
	}
	reviveRuntimeState.Unlock()
	for _, runtime := range jobs {
		runtime.mu.Lock()
		cancel := runtime.cancel
		state := runtime.oauthState
		runtime.mu.Unlock()
		if state != "" {
			_ = cancelNativeOAuth(context.Background(), runtime.authHeader, state)
		}
		if cancel != nil {
			cancel()
		}
		runtime.mu.Lock()
		if runtime.forwarder != nil {
			runtime.forwarder.Close()
		}
		runtime.mu.Unlock()
	}
}

type reviveJobSnapshot struct {
	ID      string
	State   string
	Total   int
	Done    int
	Rows    []account
	Pending []account
}

func latestReviveJob() (reviveJobSnapshot, bool, error) {
	db, err := openStore()
	if err != nil {
		return reviveJobSnapshot{}, false, err
	}
	var id string
	err = db.QueryRow(`SELECT id FROM jobs WHERE kind='revive' ORDER BY updated_at DESC,created_at DESC,rowid DESC LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return reviveJobSnapshot{}, false, nil
	}
	if err != nil {
		return reviveJobSnapshot{}, false, err
	}
	snapshot, err := loadReviveJobSnapshot(id)
	if err != nil {
		return reviveJobSnapshot{}, false, err
	}
	return snapshot, true, nil
}

// latestRecoverableReviveJob follows the same single-queue rule as the
// dashboard projection, while allowing a newer disjoint queue to finish after
// an older timed-out row. A recovery request names exact seat labels, so the
// caller still validates every requested seat against the selected snapshot.
func latestRecoverableReviveJob() (reviveJobSnapshot, bool, error) {
	db, err := openStore()
	if err != nil {
		return reviveJobSnapshot{}, false, err
	}
	var id string
	err = db.QueryRow(`SELECT j.id
FROM jobs j
WHERE j.kind='revive' AND j.state IN ('failed','cancelled','superseded')
	  AND NOT (j.state='superseded' AND j.reason IN ('owner_reset','auto_reset_recoverable_skips'))
  AND EXISTS (SELECT 1 FROM repair_rows r
    WHERE r.job_id=j.id AND r.state IN ('skipped','failed')
      AND r.reason IN ('oauth_timeout','workspace_missing'))
ORDER BY j.updated_at DESC,j.created_at DESC,j.rowid DESC LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return reviveJobSnapshot{}, false, nil
	}
	if err != nil {
		return reviveJobSnapshot{}, false, err
	}
	snapshot, err := loadReviveJobSnapshot(id)
	if err != nil {
		return reviveJobSnapshot{}, false, err
	}
	return snapshot, true, nil
}

func recoverableReviveJobForSeats(wanted map[string]struct{}) (reviveJobSnapshot, bool, error) {
	db, err := openStore()
	if err != nil {
		return reviveJobSnapshot{}, false, err
	}
	rows, err := db.Query(`SELECT j.id FROM jobs j
WHERE j.kind='revive' AND j.state IN ('failed','cancelled','superseded')
	  AND NOT (j.state='superseded' AND j.reason IN ('owner_reset','auto_reset_recoverable_skips'))
  AND EXISTS (SELECT 1 FROM repair_rows r WHERE r.job_id=j.id
    AND r.state IN ('skipped','failed') AND r.reason IN ('oauth_timeout','workspace_missing'))
ORDER BY j.updated_at DESC,j.created_at DESC,j.rowid DESC`)
	if err != nil {
		return reviveJobSnapshot{}, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return reviveJobSnapshot{}, false, err
		}
		snapshot, err := loadReviveJobSnapshot(id)
		if err != nil {
			return reviveJobSnapshot{}, false, err
		}
		seen := make(map[string]struct{})
		for i, row := range snapshot.Rows {
			if row.RepairState != "skipped" && row.RepairState != "failed" {
				continue
			}
			reason := rowReason(snapshot.ID, i+1)
			if reason == "oauth_timeout" || reason == reviveSkipWorkspaceMissingReason {
				seen[strings.ToLower(stableSeatLabel(row))] = struct{}{}
			}
		}
		match := true
		for seat := range wanted {
			if _, ok := seen[seat]; !ok {
				match = false
				break
			}
		}
		if match {
			return snapshot, true, nil
		}
	}
	return reviveJobSnapshot{}, false, rows.Err()
}

func loadReviveJobSnapshot(id string) (reviveJobSnapshot, error) {
	db, err := openStore()
	if err != nil {
		return reviveJobSnapshot{}, err
	}
	var snapshot reviveJobSnapshot
	err = db.QueryRow(`SELECT id,state,total,done FROM jobs WHERE id=? AND kind='revive'`, strings.TrimSpace(id)).Scan(&snapshot.ID, &snapshot.State, &snapshot.Total, &snapshot.Done)
	if err != nil {
		return reviveJobSnapshot{}, err
	}
	snapshot.Rows, err = loadRepairRows(snapshot.ID)
	if err != nil {
		return reviveJobSnapshot{}, err
	}
	for _, row := range snapshot.Rows {
		if row.RepairState != "repaired" {
			snapshot.Pending = append(snapshot.Pending, row)
		}
	}
	return snapshot, nil
}

// The latest outcome for each exact identity decides whether it was skipped.
// An older skipped job must not override a later successful recovery.
func skippedRepairIdentities() ([]account, error) {
	db, err := openStore()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`WITH outcomes AS (
 SELECT r.account_id,r.email,r.state,
 ROW_NUMBER() OVER (PARTITION BY TRIM(r.account_id),LOWER(TRIM(r.email)) ORDER BY j.created_at DESC,j.rowid DESC,r.ordinal DESC) AS latest
 FROM repair_rows r JOIN jobs j ON j.id=r.job_id
 WHERE j.kind='revive' AND j.state<>'superseded' AND TRIM(r.account_id)<>''
) SELECT account_id,email FROM outcomes WHERE latest=1 AND state='skipped'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var skipped []account
	for rows.Next() {
		var a account
		if err := rows.Scan(&a.AccountID, &a.Email); err != nil {
			return nil, err
		}
		skipped = append(skipped, a)
	}
	return skipped, rows.Err()
}

// currentRepairSnapshot is the one bounded inventory/quota probe used by
// Revive startup. Keeping the capture in one path ensures disposition and a
// newly-created queue see the same exact private identities.
func currentRepairSnapshot() ([]account, error) {
	accounts, err := repairListAccounts()
	if err != nil {
		return nil, err
	}
	rows := make([]account, 0, len(accounts))
	for _, a := range accounts {
		if !a.Physical {
			continue
		}
		if (a.Disabled || a.Expired || a.Unavailable) && !invalidAuthDecision(a) {
			continue
		}
		// Use the same quota-auth probe as the scan before snapshotting rows so
		// an exact provider 401 cannot be missed while status remains active.
		if !a.Disabled && !a.Expired && !a.Unavailable {
			a = repairRefreshQuota(a)
		}
		if invalidAuthDecision(a) {
			rows = append(rows, a)
		}
	}
	captured := captureRepairRows(rows)
	if len(captured) != len(rows) {
		// A dropped private identity makes a zero-overlap result inconclusive.
		// Fail closed before disposition so a partial snapshot can never
		// supersede a recoverable historical queue.
		return nil, errRepairIdentityUnavailable
	}
	return captured, nil
}

func normalizedRepairIdentity(a account) (accountID, email string) {
	return strings.TrimSpace(a.AccountID), strings.ToLower(strings.TrimSpace(a.Email))
}

func exactRepairIdentity(a, b account) bool {
	aID, aEmail := normalizedRepairIdentity(a)
	bID, bEmail := normalizedRepairIdentity(b)
	return aID != "" && aID == bID && aEmail == bEmail
}

func repairRowsOverlap(pending, current []account) bool {
	for _, oldRow := range pending {
		for _, currentRow := range current {
			if exactRepairIdentity(oldRow, currentRow) {
				return true
			}
		}
	}
	return false
}

func repairRowsContainState(rows []account, state string) bool {
	for _, row := range rows {
		if row.RepairState == state {
			return true
		}
	}
	return false
}

func repairRowsWithState(rows []account, state string) []account {
	matched := make([]account, 0)
	for _, row := range rows {
		if row.RepairState == state {
			matched = append(matched, row)
		}
	}
	return matched
}

func startReviveSnapshot(headers map[string][]string, mode string, rows []account, supersedeID string) (reviveResponse, error) {
	id, err := globalJobs.startRevive(rows, supersedeID)
	if err != nil {
		return reviveResponse{}, err
	}
	runtime := &reviveRuntime{jobID: id, authHeader: managementAuthorization(headers), browserMode: mode, state: "queued", queued: rows}
	reviveRuntimeState.Lock()
	reviveRuntimeState.jobs[id] = runtime
	reviveRuntimeState.Unlock()
	// Start the first row only after the durable queue and any predecessor
	// transition have committed. A callback-port failure therefore cannot leave
	// a partially-created replacement queue.
	go runReviveQueue(runtime)
	return reviveResponse{JobID: id, State: "running", Total: len(rows)}, nil
}

func beginRevive(headers map[string][]string, requestedMode ...string) (reviveResponse, error) {
	mode := reviveBrowserModeAutomatic
	if len(requestedMode) > 0 {
		mode = requestedMode[0]
	}
	mode, err := normalizeReviveBrowserMode(mode)
	if err != nil {
		return reviveResponse{}, err
	}
	// Active work owns the global gate. Do not probe or supersede a running
	// Revive/Ignite job; the existing management action remains the authority.
	if anyJobActive() {
		return reviveResponse{}, errIgniteJobActive
	}
	prior, hasPrior, err := latestReviveJob()
	if err != nil {
		return reviveResponse{}, err
	}
	rows, err := currentRepairSnapshot()
	if err != nil {
		return reviveResponse{}, err
	}
	skipped, err := skippedRepairIdentities()
	if err != nil {
		return reviveResponse{}, err
	}
	actionable := rows[:0]
	for _, row := range rows {
		if !repairRowsOverlap(skipped, []account{row}) {
			actionable = append(actionable, row)
		}
	}
	rows = actionable
	if hasPrior && len(prior.Pending) > 0 && (prior.State == "failed" || prior.State == "cancelled") {
		pending := make([]account, 0, len(prior.Pending))
		for _, row := range prior.Pending {
			if row.RepairState != "skipped" {
				pending = append(pending, row)
			}
		}
		if len(pending) > 0 && (len(rows) == 0 || repairRowsOverlap(pending, rows)) {
			if resumed, ok := resumeRevive(headers, mode); ok {
				return resumed, nil
			}
			return reviveResponse{}, errIgniteJobActive
		}
		if len(rows) > 0 {
			// Keep skipped records and their quarantine visible as unresolved.
			// A normal Revive only resumes actionable rows or starts fresh work.
			supersedeID := prior.ID
			if repairRowsContainState(prior.Pending, "skipped") {
				supersedeID = ""
			}
			return startReviveSnapshot(headers, mode, rows, supersedeID)
		}
	}
	if len(rows) == 0 {
		return reviveResponse{}, errNoActionableAccounts
	}
	return startReviveSnapshot(headers, mode, rows, "")
}

func captureRepairIdentity(a account) account {
	hydrated := refreshCredential(a)
	a.AccountID = strings.TrimSpace(hydrated.AccountID)
	a.AccessTokenValue = ""
	a.AccessToken = false
	return a
}

func captureRepairRows(rows []account) []account {
	captured := make([]account, 0, len(rows))
	for _, row := range rows {
		row = captureRepairIdentity(row)
		if strings.TrimSpace(row.AccountID) == "" {
			continue
		}
		captured = append(captured, row)
	}
	return captured
}

func resumeRevive(headers map[string][]string, requestedMode ...string) (reviveResponse, bool) {
	mode := reviveBrowserModeAutomatic
	if len(requestedMode) > 0 {
		mode = requestedMode[0]
	}
	mode, err := normalizeReviveBrowserMode(mode)
	if err != nil {
		return reviveResponse{}, false
	}
	snapshot, ok, err := latestReviveJob()
	if err != nil || !ok {
		return reviveResponse{}, false
	}
	return resumeReviveSnapshot(headers, mode, snapshot)
}

// recoverTimedOutRevive is the explicit owner-requested escape hatch for a
// bounded OAuth timeout or a manually paused exact-workspace mismatch.
// Automatic-browser rows may already carry a quarantine marker; regular-Chrome
// Agent rows intentionally do not. Provider-auth/deactivation skips never
// enter this path.
func recoverTimedOutRevive(headers map[string][]string, seats []string) (reviveResponse, error) {
	if anyJobActive() {
		return reviveResponse{}, errIgniteJobActive
	}
	wanted := make(map[string]struct{}, len(seats))
	for _, seat := range seats {
		seat = strings.ToLower(strings.TrimSpace(seat))
		if seat != "" {
			wanted[seat] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return reviveResponse{}, errRecoverySeatMismatch
	}
	snapshot, ok, err := recoverableReviveJobForSeats(wanted)
	if err != nil {
		return reviveResponse{}, err
	}
	if !ok || (snapshot.State != "failed" && snapshot.State != "cancelled" && snapshot.State != "superseded") {
		return reviveResponse{}, errNoRecoverableSkippedAccounts
	}
	recoverableReasons := map[string]struct{}{"oauth_timeout": {}, reviveSkipWorkspaceMissingReason: {}}
	recoverOrdinals := make([]int, 0)
	for i, row := range snapshot.Rows {
		seat := strings.ToLower(stableSeatLabel(row))
		if _, selected := wanted[seat]; !selected || (row.RepairState != "skipped" && row.RepairState != "failed") {
			continue
		}
		if _, recoverable := recoverableReasons[rowReason(snapshot.ID, i+1)]; !recoverable {
			continue
		}
		recoverOrdinals = append(recoverOrdinals, i+1)
	}
	if len(recoverOrdinals) != len(wanted) {
		return reviveResponse{}, errRecoverySeatMismatch
	}
	db, err := openStore()
	if err != nil {
		return reviveResponse{}, err
	}
	tx, err := db.Begin()
	if err != nil {
		return reviveResponse{}, err
	}
	for _, ordinal := range recoverOrdinals {
		if _, err := tx.Exec(`UPDATE repair_rows SET state='queued',reason='' WHERE job_id=? AND ordinal=? AND state IN ('skipped','failed') AND reason IN ('oauth_timeout','workspace_missing')`, snapshot.ID, ordinal); err != nil {
			_ = tx.Rollback()
			return reviveResponse{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return reviveResponse{}, err
	}
	if snapshot.State == "superseded" {
		if _, err := db.Exec(`UPDATE jobs SET state='failed',reason='',updated_at=? WHERE id=? AND state='superseded'`, time.Now().Unix(), snapshot.ID); err != nil {
			return reviveResponse{}, err
		}
		snapshot.State = "failed"
	}
	snapshot, err = loadReviveJobSnapshot(snapshot.ID)
	if err != nil {
		return reviveResponse{}, err
	}
	response, resumed := resumeReviveSnapshot(headers, reviveBrowserModeAgent, snapshot)
	if !resumed {
		return reviveResponse{}, errNoRecoverableSkippedAccounts
	}
	return response, nil
}

// resetReviveQueue dismisses only terminal jobs whose skipped rows are
// recoverable timeout or workspace-mismatch outcomes. Their rows stay in the
// store for audit and a later scan can create a fresh queue for the identity.
func resetReviveQueue() (int, int, error) {
	globalJobs.mu.Lock()
	defer globalJobs.mu.Unlock()
	if globalJobs.active != "" {
		return 0, 0, errIgniteJobActive
	}
	db, err := openStore()
	if err != nil {
		return 0, 0, err
	}
	tx, err := db.Begin()
	if err != nil {
		return 0, 0, err
	}
	rollback := func() {
		_ = tx.Rollback()
	}
	var active int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM jobs WHERE state IN ('running','awaiting_user')`).Scan(&active); err != nil {
		rollback()
		return 0, 0, err
	}
	if active > 0 {
		rollback()
		return 0, 0, errIgniteJobActive
	}
	var jobs, resetRows int
	const resettable = `j.kind='revive' AND j.state IN ('failed','cancelled','completed')
  AND r.state='skipped'
  AND r.reason IN ('oauth_timeout','workspace_missing')
  AND NOT EXISTS (SELECT 1 FROM repair_rows pending WHERE pending.job_id=j.id AND pending.state NOT IN ('repaired','skipped'))
  AND NOT EXISTS (SELECT 1 FROM repair_rows blocked WHERE blocked.job_id=j.id AND blocked.state='skipped' AND blocked.reason NOT IN ('oauth_timeout','workspace_missing'))`
	if err := tx.QueryRow(`SELECT COUNT(DISTINCT j.id), COUNT(r.ordinal)
FROM jobs j JOIN repair_rows r ON r.job_id=j.id WHERE `+resettable).Scan(&jobs, &resetRows); err != nil {
		rollback()
		return 0, 0, err
	}
	if jobs == 0 {
		rollback()
		return 0, 0, errNoResettableRepairQueue
	}
	if _, err := tx.Exec(`UPDATE jobs SET state='superseded',reason='owner_reset',updated_at=?
WHERE id IN (SELECT j.id FROM jobs j JOIN repair_rows r ON r.job_id=j.id WHERE `+resettable+`)`, time.Now().Unix()); err != nil {
		rollback()
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return jobs, resetRows, nil
}

func rowReason(jobID string, ordinal int) string {
	db, err := openStore()
	if err != nil {
		return ""
	}
	var reason string
	if err := db.QueryRow(`SELECT reason FROM repair_rows WHERE job_id=? AND ordinal=?`, jobID, ordinal).Scan(&reason); err != nil {
		return ""
	}
	return reason
}

func resumeReviveSnapshot(headers map[string][]string, mode string, snapshot reviveJobSnapshot) (reviveResponse, bool) {
	if snapshot.State == "completed" || snapshot.State == "superseded" {
		return reviveResponse{}, false
	}
	if len(snapshot.Pending) == 0 {
		return reviveResponse{}, false
	}
	if len(repairRowsWithState(snapshot.Pending, "skipped")) == len(snapshot.Pending) {
		return reviveResponse{}, false
	}
	if snapshot.State != "failed" && snapshot.State != "cancelled" && snapshot.State != "running" && snapshot.State != "awaiting_user" {
		// Preserve recovery for legacy terminal states written before the
		// resumable-state vocabulary was stabilized. Explicit completed and
		// superseded jobs remain terminal and are never revived.
		if db, openErr := openStore(); openErr == nil {
			_, _ = db.Exec(`UPDATE jobs SET state='failed',reason='interrupted',updated_at=? WHERE id=?`, time.Now().Unix(), snapshot.ID)
		}
		snapshot.State = "failed"
	}
	if err := globalJobs.claim(snapshot.ID); err != nil {
		return reviveResponse{}, false
	}
	// A crash can occur after a row is durably marked repaired but before the
	// aggregate job counter advances. Resume from the first non-repaired row so
	// a completed OAuth replacement is never quarantined or logged in again.
	startAt := 0
	for startAt < len(snapshot.Rows) && (snapshot.Rows[startAt].RepairState == "repaired" || snapshot.Rows[startAt].RepairState == "skipped") {
		startAt++
	}
	runtime := &reviveRuntime{jobID: snapshot.ID, authHeader: managementAuthorization(headers), browserMode: mode, state: snapshot.State, queued: snapshot.Rows, startAt: startAt}
	reviveRuntimeState.Lock()
	reviveRuntimeState.jobs[snapshot.ID] = runtime
	reviveRuntimeState.Unlock()
	go runReviveQueue(runtime)
	return reviveResponse{JobID: snapshot.ID, State: "running", Total: snapshot.Total, AwaitingUser: snapshot.State == "awaiting_user"}, true
}

func loadRepairRows(jobID string) ([]account, error) {
	db, err := openStore()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT account_key,auth_id,auth_index,email,account_id,workspace_name,auth_path,auth_dir,state,quarantine FROM repair_rows WHERE job_id=? ORDER BY ordinal`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var accounts []account
	for rows.Next() {
		var a account
		if err := rows.Scan(&a.Key, &a.AuthID, &a.AuthIndex, &a.Email, &a.AccountID, &a.WorkspaceName, &a.AuthPath, &a.AuthDir, &a.RepairState, &a.Quarantine); err != nil {
			return nil, err
		}
		a.Provider, a.Physical = "codex", true
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

func managementAuthorization(headers map[string][]string) string {
	for key, values := range headers {
		if strings.EqualFold(key, "authorization") && len(values) > 0 {
			return strings.TrimSpace(values[0])
		}
	}
	return ""
}

func runReviveQueue(runtime *reviveRuntime) {
	defer func() {
		reviveRuntimeState.Lock()
		delete(reviveRuntimeState.jobs, runtime.jobID)
		reviveRuntimeState.Unlock()
	}()
	ctx, cancel := context.WithCancel(context.Background())
	runtime.mu.Lock()
	runtime.cancel = cancel
	runtime.mu.Unlock()
	defer cancel()
	if !runtime.agentBrowserMode() {
		runtime.setAutomationStatus("browser_starting")
		browser, err := repairNewLoginBrowser(ctx)
		if err != nil || browser == nil {
			runtime.setAutomationStatus("browser_unavailable")
			if runtime.startAt >= 0 && runtime.startAt < len(runtime.queued) {
				_ = updateRepairRow(runtime.jobID, runtime.startAt+1, "failed", "browser_unavailable")
			}
			updateJob(runtime.jobID, "failed", "browser_unavailable", repairedRowCount(runtime.queued))
			return
		}
		runtime.mu.Lock()
		runtime.browser = browser
		runtime.mu.Unlock()
		defer func() {
			_ = browser.Close()
			runtime.mu.Lock()
			if runtime.browser == browser {
				runtime.browser = nil
			}
			runtime.mu.Unlock()
		}()
		runtime.setAutomationStatus("browser_ready")
	}
	var unresolvedReason string
	for ordinal, row := range runtime.queued {
		if row.RepairState == "skipped" && unresolvedReason == "" {
			unresolvedReason = rowReason(runtime.jobID, ordinal+1)
			if unresolvedReason == "" {
				unresolvedReason = reviveSkipProviderAuthReason
			}
			break
		}
	}
	for ordinal := runtime.startAt; ordinal < len(runtime.queued); {
		if runtime.queued[ordinal].RepairState == "repaired" || runtime.queued[ordinal].RepairState == "skipped" {
			if runtime.queued[ordinal].RepairState == "skipped" && unresolvedReason == "" {
				unresolvedReason = rowReason(runtime.jobID, ordinal+1)
				if unresolvedReason == "" {
					unresolvedReason = reviveSkipProviderAuthReason
				}
			}
			ordinal++
			continue
		}
		if ctx.Err() != nil {
			updateJob(runtime.jobID, "cancelled", "cancelled", ordinal)
			return
		}
		runtime.mu.Lock()
		runtime.current = ordinal
		runtime.mu.Unlock()
		state := processReviveRow(ctx, runtime, ordinal+1, runtime.queued[ordinal])
		if reviveAutoSkipState(state) {
			if err := updateRepairRow(runtime.jobID, ordinal+1, "skipped", state); err != nil {
				updateJob(runtime.jobID, "failed", "state_unavailable", repairedRowCount(runtime.queued))
				return
			}
			runtime.mu.Lock()
			runtime.skipReason = state
			runtime.mu.Unlock()
			state = "skipped"
		}
		if state == "repaired_other" {
			updateJob(runtime.jobID, "running", "", repairedRowCount(runtime.queued))
			continue
		}
		if state == "skipped" {
			runtime.mu.Lock()
			if ordinal >= 0 && ordinal < len(runtime.queued) {
				runtime.queued[ordinal].RepairState = "skipped"
			}
			if unresolvedReason == "" {
				unresolvedReason = runtime.skipReason
			}
			runtime.mu.Unlock()
			if unresolvedReason == "" {
				unresolvedReason = "provider_auth_error"
			}
			updateJob(runtime.jobID, "running", "", repairedRowCount(runtime.queued))
			ordinal++
			continue
		}
		if state != "repaired" {
			// An explicit cancellation owns the stopped row's recovery state:
			// leave its queued/quarantined marker intact so a later explicit
			// resume can make the same safe decision. Other row failures are
			// durably recorded, then the queue keeps working on later seats when
			// the failure does not invalidate the worker or its durable state.
			safeState := "failed"
			if state != "cancelled" {
				_ = updateRepairRow(runtime.jobID, ordinal+1, "failed", state)
				runtime.mu.Lock()
				if ordinal >= 0 && ordinal < len(runtime.queued) {
					runtime.queued[ordinal].RepairState = "failed"
				}
				if unresolvedReason == "" {
					unresolvedReason = state
				}
				runtime.mu.Unlock()
			} else {
				safeState = "cancelled"
			}
			if state != "cancelled" && !reviveFailureStopsQueue(state) {
				updateJob(runtime.jobID, "running", "", repairedRowCount(runtime.queued))
				ordinal++
				continue
			}
			updateJob(runtime.jobID, safeState, state, ordinal)
			return
		}
		runtime.mu.Lock()
		runtime.queued[ordinal].RepairState = "repaired"
		runtime.mu.Unlock()
		updateJob(runtime.jobID, "running", "", repairedRowCount(runtime.queued))
		ordinal++
	}
	for ordinal, row := range runtime.queued {
		if row.RepairState == "skipped" && unresolvedReason == "" {
			unresolvedReason = rowReason(runtime.jobID, ordinal+1)
			if unresolvedReason == "" {
				unresolvedReason = reviveSkipProviderAuthReason
			}
			break
		}
	}
	if unresolvedReason != "" {
		if autoDismissRecoverableSkippedJob(runtime.jobID) {
			updateJob(runtime.jobID, "superseded", "auto_reset_recoverable_skips", repairedRowCount(runtime.queued))
			return
		}
		updateJob(runtime.jobID, "failed", unresolvedReason, repairedRowCount(runtime.queued))
		return
	}
	updateJob(runtime.jobID, "completed", "", len(runtime.queued))
}

func autoDismissRecoverableSkippedJob(jobID string) bool {
	db, err := openStore()
	if err != nil {
		return false
	}
	var total, repaired, recoverableSkipped int
	err = db.QueryRow(`SELECT COUNT(*),
SUM(CASE WHEN state='repaired' THEN 1 ELSE 0 END),
SUM(CASE WHEN state='skipped' AND reason IN ('oauth_timeout','workspace_missing') THEN 1 ELSE 0 END)
FROM repair_rows WHERE job_id=?`, jobID).Scan(&total, &repaired, &recoverableSkipped)
	return err == nil && total > 0 && recoverableSkipped > 0 && repaired+recoverableSkipped == total
}

// A row-level OAuth or inventory failure should not strand every later seat in
// the same queue. The worker must still stop when its browser, durable store,
// or explicit job cancellation is unavailable, because continuing would lose
// the queue's authoritative state or repeat an unsafe operation.
func reviveFailureStopsQueue(state string) bool {
	switch strings.TrimSpace(state) {
	case "cancelled", "browser_unavailable", "state_unavailable", "missing_identity":
		return true
	default:
		return false
	}
}

func reviveAutoSkipState(state string) bool {
	switch strings.TrimSpace(state) {
	case reviveSkipProviderAuthReason, "oauth_timeout":
		return true
	default:
		return false
	}
}

func repairedRowCount(rows []account) int {
	count := 0
	for _, row := range rows {
		if row.RepairState == "repaired" {
			count++
		}
	}
	return count
}

func processReviveRow(ctx context.Context, runtime *reviveRuntime, ordinal int, expected account) string {
	if strings.TrimSpace(expected.AccountID) == "" {
		return "missing_identity"
	}
	accounts, err := repairListAccounts()
	if err != nil {
		return "inventory_unavailable"
	}
	before := append([]account(nil), accounts...)
	alreadyQuarantined := expected.Quarantine != "" || expected.RepairState == "quarantined" || expected.RepairState == "awaiting_user"
	var current account
	found := false
	if alreadyQuarantined {
		// The predecessor path is intentionally not persisted. Once the row is
		// marked quarantined, resume from the private identity even if CPA's
		// inventory still briefly reports a stale old record.
		current, found = expected, true
	} else {
		current, found = resolveRepairAccount(accounts, expected)
	}
	if !found && !alreadyQuarantined {
		return "inventory_changed"
	}
	if !found {
		current = expected
	}
	if !current.Physical || (!alreadyQuarantined && (current.AuthPath == "" || current.AuthDir == "")) {
		return "inventory_changed"
	}
	if ctx.Err() != nil {
		return "cancelled"
	}
	if alreadyQuarantined {
		// A prior callback can succeed before CPA's host inventory exposes the
		// replacement. On explicit resume, reconcile an exact private-identity
		// replacement before starting another OAuth session. A direct 2xx quota
		// read is the authority that the new credential is healthy; shared email
		// and stale host keys are never enough.
		if replacement, ok := resolveRepairAccount(accounts, expected); ok {
			replacement = repairRefreshQuota(replacement)
			if definitiveHealthy(replacement) {
				if err := updateRepairRow(runtime.jobID, ordinal, "repaired", "already_healthy"); err != nil {
					return "state_unavailable"
				}
				return "repaired"
			}
		}
	}
	if !alreadyQuarantined {
		if !current.Disabled && !current.Expired && !current.Unavailable && !definitiveHealthy(current) {
			current = repairRefreshQuota(current)
		}
		// A successful direct quota read is authoritative even when CPA's
		// runtime projection still carries a stale authentication marker.
		if definitiveHealthy(current) {
			if err := updateRepairRow(runtime.jobID, ordinal, "repaired", "already_healthy"); err != nil {
				return "state_unavailable"
			}
			return "repaired"
		}
		if !invalidAuthDecision(current) {
			return "no_longer_invalid"
		}
	}
	runtime.mu.Lock()
	browser := runtime.browser
	agentMode := runtime.agentBrowserMode()
	runtime.mu.Unlock()
	// Queue startup owns the managed browser before any predecessor can be
	// quarantined. Agent mode deliberately leaves navigation to the caller and
	// therefore has no browser controller.
	if !agentMode && browser == nil {
		return "browser_unavailable"
	}
	// Own the exact callback port for this row only. A later queued row must
	// prove ownership again after this listener is closed.
	forwarder, err := repairBindCallbackForwarder()
	if err != nil {
		return "callback_port_unavailable"
	}
	runtime.mu.Lock()
	runtime.forwarder = forwarder
	runtime.started = true
	runtime.mu.Unlock()
	defer func() {
		forwarder.Close()
		runtime.mu.Lock()
		if runtime.forwarder == forwarder {
			runtime.forwarder = nil
		}
		runtime.mu.Unlock()
	}()
	if err := updateRepairRow(runtime.jobID, ordinal, "revalidating", ""); err != nil {
		return "state_unavailable"
	}
	if !alreadyQuarantined && !agentMode {
		stateRoot, rootErr := userStateRoot()
		if rootErr != nil {
			return "quarantine_unavailable"
		}
		root := filepath.Join(stateRoot, "cpa-phoenix", "quarantine")
		quarantine, err := repairSafeQuarantine(current.AuthPath, current.AuthDir, root)
		if err != nil {
			return "quarantine_failed"
		}
		if err := updateRepairRow(runtime.jobID, ordinal, "quarantined", "", quarantine); err != nil {
			return "state_unavailable"
		}
		runtime.mu.Lock()
		if ordinal > 0 && ordinal <= len(runtime.queued) {
			runtime.queued[ordinal-1].RepairState = "quarantined"
			runtime.queued[ordinal-1].Quarantine = filepath.Base(quarantine)
		}
		runtime.mu.Unlock()
		if err := waitForOldRecordGone(ctx, current); err != nil {
			return err.Error()
		}
	}
	for workspaceTrial := 0; workspaceTrial < maxWorkspaceTrials; workspaceTrial++ {
		replacement, outcome := runReviveOAuthAttempt(ctx, runtime, browser, agentMode, current, expected, before, ordinal, workspaceTrial)
		if outcome == "retry_workspace" {
			continue
		}
		if outcome != "repaired" && outcome != "repaired_other" {
			return outcome
		}
		target := queuedReplacementOrdinal(runtime.queued, replacement)
		if target == 0 {
			return "replacement_not_queued"
		}
		runtime.mu.Lock()
		if runtime.skipRequested {
			runtime.mu.Unlock()
			return "skipped"
		}
		if err := updateRepairRow(runtime.jobID, target, "repaired", ""); err != nil {
			runtime.mu.Unlock()
			return "state_unavailable"
		}
		name := runtime.selectedWorkspaceName
		if name != "" {
			_ = updateRepairWorkspaceName(runtime.jobID, target, name)
		}
		if target > 0 && target <= len(runtime.queued) {
			runtime.queued[target-1].RepairState = "repaired"
			runtime.queued[target-1].WorkspaceName = name
		}
		runtime.mu.Unlock()
		if target != ordinal {
			return "repaired_other"
		}
		return "repaired"
	}
	return "workspace_trials_exhausted"
}

// runReviveOAuthAttempt owns exactly one native OAuth session. A successful
// callback with a different workspace is deliberately retryable: the next
// attempt presents the next provider choice, while the callback validator
// remains the authority for declaring the queued seat repaired.
func runReviveOAuthAttempt(ctx context.Context, runtime *reviveRuntime, browser loginBrowserController, agentMode bool, current, expected account, before []account, ordinal, workspaceTrial int) (account, string) {
	if ctx.Err() != nil {
		return account{}, "cancelled"
	}
	runtime.mu.Lock()
	requestedAt := time.Now()
	runtime.verificationRequestedAt = requestedAt
	runtime.codeProbe = reviveCodeProbe{}
	runtime.skipRequested = false
	runtime.skipReason = ""
	runtime.selectedWorkspaceName = ""
	runtime.nativeOAuthCancelOnce = sync.Once{}
	runtime.mu.Unlock()
	attemptCtx, cancelAttempt := context.WithCancel(ctx)
	runtime.mu.Lock()
	runtime.attemptCancel = cancelAttempt
	runtime.mu.Unlock()
	defer func() {
		cancelAttempt()
		runtime.mu.Lock()
		runtime.attemptCancel = nil
		runtime.mu.Unlock()
	}()
	selectAccount := queuedEmailCount(runtime.queued, current.Email) > 1
	var started nativeOAuthStart
	var err error
	if selectAccount {
		started, err = repairStartNativeOAuthMode(attemptCtx, runtime.authHeader, current.Email, true)
	} else {
		started, err = repairStartNativeOAuth(attemptCtx, runtime.authHeader, current.Email)
	}
	if err != nil {
		return account{}, "oauth_start_failed"
	}
	oauthSucceeded := false
	defer func() {
		if !oauthSucceeded {
			runtime.cancelNativeOAuthOnce(runtime.authHeader, started.State)
		}
		runtime.mu.Lock()
		runtime.oauthURL = ""
		runtime.oauthState = ""
		if runtime.forwarder != nil {
			runtime.forwarder.setState("")
		}
		runtime.mu.Unlock()
	}()
	runtime.forwarder.setState(started.State)
	runtime.mu.Lock()
	runtime.oauthURL, runtime.oauthState = started.URL, started.State
	runtime.mu.Unlock()
	providerAuthError := func() bool { return false }
	if !agentMode {
		browserDone := make(chan struct{})
		browserResult := make(chan error, 1)
		report := func(status string) {
			if strings.HasPrefix(status, "workspace_name:") {
				runtime.setWorkspaceName(strings.TrimPrefix(status, "workspace_name:"))
				return
			}
			runtime.setAutomationStatus(status)
		}
		go func() {
			defer close(browserDone)
			var browserErr error
			if trialBrowser, ok := browser.(workspaceTrialLoginBrowser); ok {
				browserErr = trialBrowser.RunWorkspaceTrial(attemptCtx, started.URL, current.Email, expected.AccountID, expected.WorkspaceName, workspaceTrial, requestedAt, report)
			} else {
				browserErr = browser.Run(attemptCtx, started.URL, current.Email, expected.AccountID, requestedAt, report)
			}
			if browserErr != nil && !errors.Is(browserErr, context.Canceled) && !errors.Is(browserErr, context.DeadlineExceeded) {
				runtime.setAutomationStatus(sanitizeAutomationStatus(browserErr.Error()))
			}
			browserResult <- browserErr
		}()
		defer func() {
			cancelAttempt()
			<-browserDone
		}()
		providerAuthError = func() bool {
			select {
			case browserErr := <-browserResult:
				return browserErr != nil && strings.TrimSpace(browserErr.Error()) == "provider_auth_error"
			default:
				return false
			}
		}
	}
	if err := setRepairAwaiting(runtime.jobID, ordinal, started.URL); err != nil {
		return account{}, "state_unavailable"
	}
	deadline := time.Now().Add(5 * time.Minute)
	for {
		if providerAuthError() {
			return account{}, "provider_auth_error"
		}
		if time.Now().After(deadline) {
			return account{}, "oauth_timeout"
		}
		status, pollErr := repairPollNativeOAuth(attemptCtx, runtime.authHeader, started.State)
		if pollErr != nil {
			if reason := reviveSkipReason(runtime); reason != "" {
				return account{}, "skipped"
			}
			return account{}, "oauth_poll_failed"
		}
		if providerAuthError() {
			return account{}, "provider_auth_error"
		}
		if reason := reviveSkipReason(runtime); reason != "" {
			return account{}, "skipped"
		}
		switch status {
		case "wait":
			updateJob(runtime.jobID, "awaiting_user", "", repairedRowCount(runtime.queued))
			if accounts, listErr := repairListAccounts(); listErr == nil {
				if replacement, ok := resolveRepairAccount(accounts, expected); ok {
					replacement = repairRefreshQuota(replacement)
					if definitiveHealthy(replacement) {
						return replacement, "repaired"
					}
				}
			}
			select {
			case <-attemptCtx.Done():
				if reason := reviveSkipReason(runtime); reason != "" {
					return account{}, "skipped"
				}
				return account{}, "cancelled"
			case <-time.After(2 * time.Second):
			}
		case "success":
			replacement, validationErr := validatedReplacementForMode(attemptCtx, before, expected, agentMode)
			if validationErr != nil {
				if reason := reviveSkipReason(runtime); reason != "" {
					return account{}, "skipped"
				}
				if strings.Contains(validationErr.Error(), "replacement_mismatch") || strings.Contains(validationErr.Error(), "replacement_unvalidated") {
					return account{}, "retry_workspace"
				}
				return account{}, validationErr.Error()
			}
			oauthSucceeded = true
			return replacement, "repaired"
		default:
			return account{}, "oauth_failed"
		}
	}
}

// validatedReplacementForMode is shared by both repair paths. The browser
// operator determines only which exact candidate transition is expected;
// credential refresh, quota validation, timeout, and cancellation are common.
func validatedReplacementForMode(ctx context.Context, before []account, expected account, agentMode bool) (account, error) {
	deadline := time.Now().Add(repairReplacementWaitLimit)
	lastErr := errors.New("replacement_mismatch")
	for {
		after, err := repairListAccounts()
		if err != nil {
			lastErr = errors.New("replacement_unavailable")
		} else {
			var match account
			var found bool
			if agentMode {
				match, found = inPlaceReplacementCandidate(after, expected)
			} else {
				match, found = replacementMatches(before, after, expected)
				if !found {
					match, found = changedSameEmailReplacement(before, after, expected)
				}
			}
			if found {
				validated, validationErr := validateReplacementCredential(match)
				if validationErr == nil {
					return validated, nil
				}
				lastErr = validationErr
			}
		}
		if !time.Now().Before(deadline) {
			return account{}, lastErr
		}
		timer := time.NewTimer(repairReplacementPollDelay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return account{}, errors.New("cancelled")
		case <-timer.C:
		}
	}
}

// inPlaceReplacementCandidate hydrates the private identity before comparing
// it because host.auth.list may omit account_id.
func inPlaceReplacementCandidate(after []account, expected account) (account, bool) {
	var match account
	found := 0
	for _, candidate := range after {
		if !candidate.Physical || !exactRepairEmail(candidate.Email, expected.Email) {
			continue
		}
		if candidate.AccountID == "" {
			candidate = repairCaptureIdentity(candidate)
		}
		if strings.TrimSpace(candidate.AccountID) != strings.TrimSpace(expected.AccountID) {
			continue
		}
		match, found = candidate, found+1
	}
	return match, found == 1
}

func validateReplacementCredential(candidate account) (account, error) {
	validated := refreshCredential(candidate)
	if !credentialAvailable(validated) {
		return account{}, errors.New("replacement_unvalidated")
	}
	validated = repairRefreshQuota(validated)
	if !quotaReadValidated(validated.Quota) {
		return account{}, errors.New("replacement_unvalidated")
	}
	return validated, nil
}

func queuedReplacementOrdinal(queued []account, replacement account) int {
	accountID := strings.TrimSpace(replacement.AccountID)
	if accountID == "" {
		return 0
	}
	match := 0
	for i, candidate := range queued {
		if strings.TrimSpace(candidate.AccountID) != accountID || !exactRepairEmail(candidate.Email, replacement.Email) {
			continue
		}
		if match != 0 {
			return 0
		}
		match = i + 1
	}
	return match
}

func queuedEmailCount(queued []account, email string) int {
	email = strings.TrimSpace(email)
	if email == "" {
		return 0
	}
	count := 0
	for _, candidate := range queued {
		if strings.EqualFold(strings.TrimSpace(candidate.Email), email) {
			count++
		}
	}
	return count
}

func waitForOldRecordGone(ctx context.Context, expected account) error {
	deadline := time.Now().Add(5 * time.Second)
	for {
		accounts, err := repairListAccounts()
		if err != nil {
			return errors.New("inventory_unavailable")
		}
		visible := false
		for _, a := range accounts {
			if a.Key == expected.Key || (expected.AuthPath != "" && a.AuthPath == expected.AuthPath) {
				visible = true
				break
			}
		}
		if !visible {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("quarantine_not_confirmed")
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return errors.New("cancelled")
		case <-timer.C:
		}
	}
}

func validatedReplacement(ctx context.Context, before []account, expected account) (account, error) {
	return validatedReplacementForMode(ctx, before, expected, false)
}

func changedSameEmailReplacement(before, after []account, expected account) (account, bool) {
	beforeByKey := make(map[string]account, len(before))
	for _, candidate := range before {
		beforeByKey[candidate.Key] = candidate
	}
	var match account
	found := 0
	for _, candidate := range after {
		if !candidate.Physical || !exactRepairEmail(candidate.Email, expected.Email) {
			continue
		}
		if candidate.AccountID == "" {
			candidate = captureRepairIdentity(candidate)
		}
		if strings.TrimSpace(candidate.AccountID) == "" {
			continue
		}
		if prior, existed := beforeByKey[candidate.Key]; existed && !replacementMarkerChanged(prior, candidate) {
			continue
		}
		match, found = candidate, found+1
	}
	return match, found == 1
}

func quotaReadValidated(quota quotaSnapshot) bool {
	reported := 0
	for _, window := range quotaWindows(quota) {
		switch parsePresence(window.Presence) {
		case windowPresent:
			reported++
		case windowAbsent:
		default:
			return false
		}
	}
	return reported > 0
}

func updateRepairRow(jobID string, ordinal int, state, reason string, quarantine ...string) error {
	db, err := openStore()
	if err != nil {
		return err
	}
	if len(quarantine) > 0 {
		q := filepath.Base(quarantine[0])
		_, err = db.Exec(`UPDATE repair_rows SET state=?,reason=?,quarantine=? WHERE job_id=? AND ordinal=?`, state, reason, q, jobID, ordinal)
		return err
	}
	_, err = db.Exec(`UPDATE repair_rows SET state=?,reason=? WHERE job_id=? AND ordinal=?`, state, reason, jobID, ordinal)
	return err
}

func updateRepairWorkspaceName(jobID string, ordinal int, name string) error {
	name = sanitizeWorkspaceName(name)
	if name == "" {
		return nil
	}
	db, err := openStore()
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE repair_rows SET workspace_name=? WHERE job_id=? AND ordinal=?`, name, jobID, ordinal)
	return err
}

func setRepairAwaiting(jobID string, ordinal int, urlValue string) error {
	// URL is intentionally not persisted. The in-memory runtime is the only
	// place where the current OAuth tab's URL may be observed by the page.
	return updateRepairRow(jobID, ordinal, "awaiting_user", "")
}

func revivePoll(id string) managementResponse {
	snapshot, err := readJob(id)
	if err != nil {
		return jsonResponse(http.StatusNotFound, map[string]string{"error": "not_found"})
	}
	result := map[string]any{"job_id": snapshot.ID, "state": snapshot.State, "done": snapshot.Done, "total": snapshot.Total, "automatic": true, "summary": repairJobSummary(snapshot.ID)}
	if snapshot.Reason != "" {
		result["reason"] = snapshot.Reason
	}
	reviveRuntimeState.Lock()
	runtime := reviveRuntimeState.jobs[id]
	if runtime != nil {
		runtime.mu.Lock()
		if runtime.current >= 0 && runtime.current < len(runtime.queued) {
			current := runtime.queued[runtime.current]
			result["current_index"] = runtime.current + 1
			result["current_email"] = current.Email
			result["current_seat"] = stableSeatLabel(current)
			if name := strings.TrimSpace(current.WorkspaceName); name != "" {
				result["workspace_name"] = name
			}
			result["current_state"] = current.RepairState
		}
		if runtime.agentBrowserMode() {
			result["automatic"] = false
		}
		if runtime.automationStatus != "" {
			result["automation_status"] = runtime.automationStatus
		}
		if runtime.oauthURL != "" && (snapshot.State == "running" || snapshot.State == "awaiting_user") {
			result["oauth_url"] = runtime.oauthURL
			result["awaiting_user"] = snapshot.State == "awaiting_user"
			if attempt := formatReviveAttempt(runtime.verificationRequestedAt); attempt != "" {
				result["attempt"] = attempt
			}
			if runtime.current >= 0 && runtime.current < len(runtime.queued) {
				current := runtime.queued[runtime.current]
				result["email"] = current.Email
				result["seat"] = stableSeatLabel(current)
				if name := strings.TrimSpace(runtime.selectedWorkspaceName); name != "" {
					result["workspace_name"] = name
				}
			}
		}
		runtime.mu.Unlock()
	}
	reviveRuntimeState.Unlock()
	return jsonResponse(http.StatusOK, result)
}

func formatReviveAttempt(requestedAt time.Time) string {
	if requestedAt.IsZero() {
		return ""
	}
	return requestedAt.UTC().Format(time.RFC3339Nano)
}

func (runtime *reviveRuntime) setAutomationStatus(status string) {
	if strings.TrimSpace(status) == "" {
		return
	}
	status = sanitizeAutomationStatus(status)
	runtime.mu.Lock()
	runtime.automationStatus = status
	runtime.mu.Unlock()
}

func (runtime *reviveRuntime) setWorkspaceName(name string) {
	name = sanitizeWorkspaceName(name)
	if runtime == nil || name == "" {
		return
	}
	runtime.mu.Lock()
	runtime.selectedWorkspaceName = name
	runtime.mu.Unlock()
}

func (runtime *reviveRuntime) workspaceName() string {
	if runtime == nil {
		return ""
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.selectedWorkspaceName
}

func reviveSkipReason(runtime *reviveRuntime) string {
	if runtime == nil {
		return ""
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if !runtime.skipRequested {
		return ""
	}
	return runtime.skipReason
}

func (runtime *reviveRuntime) cancelNativeOAuthOnce(authHeader, state string) {
	if runtime == nil || strings.TrimSpace(state) == "" {
		return
	}
	runtime.nativeOAuthCancelOnce.Do(func() {
		_ = cancelNativeOAuth(context.Background(), authHeader, state)
	})
}

func sanitizeAutomationStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "browser_starting", "browser_ready", "browser_unavailable", "browser_navigation_failed", "browser_automation_failed", "oauth_url_invalid",
		"login_opened", "email_submitted", "email_code_requested", "agent_autofill_unavailable", "verification_code_waiting", "verification_code_submitted", "verification_code_resent",
		"workspace_selected", "manual_workspace_selection_required", "manual_password_required", "manual_captcha_required",
		"manual_login_required", "manual_recipient_mismatch", "provider_auth_error", "callback_reached", "login_window_closed":
		return strings.TrimSpace(status)
	default:
		return "browser_automation_failed"
	}
}

func sanitizeRepairCodeReason(err error) string {
	if err == nil {
		return "verification_code_not_found"
	}
	switch strings.TrimSpace(err.Error()) {
	case "verification_code_not_found", "verification_code_ambiguous", "agent_autofill_unavailable", "agent_autofill_not_configured", "verification_code_not_configured":
		return strings.TrimSpace(err.Error())
	default:
		return "verification_code_unavailable"
	}
}

// reviveCode performs a read-only lookup for the currently active agent row.
// Probe results live only in the runtime for this attempt: they are never
// persisted or logged. This prevents dashboard polling from repeatedly
// consuming the same Agent Autofill message.
func reviveCode(id string, expectedAttempt ...string) managementResponse {
	id = strings.TrimSpace(id)
	if id == "" {
		return jsonResponse(http.StatusBadRequest, map[string]string{"error": "id_required"})
	}
	attemptInput := ""
	if len(expectedAttempt) > 0 {
		attemptInput = strings.TrimSpace(expectedAttempt[0])
	}
	if attemptInput == "" {
		return jsonResponse(http.StatusBadRequest, map[string]string{"error": "attempt_required"})
	}
	snapshot, err := readJob(id)
	if err != nil || snapshot.Kind != "revive" {
		return jsonResponse(http.StatusNotFound, map[string]string{"error": "not_found"})
	}
	if snapshot.State != "running" && snapshot.State != "awaiting_user" {
		return jsonResponse(http.StatusConflict, map[string]string{"error": "no_active_row"})
	}
	reviveRuntimeState.Lock()
	runtime := reviveRuntimeState.jobs[id]
	if runtime == nil {
		reviveRuntimeState.Unlock()
		return jsonResponse(http.StatusConflict, map[string]string{"error": "no_active_row"})
	}
	runtime.mu.Lock()
	if !runtime.agentBrowserMode() {
		runtime.mu.Unlock()
		reviveRuntimeState.Unlock()
		return jsonResponse(http.StatusConflict, map[string]string{"error": "agent_mode_required"})
	}
	ordinal := runtime.current
	requestedAt := runtime.verificationRequestedAt
	if ordinal < 0 || ordinal >= len(runtime.queued) || requestedAt.IsZero() {
		runtime.mu.Unlock()
		reviveRuntimeState.Unlock()
		return jsonResponse(http.StatusConflict, map[string]string{"error": "no_active_row"})
	}
	recipient := strings.TrimSpace(runtime.queued[ordinal].Email)
	attempt := formatReviveAttempt(requestedAt)
	if attemptInput != attempt {
		runtime.mu.Unlock()
		reviveRuntimeState.Unlock()
		return jsonResponse(http.StatusConflict, map[string]any{"error": "stale_attempt", "job_id": id, "attempt": attempt})
	}
	if runtime.queued[ordinal].RepairState == "repaired" || runtime.queued[ordinal].RepairState == "skipped" {
		runtime.mu.Unlock()
		reviveRuntimeState.Unlock()
		return jsonResponse(http.StatusConflict, map[string]string{"error": "no_active_row"})
	}
	now := time.Now()
	probe := &runtime.codeProbe
	if probe.attempt != attempt || !strings.EqualFold(probe.recipient, recipient) {
		*probe = reviveCodeProbe{attempt: attempt, recipient: recipient}
	}
	if probe.positive != nil {
		cached := *probe.positive
		runtime.mu.Unlock()
		reviveRuntimeState.Unlock()
		return jsonResponse(http.StatusOK, map[string]any{"job_id": id, "attempt": attempt, "detected": true, "code": cached.Code, "received_at": cached.ReceivedAt.UTC().Format(time.RFC3339Nano)})
	}
	if probe.inFlight {
		runtime.mu.Unlock()
		reviveRuntimeState.Unlock()
		return jsonResponse(http.StatusOK, map[string]any{"job_id": id, "attempt": attempt, "detected": false, "reason": "verification_code_checking"})
	}
	if probe.negative != "" && now.Sub(probe.checkedAt) < reviveCodeNegativeTTL {
		reason := probe.negative
		runtime.mu.Unlock()
		reviveRuntimeState.Unlock()
		return jsonResponse(http.StatusOK, map[string]any{"job_id": id, "attempt": attempt, "detected": false, "reason": reason})
	}
	probe.inFlight = true
	runtime.mu.Unlock()
	reviveRuntimeState.Unlock()

	code, detectErr := repairDetectCode(recipient, requestedAt)
	valid := detectErr == nil
	reason := ""
	if detectErr != nil {
		reason = sanitizeRepairCodeReason(detectErr)
	} else if !sixDigitCode.MatchString(strings.TrimSpace(code.Code)) || code.ReceivedAt.IsZero() || code.ReceivedAt.Before(requestedAt.Add(-2*time.Second)) || code.ReceivedAt.After(time.Now().Add(30*time.Second)) || !strings.EqualFold(strings.TrimSpace(code.Recipient), recipient) {
		valid = false
		reason = "invalid_verification_code"
	}

	// The detector can block while the queue advances. Revalidate every piece of
	// attempt identity before publishing or caching its result.
	reviveRuntimeState.Lock()
	currentRuntime := reviveRuntimeState.jobs[id]
	if currentRuntime != runtime {
		reviveRuntimeState.Unlock()
		return jsonResponse(http.StatusConflict, map[string]string{"error": "stale_attempt"})
	}
	runtime.mu.Lock()
	currentOrdinal := runtime.current
	currentAttempt := formatReviveAttempt(runtime.verificationRequestedAt)
	currentRecipient := ""
	if currentOrdinal >= 0 && currentOrdinal < len(runtime.queued) {
		currentRecipient = strings.TrimSpace(runtime.queued[currentOrdinal].Email)
	}
	if currentOrdinal != ordinal || currentAttempt != attempt || !strings.EqualFold(currentRecipient, recipient) || !runtime.agentBrowserMode() || currentOrdinal < 0 || currentOrdinal >= len(runtime.queued) || runtime.queued[currentOrdinal].RepairState == "repaired" || runtime.queued[currentOrdinal].RepairState == "skipped" {
		runtime.mu.Unlock()
		reviveRuntimeState.Unlock()
		return jsonResponse(http.StatusConflict, map[string]string{"error": "stale_attempt"})
	}
	probe = &runtime.codeProbe
	probe.inFlight = false
	probe.checkedAt = time.Now()
	if valid {
		cached := code
		probe.positive = &cached
		probe.negative = ""
	} else {
		probe.positive = nil
		probe.negative = reason
	}
	runtime.mu.Unlock()
	reviveRuntimeState.Unlock()
	if !valid {
		return jsonResponse(http.StatusOK, map[string]any{"job_id": id, "attempt": attempt, "detected": false, "reason": reason})
	}
	return jsonResponse(http.StatusOK, map[string]any{"job_id": id, "attempt": attempt, "detected": true, "code": code.Code, "received_at": code.ReceivedAt.UTC().Format(time.RFC3339Nano)})
}

const (
	reviveSkipProviderAuthReason     = "provider_auth_error"
	reviveSkipWorkspaceMissingReason = "workspace_missing"
)

// reviveSkip marks only the currently published agent OAuth attempt as
// unresolved. It deliberately does not call the job-wide cancel function: the
// queue worker owns advancement to the next ordinal after this attempt's
// context is cancelled.
func reviveSkip(jobID, attempt, reason string) managementResponse {
	jobID = strings.TrimSpace(jobID)
	attempt = strings.TrimSpace(attempt)
	reason = strings.TrimSpace(reason)
	if jobID == "" {
		return jsonResponse(http.StatusBadRequest, map[string]string{"error": "job_id_required"})
	}
	if attempt == "" {
		return jsonResponse(http.StatusBadRequest, map[string]string{"error": "attempt_required"})
	}
	if reason == "" {
		return jsonResponse(http.StatusBadRequest, map[string]string{"error": "reason_required"})
	}
	if reason != reviveSkipProviderAuthReason && reason != reviveSkipWorkspaceMissingReason {
		return jsonResponse(http.StatusBadRequest, map[string]string{"error": "invalid_reason"})
	}
	snapshot, err := readJob(jobID)
	if err != nil || snapshot.Kind != "revive" {
		return jsonResponse(http.StatusNotFound, map[string]string{"error": "not_found"})
	}
	if snapshot.State != "running" && snapshot.State != "awaiting_user" {
		return jsonResponse(http.StatusConflict, map[string]string{"error": "no_active_row"})
	}

	reviveRuntimeState.Lock()
	runtime := reviveRuntimeState.jobs[jobID]
	reviveRuntimeState.Unlock()
	if runtime == nil || strings.TrimSpace(runtime.jobID) != jobID {
		return jsonResponse(http.StatusConflict, map[string]string{"error": "no_active_row"})
	}
	runtime.mu.Lock()
	if !runtime.agentBrowserMode() {
		runtime.mu.Unlock()
		return jsonResponse(http.StatusConflict, map[string]string{"error": "agent_mode_required"})
	}
	ordinal := runtime.current
	requestedAt := runtime.verificationRequestedAt
	currentAttempt := formatReviveAttempt(requestedAt)
	if ordinal < 0 || ordinal >= len(runtime.queued) || requestedAt.IsZero() || runtime.oauthState == "" || runtime.skipRequested || runtime.queued[ordinal].RepairState == "repaired" || runtime.queued[ordinal].RepairState == "skipped" {
		runtime.mu.Unlock()
		return jsonResponse(http.StatusConflict, map[string]string{"error": "no_active_row"})
	}
	if attempt != currentAttempt {
		runtime.mu.Unlock()
		// Do not return the live attempt token to stale callers. The token is
		// only useful to the tab that already has it, and a caller can poll to
		// obtain a fresh one when it still owns the job.
		return jsonResponse(http.StatusConflict, map[string]string{"error": "stale_attempt"})
	}
	// Keep this durable row transition under the runtime lock so a concurrent
	// successful callback cannot overwrite the skip with repaired state after
	// this endpoint has validated the same attempt.
	if err := updateRepairRow(jobID, ordinal+1, "skipped", reason); err != nil {
		runtime.mu.Unlock()
		return jsonResponse(http.StatusServiceUnavailable, map[string]string{"error": "state_unavailable"})
	}
	runtime.skipRequested = true
	runtime.skipReason = reason
	runtime.queued[ordinal].RepairState = "skipped"
	state := runtime.oauthState
	authHeader := runtime.authHeader
	attemptCancel := runtime.attemptCancel
	runtime.mu.Unlock()

	// Cancel exactly the captured native OAuth session, then release the
	// per-attempt context. The queue's job context remains live for later rows.
	runtime.cancelNativeOAuthOnce(authHeader, state)
	if attemptCancel != nil {
		attemptCancel()
	}
	return jsonResponse(http.StatusOK, map[string]string{"job_id": jobID, "state": "skipped", "reason": reason})
}

func cancelRevive(id string, headers map[string][]string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	snapshot, err := readJob(id)
	if err != nil || snapshot.Kind != "revive" {
		return false
	}
	switch snapshot.State {
	case "running", "awaiting_user", "failed", "cancelled":
	default:
		return false
	}
	reviveRuntimeState.Lock()
	runtime := reviveRuntimeState.jobs[id]
	if runtime != nil {
		runtime.mu.Lock()
		state := runtime.oauthState
		cancel := runtime.cancel
		runtime.mu.Unlock()
		if state != "" {
			_ = cancelNativeOAuth(context.Background(), managementAuthorization(headers), state)
		}
		if cancel != nil {
			cancel()
		}
	}
	reviveRuntimeState.Unlock()
	// A restart may leave a recoverable Revive row without an in-memory
	// runtime. Mark only that exact durable job cancelled; never touch another
	// active job (especially Ignite).
	if runtime == nil {
		if db, dbErr := openStore(); dbErr == nil {
			_, _ = db.Exec(`UPDATE jobs SET state='cancelled',reason='cancelled',updated_at=? WHERE id=? AND kind='revive' AND state IN ('running','awaiting_user','failed','cancelled')`, time.Now().Unix(), id)
		}
	}
	return true
}

func definitiveHealthy(a account) bool {
	// A direct successful quota response is newer, authoritative evidence than
	// a stale host runtime auth marker. The caller only sets QuotaStatusCode from
	// the bounded provider probe, so a 2xx result may clear a queued auth signal
	// and advance without quarantine/OAuth.
	return a.Physical && a.QuotaStatusCode >= http.StatusOK && a.QuotaStatusCode < http.StatusMultipleChoices
}

func userStateRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "", errors.New("home_unavailable")
	}
	return filepath.Join(home, ".cli-proxy-api-state"), nil
}
