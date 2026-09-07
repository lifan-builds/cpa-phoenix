package main

import (
	"context"
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

var errInvalidBrowserMode = errors.New("invalid_browser_mode")

const (
	reviveBrowserModeAutomatic = "automatic"
	reviveBrowserModeAgent     = "agent"
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
	repairDetectThunderbirdCode = detectThunderbirdCode
	repairReplacementWaitLimit  = 5 * time.Second
	repairReplacementPollDelay  = 100 * time.Millisecond
	repairNewLoginBrowser       = func(ctx context.Context) (loginBrowserController, error) { return newLoginBrowser(ctx) }
)

// loginBrowserController is owned for the lifetime of one Revive queue. Each
// OAuth row gets a cancellable Run call, while CPA's native poll remains the
// authority for whether login and replacement validation succeeded.
type loginBrowserController interface {
	Close() error
	Run(context.Context, string, string, string, time.Time, func(string)) error
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
	ln, err := net.Listen("tcp4", "127.0.0.1:1455")
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
	// preserve every retained segment byte-for-byte and append the two Butler
	// prefill fields at the end.
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
			if _, valueErr := url.QueryUnescape(segment); valueErr != nil {
				return "", errors.New("oauth_url_invalid")
			}
			if key == "login_hint" || key == "prompt" {
				continue
			}
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
	automationStatus string
	// verificationRequestedAt is runtime-only and gates Thunderbird matches so
	// a code from an earlier OAuth attempt can never be reused.
	verificationRequestedAt time.Time
	queued                  []account
	started                 bool
	cancel                  context.CancelFunc
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

func beginRevive(headers map[string][]string, requestedMode ...string) (reviveResponse, error) {
	mode := reviveBrowserModeAutomatic
	if len(requestedMode) > 0 {
		mode = requestedMode[0]
	}
	mode, err := normalizeReviveBrowserMode(mode)
	if err != nil {
		return reviveResponse{}, err
	}
	if resumed, ok := resumeRevive(headers, mode); ok {
		return resumed, nil
	}
	accounts, err := repairListAccounts()
	if err != nil {
		return reviveResponse{}, err
	}
	rows := make([]account, 0, len(accounts))
	for i, a := range accounts {
		if !a.Physical {
			continue
		}
		if (a.Disabled || a.Expired || a.Unavailable) && !invalidAuthDecision(a) {
			continue
		}
		// Use the same quota-auth probe as scan before snapshotting rows so an
		// exact provider 401 cannot be missed while status remains active.
		if !a.Disabled && !a.Expired && !a.Unavailable {
			a = refreshQuota(a)
		}
		accounts[i] = a
		if invalidAuthDecision(a) {
			rows = append(rows, a)
		}
	}
	// host.auth.list intentionally omits the private ChatGPT account ID. Read
	// the exact credential wrapper once while the old file is still present so
	// same-email seats remain distinguishable after restart. Never retain the
	// credential itself in the queue or persisted repair row. Missing private
	// identity is a sanitized skip: replacement validation can never prove the
	// correct team without it.
	rows = captureRepairRows(rows)
	if len(rows) == 0 {
		return reviveResponse{}, errNoActionableAccounts
	}
	id, err := globalJobs.start("revive", len(rows))
	if err != nil {
		return reviveResponse{}, err
	}
	db, err := openStore()
	if err != nil {
		globalJobs.stop()
		return reviveResponse{}, err
	}
	for i, a := range rows {
		// Physical paths are needed only by the in-memory row that is about to
		// be quarantined. Never persist them: after restart, queued rows resolve
		// the current exact path from host.auth.list, while quarantined rows no
		// longer need their predecessor path.
		if _, err := db.Exec(`INSERT INTO repair_rows(job_id,ordinal,account_key,auth_id,auth_index,email,account_id,auth_path,auth_dir,state) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, i+1, a.Key, a.AuthID, a.AuthIndex, a.Email, a.AccountID, "", "", "queued"); err != nil {
			globalJobs.stop()
			return reviveResponse{}, err
		}
	}
	runtime := &reviveRuntime{jobID: id, authHeader: managementAuthorization(headers), browserMode: mode, state: "queued", queued: rows}
	reviveRuntimeState.Lock()
	reviveRuntimeState.jobs[id] = runtime
	reviveRuntimeState.Unlock()
	// Start the first row only after the callback port is owned. Any occupied
	// port therefore fails before quarantine or OAuth mutation.
	go runReviveQueue(runtime)
	return reviveResponse{JobID: id, State: "running", Total: len(rows)}, nil
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
	db, err := openStore()
	if err != nil {
		return reviveResponse{}, false
	}
	var id, state string
	var total, done int
	err = db.QueryRow(`SELECT id,state,total,done FROM jobs WHERE kind='revive' ORDER BY updated_at DESC LIMIT 1`).Scan(&id, &state, &total, &done)
	if err != nil {
		return reviveResponse{}, false
	}
	rows, err := loadRepairRows(id)
	if err != nil || len(rows) == 0 {
		return reviveResponse{}, false
	}
	incomplete := false
	for _, row := range rows {
		if row.RepairState != "repaired" {
			incomplete = true
			break
		}
	}
	if !incomplete {
		return reviveResponse{}, false
	}
	if state != "failed" && state != "cancelled" && state != "running" && state != "awaiting_user" {
		state = "failed"
		_, _ = db.Exec(`UPDATE jobs SET state='failed',reason='interrupted',updated_at=? WHERE id=?`, time.Now().Unix(), id)
	}
	if err := globalJobs.claim(id); err != nil {
		return reviveResponse{}, false
	}
	// A crash can occur after a row is durably marked repaired but before the
	// aggregate job counter advances. Resume from the first non-repaired row so
	// a completed OAuth replacement is never quarantined or logged in again.
	startAt := 0
	for startAt < len(rows) && rows[startAt].RepairState == "repaired" {
		startAt++
	}
	runtime := &reviveRuntime{jobID: id, authHeader: managementAuthorization(headers), browserMode: mode, state: state, queued: rows, startAt: startAt}
	reviveRuntimeState.Lock()
	reviveRuntimeState.jobs[id] = runtime
	reviveRuntimeState.Unlock()
	go runReviveQueue(runtime)
	return reviveResponse{JobID: id, State: "running", Total: total, AwaitingUser: state == "awaiting_user"}, true
}

func loadRepairRows(jobID string) ([]account, error) {
	db, err := openStore()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT account_key,auth_id,auth_index,email,account_id,auth_path,auth_dir,state,quarantine FROM repair_rows WHERE job_id=? ORDER BY ordinal`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var accounts []account
	for rows.Next() {
		var a account
		if err := rows.Scan(&a.Key, &a.AuthID, &a.AuthIndex, &a.Email, &a.AccountID, &a.AuthPath, &a.AuthDir, &a.RepairState, &a.Quarantine); err != nil {
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
	for ordinal := runtime.startAt; ordinal < len(runtime.queued); {
		if runtime.queued[ordinal].RepairState == "repaired" {
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
		if state == "repaired_other" {
			updateJob(runtime.jobID, "running", "", repairedRowCount(runtime.queued))
			continue
		}
		if state != "repaired" {
			// An explicit cancellation owns the stopped row's recovery state:
			// leave its queued/quarantined marker intact so a later explicit
			// resume can make the same safe decision. Every other terminal
			// failure must be durably visible on the row before the job stops.
			safeState := "failed"
			if state != "cancelled" {
				_ = updateRepairRow(runtime.jobID, ordinal+1, "failed", state)
			} else {
				safeState = "cancelled"
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
	updateJob(runtime.jobID, "completed", "", len(runtime.queued))
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
	// Queue startup owns the browser before any predecessor can be quarantined.
	// Keep this guard here as well so direct callers cannot mutate an auth file
	// without a usable automatic controller. Agent mode deliberately leaves
	// navigation to the caller and therefore has no browser controller.
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
	if !alreadyQuarantined {
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
	runtime.mu.Lock()
	requestedAt := time.Now()
	runtime.verificationRequestedAt = requestedAt
	runtime.mu.Unlock()
	selectAccount := queuedEmailCount(runtime.queued, current.Email) > 1
	var started nativeOAuthStart
	if selectAccount {
		started, err = repairStartNativeOAuthMode(ctx, runtime.authHeader, current.Email, true)
	} else {
		started, err = repairStartNativeOAuth(ctx, runtime.authHeader, current.Email)
	}
	if err != nil {
		return "oauth_start_failed"
	}
	oauthSucceeded := false
	defer func() {
		if !oauthSucceeded {
			_ = cancelNativeOAuth(context.Background(), runtime.authHeader, started.State)
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
	if !agentMode {
		attemptCtx, cancelAttempt := context.WithCancel(ctx)
		browserDone := make(chan struct{})
		go func() {
			defer close(browserDone)
			err := browser.Run(attemptCtx, started.URL, current.Email, expected.AccountID, requestedAt, runtime.setAutomationStatus)
			if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				runtime.setAutomationStatus(sanitizeAutomationStatus(err.Error()))
			}
		}()
		defer func() {
			cancelAttempt()
			<-browserDone
		}()
	}
	// The page receives the URL through the transient status response. The raw
	// URL/state never enter SQLite or logs.
	if err := setRepairAwaiting(runtime.jobID, ordinal, started.URL); err != nil {
		return "state_unavailable"
	}
	deadline := time.Now().Add(5 * time.Minute)
	for {
		if time.Now().After(deadline) {
			return "oauth_timeout"
		}
		status, err := repairPollNativeOAuth(ctx, runtime.authHeader, started.State)
		if err != nil {
			return "oauth_poll_failed"
		}
		switch status {
		case "wait":
			updateJob(runtime.jobID, "awaiting_user", "", repairedRowCount(runtime.queued))
			// The user (or another process) may repair this exact seat while the
			// OAuth page is open. Re-read the inventory and quota before waiting
			// again; a definitive 2xx means the row is complete and no second
			// OAuth attempt is needed.
			if accounts, listErr := repairListAccounts(); listErr == nil {
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
			select {
			case <-ctx.Done():
				return "cancelled"
			case <-time.After(2 * time.Second):
			}
		case "success":
			replacement, err := validatedReplacement(ctx, before, expected)
			if err != nil {
				return err.Error()
			}
			target := queuedReplacementOrdinal(runtime.queued, replacement)
			if target == 0 {
				return "replacement_not_queued"
			}
			if err := updateRepairRow(runtime.jobID, target, "repaired", ""); err != nil {
				return "state_unavailable"
			}
			runtime.mu.Lock()
			if target > 0 && target <= len(runtime.queued) {
				runtime.queued[target-1].RepairState = "repaired"
			}
			runtime.oauthURL = ""
			runtime.oauthState = ""
			if runtime.forwarder != nil {
				runtime.forwarder.setState("")
			}
			runtime.mu.Unlock()
			oauthSucceeded = true
			if target != ordinal {
				return "repaired_other"
			}
			return "repaired"
		default:
			return "oauth_failed"
		}
	}
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
	deadline := time.Now().Add(repairReplacementWaitLimit)
	lastErr := errors.New("replacement_mismatch")
	for {
		after, err := repairListAccounts()
		if err != nil {
			lastErr = errors.New("replacement_unavailable")
		} else {
			replacement, ok := replacementMatches(before, after, expected)
			if !ok {
				replacement, ok = changedSameEmailReplacement(before, after, expected)
			}
			if ok {
				replacement = refreshCredential(replacement)
				if credentialAvailable(replacement) {
					validated := repairRefreshQuota(replacement)
					if quotaReadValidated(validated.Quota) {
						return replacement, nil
					}
					return account{}, errors.New("replacement_unvalidated")
				}
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
	result := map[string]any{"job_id": snapshot.ID, "state": snapshot.State, "done": snapshot.Done, "total": snapshot.Total, "automatic": true}
	if snapshot.Reason != "" {
		result["reason"] = snapshot.Reason
	}
	reviveRuntimeState.Lock()
	runtime := reviveRuntimeState.jobs[id]
	if runtime != nil {
		runtime.mu.Lock()
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

func sanitizeAutomationStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "browser_starting", "browser_ready", "browser_unavailable", "browser_navigation_failed", "browser_automation_failed", "oauth_url_invalid",
		"login_opened", "email_submitted", "email_code_requested", "verification_code_waiting", "verification_code_submitted", "verification_code_resent",
		"workspace_selected", "manual_workspace_selection_required", "manual_password_required", "manual_captcha_required",
		"manual_login_required", "manual_recipient_mismatch", "callback_reached", "login_window_closed":
		return strings.TrimSpace(status)
	default:
		return "browser_automation_failed"
	}
}

// reviveCode performs a best-effort, read-only lookup for the currently active
// row. The value is returned only in this response and is never persisted or
// logged by Phoenix.
func reviveCode(id string, expectedAttempt ...string) managementResponse {
	id = strings.TrimSpace(id)
	if id == "" {
		return jsonResponse(http.StatusBadRequest, map[string]string{"error": "id_required"})
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
	ordinal := runtime.current
	requestedAt := runtime.verificationRequestedAt
	if ordinal < 0 || ordinal >= len(runtime.queued) || requestedAt.IsZero() {
		runtime.mu.Unlock()
		reviveRuntimeState.Unlock()
		return jsonResponse(http.StatusConflict, map[string]string{"error": "no_active_row"})
	}
	recipient := runtime.queued[ordinal].Email
	attempt := formatReviveAttempt(requestedAt)
	runtime.mu.Unlock()
	reviveRuntimeState.Unlock()
	if len(expectedAttempt) > 0 && strings.TrimSpace(expectedAttempt[0]) != "" && expectedAttempt[0] != attempt {
		return jsonResponse(http.StatusConflict, map[string]any{"error": "stale_attempt", "job_id": id, "attempt": attempt})
	}
	code, err := repairDetectThunderbirdCode(recipient, requestedAt)
	if err != nil {
		return jsonResponse(http.StatusOK, map[string]any{"job_id": id, "attempt": attempt, "detected": false, "reason": strings.TrimSpace(err.Error())})
	}
	return jsonResponse(http.StatusOK, map[string]any{"job_id": id, "attempt": attempt, "detected": true, "code": code.Code, "received_at": code.ReceivedAt.UTC().Format(time.RFC3339)})
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
