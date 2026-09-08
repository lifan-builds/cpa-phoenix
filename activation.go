package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// The quota projection intentionally keeps the provider's explicit window
// presence. Missing or contradictory fields are unknown and never fresh.
type windowPresence string

const (
	windowUnknown windowPresence = "unknown"
	windowPresent windowPresence = "present"
	windowAbsent  windowPresence = "absent"
)

type quotaWindow struct {
	Presence           windowPresence `json:"presence"`
	UsedPercent        *float64       `json:"used_percent,omitempty"`
	ResetAt            *int64         `json:"reset_at,omitempty"`
	UsedTokens         *int64         `json:"used_tokens,omitempty"`
	RemainingTokens    *int64         `json:"remaining_tokens,omitempty"`
	LimitTokens        *int64         `json:"limit_tokens,omitempty"`
	LimitWindowSeconds *int64         `json:"limit_window_seconds,omitempty"`
	ResetAfterSeconds  *int64         `json:"reset_after_seconds,omitempty"`
}

type quotaSnapshot struct {
	Primary    quotaWindow   `json:"primary"`
	Secondary  quotaWindow   `json:"secondary"`
	Windows    []quotaWindow `json:"windows,omitempty"`
	ObservedAt int64         `json:"observed_at"`
}

func parseQuota(raw json.RawMessage) quotaSnapshot {
	var q quotaSnapshot
	if len(raw) == 0 || json.Unmarshal(raw, &q) != nil {
		return q
	}
	if len(q.Windows) > 0 {
		for i := range q.Windows {
			q.Windows[i].Presence = parsePresence(q.Windows[i].Presence)
		}
		return q
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return q
	}
	if nested, ok := fields["rate_limit"]; ok {
		var n map[string]json.RawMessage
		if json.Unmarshal(nested, &n) == nil {
			fields = n
		}
	}
	if value, ok := fields["primary"]; ok {
		q.Primary = decodeQuotaWindow(value)
	} else if value, ok := fields["primary_window"]; ok {
		q.Primary = decodeQuotaWindow(value)
	} else if value, ok := fields["primaryWindow"]; ok {
		q.Primary = decodeQuotaWindow(value)
	}
	if value, ok := fields["secondary"]; ok {
		q.Secondary = decodeQuotaWindow(value)
	} else if value, ok := fields["secondary_window"]; ok {
		q.Secondary = decodeQuotaWindow(value)
	} else if value, ok := fields["secondaryWindow"]; ok {
		q.Secondary = decodeQuotaWindow(value)
	}
	if _, ok := fields["primary"]; ok || q.Primary.Presence != windowUnknown {
		q.Windows = append(q.Windows, q.Primary)
	}
	if _, ok := fields["secondary"]; ok || q.Secondary.Presence != windowUnknown {
		q.Windows = append(q.Windows, q.Secondary)
	}
	return q
}

// parseCodexQuotaPayload adapts the native /wham/usage response. It accepts
// only the documented rate_limit/body aliases and marks omitted or malformed
// windows unknown.
func parseCodexQuotaPayload(raw []byte, observedAt int64) quotaSnapshot {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return quotaSnapshot{ObservedAt: observedAt, Windows: []quotaWindow{{Presence: windowUnknown}}}
	}
	root := mapValue(value)
	if nested := mapValue(root["body"]); len(nested) > 0 {
		root = nested
	}
	limit := mapValue(root["rate_limit"])
	if len(limit) == 0 {
		limit = mapValue(root["rateLimit"])
	}
	if len(limit) == 0 {
		limit = root
	}
	primary, primaryPresent := quotaWindowFromMap(limit, "primary_window", "primaryWindow")
	secondary, secondaryPresent := quotaWindowFromMap(limit, "secondary_window", "secondaryWindow")
	windows := make([]quotaWindow, 0, 2)
	if primaryPresent {
		windows = append(windows, primary)
	} else {
		windows = append(windows, quotaWindow{Presence: windowUnknown})
	}
	if secondaryPresent {
		windows = append(windows, secondary)
	} else {
		// A missing secondary is unknown, not an invented absent window.
		windows = append(windows, quotaWindow{Presence: windowUnknown})
	}
	return quotaSnapshot{Primary: primary, Secondary: secondary, Windows: windows, ObservedAt: observedAt}
}

func mapValue(value any) map[string]any {
	if result, ok := value.(map[string]any); ok {
		return result
	}
	return nil
}

func quotaWindowFromMap(limit map[string]any, keys ...string) (quotaWindow, bool) {
	var raw any
	found := false
	for _, key := range keys {
		if candidate, ok := limit[key]; ok {
			raw, found = candidate, true
			break
		}
	}
	if !found {
		return quotaWindow{Presence: windowUnknown}, false
	}
	if raw == nil {
		return quotaWindow{Presence: windowAbsent}, true
	}
	values := mapValue(raw)
	if len(values) == 0 {
		return quotaWindow{Presence: windowUnknown}, true
	}
	w := quotaWindow{Presence: windowPresent}
	var ok bool
	if w.UsedPercent, ok = floatValue(values, "used_percent", "usedPercent", "percent", "used"); !ok && hasAny(values, "used_percent", "usedPercent", "percent", "used") {
		return quotaWindow{Presence: windowUnknown}, true
	}
	if w.ResetAt, ok = intValue(values, "reset_at", "resetAt"); !ok && hasAny(values, "reset_at", "resetAt") {
		return quotaWindow{Presence: windowUnknown}, true
	}
	if w.UsedTokens, ok = intValue(values, "used_tokens", "usedTokens", "used_token_count", "usedTokenCount"); !ok && hasAny(values, "used_tokens", "usedTokens", "used_token_count", "usedTokenCount") {
		return quotaWindow{Presence: windowUnknown}, true
	}
	if w.RemainingTokens, ok = intValue(values, "remaining_tokens", "remainingTokens", "remaining", "available_tokens", "availableTokens"); !ok && hasAny(values, "remaining_tokens", "remainingTokens", "remaining", "available_tokens", "availableTokens") {
		return quotaWindow{Presence: windowUnknown}, true
	}
	if w.LimitTokens, ok = intValue(values, "limit_tokens", "limitTokens", "quota_tokens", "quotaTokens", "total_tokens", "totalTokens", "limit", "quota", "total"); !ok && hasAny(values, "limit_tokens", "limitTokens", "quota_tokens", "quotaTokens", "total_tokens", "totalTokens", "limit", "quota", "total") {
		return quotaWindow{Presence: windowUnknown}, true
	}
	if w.LimitWindowSeconds, ok = intValue(values, "limit_window_seconds", "limitWindowSeconds", "window_seconds", "windowSeconds"); !ok && hasAny(values, "limit_window_seconds", "limitWindowSeconds", "window_seconds", "windowSeconds") {
		return quotaWindow{Presence: windowUnknown}, true
	}
	if w.ResetAfterSeconds, ok = intValue(values, "reset_after_seconds", "resetAfterSeconds", "reset_in", "resetIn"); !ok && hasAny(values, "reset_after_seconds", "resetAfterSeconds", "reset_in", "resetIn") {
		return quotaWindow{Presence: windowUnknown}, true
	}
	return w, true
}

func hasAny(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := values[key]; ok {
			return true
		}
	}
	return false
}

func floatValue(values map[string]any, keys ...string) (*float64, bool) {
	for _, key := range keys {
		value, ok := values[key]
		if !ok {
			continue
		}
		number, ok := value.(float64)
		if !ok {
			return nil, false
		}
		return &number, true
	}
	return nil, true
}

func intValue(values map[string]any, keys ...string) (*int64, bool) {
	for _, key := range keys {
		value, ok := values[key]
		if !ok {
			continue
		}
		number, ok := value.(float64)
		if !ok || number != float64(int64(number)) {
			return nil, false
		}
		result := int64(number)
		return &result, true
	}
	return nil, true
}

func decodeQuotaWindow(raw json.RawMessage) quotaWindow {
	if strings.TrimSpace(string(raw)) == "null" {
		return quotaWindow{Presence: windowAbsent}
	}
	var w quotaWindow
	if json.Unmarshal(raw, &w) != nil {
		return quotaWindow{Presence: windowUnknown}
	}
	w.Presence = parsePresence(w.Presence)
	return w
}

func parsePresence(v windowPresence) windowPresence {
	switch strings.ToLower(strings.TrimSpace(string(v))) {
	case string(windowPresent):
		return windowPresent
	case string(windowAbsent):
		return windowAbsent
	default:
		return windowUnknown
	}
}

func quotaWindows(q quotaSnapshot) []quotaWindow {
	if len(q.Windows) > 0 {
		return q.Windows
	}
	if q.Primary.Presence == "" && q.Secondary.Presence == "" {
		return nil
	}
	return []quotaWindow{q.Primary, q.Secondary}
}

func windowFresh(w quotaWindow, now time.Time) bool {
	if parsePresence(w.Presence) != windowPresent || w.UsedPercent == nil || w.ResetAt == nil {
		return false
	}
	if *w.UsedPercent < 0 || *w.UsedPercent > 100 {
		return false
	}
	if w.LimitWindowSeconds == nil || w.ResetAfterSeconds == nil || *w.LimitWindowSeconds <= 0 || *w.ResetAfterSeconds < 0 || *w.ResetAfterSeconds > *w.LimitWindowSeconds {
		return false
	}
	if *w.ResetAt <= now.Unix() || absInt64(*w.ResetAt-(now.Unix()+*w.ResetAfterSeconds)) > 30 {
		return false
	}
	if w.UsedTokens != nil && *w.UsedTokens < 0 {
		return false
	}
	if (w.RemainingTokens == nil) != (w.LimitTokens == nil) {
		return false
	}
	if w.RemainingTokens != nil && (*w.RemainingTokens < 0 || *w.LimitTokens <= 0 || *w.RemainingTokens > *w.LimitTokens) {
		return false
	}
	if w.UsedTokens != nil && w.LimitTokens != nil && (*w.UsedTokens > *w.LimitTokens || *w.UsedTokens != *w.LimitTokens-*w.RemainingTokens) {
		return false
	}
	if *w.UsedPercent > 0 || (w.UsedTokens != nil && *w.UsedTokens > 0) || *w.ResetAfterSeconds < *w.LimitWindowSeconds {
		return false
	}
	return w.RemainingTokens == nil || *w.RemainingTokens == *w.LimitTokens
}

func freshQuota(q quotaSnapshot, now time.Time) bool {
	windows := quotaWindows(q)
	if len(windows) == 0 {
		return false
	}
	reported := 0
	for _, w := range windows {
		switch parsePresence(w.Presence) {
		case windowAbsent:
			continue
		case windowPresent:
			reported++
			if !windowFresh(w, now) {
				return false
			}
		default:
			return false
		}
	}
	return reported > 0
}

type activationResult struct {
	Total    int                 `json:"total"`
	Eligible int                 `json:"eligible"`
	Sent     int                 `json:"sent"`
	Skipped  int                 `json:"skipped"`
	Unknown  int                 `json:"unknown"`
	Outcomes []activationOutcome `json:"outcomes"`
}

type activationOutcome struct {
	Ordinal int    `json:"ordinal"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
}

// The production contract sends the normal streamed, non-persisted compact
// request. A narrowly scoped compatibility retry drops only stream/store when
// the server explicitly rejects one of those parameters as unknown.
var probeRequest = []byte(`{"model":"gpt-5.5","instructions":"Reply with OK.","input":[{"role":"user","content":[{"type":"input_text","text":"ping"}]}],"stream":true,"store":false}`)
var minimalRequest = []byte(`{"model":"gpt-5.5","instructions":"Reply with OK.","input":[{"role":"user","content":[{"type":"input_text","text":"ping"}]}]}`)

// Kept injectable for deterministic offline tests. The default implementation
// performs the fixed Codex compact request using a credential held only in
// memory for the duration of the call.
var upstreamRequest = defaultUpstreamRequest

// Keep inventory injectable at the job boundary. The production path still
// uses the host inventory, while schedule tests can exercise the complete
// worker without invoking a live CPA installation.
var igniteListAccounts = listAccounts

var errIgniteJobActive = errors.New("job_active")

const codexCompactURL = "https://chatgpt.com/backend-api/codex/responses/compact"

var codexCompactURLForTest string

func defaultUpstreamRequest(ctx context.Context, a account, body []byte) (int, error) {
	if !fixedBody(body) || strings.TrimSpace(a.AccessTokenValue) == "" {
		return 0, errors.New("credential_unavailable")
	}
	status, responseBody, err := doCompactRequest(ctx, a, body)
	if err != nil {
		return 0, err
	}
	if status == http.StatusBadRequest && shouldRetryMinimal(status, responseBody) && bytes.Equal(body, probeRequest) {
		status, _, err = doCompactRequest(ctx, a, minimalRequest)
	}
	return status, err
}

func doCompactRequest(ctx context.Context, a account, body []byte) (int, []byte, error) {
	target := codexCompactURL
	if codexCompactURLForTest != "" {
		target = codexCompactURLForTest
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return 0, nil, errors.New("upstream_request_failed")
	}
	req.Header.Set("Authorization", "Bearer "+a.AccessTokenValue)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Connection", "Keep-Alive")
	req.Header.Set("Originator", "codex-tui")
	// Keep the proven Codex client identity without embedding a developer's
	// operating-system, terminal, or machine-specific details.
	req.Header.Set("User-Agent", "codex-tui/0.135.0")
	if a.AccountID != "" {
		req.Header.Set("Chatgpt-Account-Id", a.AccountID)
	}
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	return resp.StatusCode, responseBody, nil
}

type igniteResponse struct {
	JobID string `json:"job_id"`
	State string `json:"state"`
	Total int    `json:"total"`
}

// beginIgnite owns the common preflight, durable action gate, and worker
// launch used by both the management action and the daily scheduler. The
// optional scheduled flag lets the worker publish terminal status back to the
// persisted schedule before it is launched, avoiding a completion race for
// very small fixture jobs.
func beginIgnite(scheduled ...bool) (igniteResponse, error) {
	accounts, err := igniteListAccounts()
	if err != nil {
		return igniteResponse{}, err
	}
	if countFreshActionable(accounts, timeNow()) == 0 {
		return igniteResponse{}, errNoActionableAccounts
	}
	id, err := globalJobs.start("ignite", len(accounts))
	if err != nil {
		if errors.Is(err, errIgniteJobActive) || strings.Contains(err.Error(), "job already active") {
			return igniteResponse{}, errIgniteJobActive
		}
		return igniteResponse{}, err
	}
	isScheduled := len(scheduled) > 0 && scheduled[0]
	if isScheduled {
		// Persist the attempted run before starting its goroutine. This leaves a
		// useful last_job_id even if the worker exits before its first callback.
		markIgniteScheduleStarted(id, timeNow().Unix())
	}
	go runIgniteJob(id, isScheduled)
	return igniteResponse{JobID: id, State: "running", Total: len(accounts)}, nil
}

func runIgniteJob(id string, scheduled bool) {
	ctx := globalJobs.context(id)
	current, inventoryErr := igniteListAccounts()
	if inventoryErr != nil {
		updateJob(id, "completed", "inventory_unavailable", 0)
		if scheduled {
			finishIgniteSchedule(id, "failed")
		}
		return
	}
	if db, dbErr := openStore(); dbErr == nil {
		_, _ = db.Exec(`UPDATE jobs SET total=?,updated_at=? WHERE id=?`, len(current), time.Now().Unix(), id)
	}
	result := ignite(ctx, current, timeNow())
	updateJobResult(id, result)
	if ctx.Err() != nil {
		updateJob(id, "cancelled", "cancelled", len(current))
		if scheduled {
			finishIgniteSchedule(id, "cancelled")
		}
	} else {
		updateJob(id, "completed", "", len(current))
		if scheduled {
			finishIgniteSchedule(id, "completed")
		}
	}
}

func shouldRetryMinimal(status int, body []byte) bool {
	if status != http.StatusBadRequest {
		return false
	}
	text := strings.ToLower(string(body))
	if !strings.Contains(text, "unknown_parameter") && !strings.Contains(text, "unknown parameter") {
		return false
	}
	return strings.Contains(text, "stream") || strings.Contains(text, "store")
}

func absInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func ignite(ctx context.Context, accounts []account, now time.Time) activationResult {
	if ctx == nil {
		ctx = context.Background()
	}
	out := activationResult{Total: len(accounts), Outcomes: make([]activationOutcome, 0, len(accounts))}
	for i, original := range accounts {
		row := activationOutcome{Ordinal: i + 1}
		if ctx != nil && ctx.Err() != nil {
			row.Status, row.Reason = "skipped", "cancelled"
			out.Skipped++
			out.Outcomes = append(out.Outcomes, row)
			continue
		}
		// Runtime-only or otherwise nonphysical records remain visible as a
		// sanitized skip, but never need a credential or quota read for Ignite.
		if !original.Physical {
			row.Status, row.Reason = "skipped", "ineligible"
			out.Skipped++
			out.Outcomes = append(out.Outcomes, row)
			continue
		}
		if original.Disabled || original.Expired || original.Unavailable {
			row.Status, row.Reason = "skipped", "ineligible"
			out.Skipped++
			out.Outcomes = append(out.Outcomes, row)
			continue
		}
		a := refreshCredential(original)
		if !credentialAvailable(a) {
			row.Status, row.Reason = "skipped", "ineligible"
			out.Skipped++
			out.Outcomes = append(out.Outcomes, row)
			continue
		}
		a = refreshQuota(a)
		// Physical records still need all other server-owned eligibility checks
		// before dispatch.
		if a.Disabled || a.Expired || a.Unavailable || !credentialAvailable(a) {
			row.Status, row.Reason = "skipped", "ineligible"
			out.Skipped++
			out.Outcomes = append(out.Outcomes, row)
			continue
		}
		key, keyErr := activationCycleKey(a)
		if keyErr != nil {
			row.Status, row.Reason = "skipped", "duplicate_cycle"
			out.Skipped++
			out.Outcomes = append(out.Outcomes, row)
			continue
		}
		if !freshQuota(a.Quota, now) {
			row.Status, row.Reason = "skipped", "not_fresh"
			out.Skipped++
			out.Outcomes = append(out.Outcomes, row)
			continue
		}
		out.Eligible++
		if ctx != nil && ctx.Err() != nil {
			row.Status, row.Reason = "skipped", "cancelled"
			out.Skipped++
			out.Outcomes = append(out.Outcomes, row)
			continue
		}
		cycle, err := reserveCycle(a.Key, key)
		if err != nil {
			row.Status, row.Reason = "failed_before_send", "state_unavailable"
			out.Skipped++
			out.Outcomes = append(out.Outcomes, row)
			continue
		}
		if !cycle {
			row.Status, row.Reason = "skipped", "duplicate_cycle"
			out.Skipped++
			out.Outcomes = append(out.Outcomes, row)
			continue
		}
		if err := setCycleBoundary(a.Key, key, safeCycleBoundary(a.Quota)); err != nil {
			// The request has not been dispatched yet, so a failed persistence
			// update is a definite pre-send failure and must fail closed.
			_ = updateCycle(a.Key, key, "failed_before_send")
			row.Status, row.Reason = "failed_before_send", "state_unavailable"
			out.Skipped++
			out.Outcomes = append(out.Outcomes, row)
			continue
		}
		if ctx != nil && ctx.Err() != nil {
			_ = updateCycle(a.Key, key, "failed_before_send")
			row.Status, row.Reason = "skipped", "cancelled"
			out.Skipped++
			out.Outcomes = append(out.Outcomes, row)
			continue
		}
		status, requestErr := upstreamRequest(ctx, a, probeRequest)
		if requestErr != nil {
			_ = updateCycle(a.Key, key, "sent_unknown")
			row.Status, row.Reason = "sent_unknown", "ambiguous_send"
			out.Unknown++
			out.Outcomes = append(out.Outcomes, row)
			continue
		}
		if status < 200 || status >= 300 {
			// The request reached an HTTP server. A whitelisted definite
			// rejection is terminal, while every other non-success response is
			// ambiguous and must remain permanently duplicate-blocked.
			if definiteHTTPRejection(status) {
				// Keep the persisted/result state aligned with the consequential
				// action contract: a whitelisted definite rejection is known to
				// have failed before any model success, so only this exact state is
				// eligible for a later explicit retry.
				_ = updateCycle(a.Key, key, "failed_before_send")
				row.Status, row.Reason = "failed_before_send", "upstream_rejected"
				out.Skipped++
			} else {
				_ = updateCycle(a.Key, key, "sent_unknown")
				row.Status, row.Reason = "sent_unknown", "ambiguous_response"
				out.Unknown++
			}
			out.Outcomes = append(out.Outcomes, row)
			continue
		}
		after := refreshQuota(a)
		verified := activationVerification(a.Quota, after.Quota)
		_ = updateCycle(a.Key, key, verified)
		_ = observeCycle(a.Key, key, after.Quota)
		row.Status, row.Reason = verified, ""
		if verified == "verified" {
			out.Sent++
		} else {
			out.Unknown++
		}
		out.Outcomes = append(out.Outcomes, row)
	}
	return out
}

func scanFreshEligible(a account, now time.Time) bool {
	if !a.Physical || a.Disabled || a.Expired || a.Unavailable || !credentialAvailable(a) {
		return false
	}
	key, err := activationCycleKey(a)
	if err != nil {
		return false
	}
	if !freshQuota(a.Quota, now) {
		return false
	}
	blocked, err := cycleBlocked(a.Key, key)
	return err == nil && !blocked
}

func countFreshActionable(accounts []account, now time.Time) int {
	count := 0
	for _, account := range accounts {
		if !account.Physical || account.Disabled || account.Expired || account.Unavailable {
			continue
		}
		if scanFreshEligible(refreshCredential(refreshQuota(account)), now) {
			count++
		}
	}
	return count
}

func definiteHTTPRejection(status int) bool {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden, http.StatusTooManyRequests:
		return true
	default:
		return false
	}
}

func activationVerification(before, after quotaSnapshot) string {
	beforeWindows, afterWindows := quotaWindows(before), quotaWindows(after)
	targets, evidence := 0, 0
	for i, beforeWindow := range beforeWindows {
		if parsePresence(beforeWindow.Presence) != windowPresent {
			continue
		}
		targets++
		if i < len(afterWindows) && activationWindowEvidence(beforeWindow, afterWindows[i]) {
			evidence++
		}
	}
	if targets > 0 && evidence == targets {
		return "verified"
	}
	if evidence > 0 {
		return "partial"
	}
	return "sent_unknown"
}

func activationWindowEvidence(before, after quotaWindow) bool {
	if parsePresence(after.Presence) != windowPresent {
		return false
	}
	if after.UsedPercent != nil && *after.UsedPercent > 0 && (before.UsedPercent == nil || *after.UsedPercent > *before.UsedPercent) {
		return true
	}
	if after.UsedTokens != nil && *after.UsedTokens > 0 && (before.UsedTokens == nil || *after.UsedTokens > *before.UsedTokens) {
		return true
	}
	if before.ResetAt == nil || before.ResetAfterSeconds == nil || before.LimitWindowSeconds == nil {
		return false
	}
	return *before.ResetAfterSeconds == *before.LimitWindowSeconds && after.ResetAfterSeconds != nil && after.LimitWindowSeconds != nil && *after.ResetAfterSeconds < *after.LimitWindowSeconds
}

func cycleKey(q quotaSnapshot) string {
	markers := make([]string, 0, len(quotaWindows(q)))
	for _, w := range quotaWindows(q) {
		switch parsePresence(w.Presence) {
		case windowAbsent:
			markers = append(markers, "absent")
		case windowPresent:
			if w.LimitWindowSeconds != nil && w.ResetAfterSeconds != nil && *w.LimitWindowSeconds == *w.ResetAfterSeconds && w.UsedPercent != nil && *w.UsedPercent == 0 && (w.UsedTokens == nil || *w.UsedTokens == 0) {
				markers = append(markers, "present:fresh:"+strconv.FormatInt(*w.LimitWindowSeconds, 10))
			} else {
				marker := "present:active"
				if w.LimitWindowSeconds != nil {
					marker += ":duration:" + strconv.FormatInt(*w.LimitWindowSeconds, 10)
				}
				if w.ResetAt != nil {
					marker += ":reset:" + strconv.FormatInt(*w.ResetAt, 10)
				}
				markers = append(markers, marker)
			}
		default:
			markers = append(markers, "unknown")
		}
	}
	sum := sha256.Sum256([]byte(strings.Join(markers, "\x00")))
	return hex.EncodeToString(sum[:])
}

func validFreshObservation(q quotaSnapshot) bool {
	return q.ObservedAt > 0 && freshQuota(q, time.Unix(q.ObservedAt, 0))
}

func validActiveObservation(q quotaSnapshot) bool {
	if q.ObservedAt <= 0 {
		return false
	}
	windows := quotaWindows(q)
	if len(windows) == 0 {
		return false
	}
	active := false
	for _, window := range windows {
		if parsePresence(window.Presence) != windowPresent {
			return false
		}
		if !freshQuota(quotaSnapshot{Windows: []quotaWindow{window}}, time.Unix(q.ObservedAt, 0)) {
			active = true
		}
	}
	return active
}

func observedSuccessorKey(freshBase, predecessor string, observedAt int64) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{freshBase, predecessor, "observed-refresh", strconv.FormatInt(observedAt, 10)}, "\x00")))
	return hex.EncodeToString(sum[:])
}

func scheduledSuccessorKey(freshBase, predecessor string, boundary int64) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{freshBase, predecessor, "scheduled-boundary", strconv.FormatInt(boundary, 10)}, "\x00")))
	return hex.EncodeToString(sum[:])
}

func safeCycleBoundary(q quotaSnapshot) int64 {
	var boundary int64
	for _, w := range quotaWindows(q) {
		if parsePresence(w.Presence) != windowPresent || w.ResetAt == nil || w.ResetAfterSeconds == nil || w.LimitWindowSeconds == nil || *w.ResetAfterSeconds > *w.LimitWindowSeconds {
			return 0
		}
		if *w.ResetAt > boundary {
			boundary = *w.ResetAt
		}
	}
	return boundary
}

func observeCycle(accountKey, cycle string, q quotaSnapshot) error {
	record, err := readCycle(accountKey, cycle)
	if err != nil {
		return err
	}
	if record.ActiveObserved == 0 && validActiveObservation(q) && q.ObservedAt >= record.ReservedAt {
		return updateCycleObservation(accountKey, cycle, q.ObservedAt, 0)
	}
	return nil
}

// activationCycleKey reconciles one monotonic active-to-fresh transition and
// returns the stable successor identity. It is bounded so malformed state
// cannot create an unbounded generation chain.
func activationCycleKey(a account) (string, error) {
	base := cycleKey(a.Quota)
	current := base
	if latest, err := latestCycle(a.Key); err == nil {
		current = latest.Key
		if validActiveObservation(a.Quota) && a.Quota.ObservedAt >= latest.ReservedAt && latest.ActiveObserved == 0 {
			if err := updateCycleObservation(a.Key, latest.Key, a.Quota.ObservedAt, 0); err != nil {
				return "", err
			}
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if current != base {
		// Continue reconciliation from the latest guarded predecessor, even
		// when its active quota naturally hashes to a different cycle key.
	}
	for generation := 0; generation < 32; generation++ {
		record, err := readCycle(a.Key, current)
		if errors.Is(err, sql.ErrNoRows) {
			return current, nil
		}
		if err != nil {
			return "", err
		}
		if record.ActiveObserved > 0 && validFreshObservation(a.Quota) && a.Quota.ObservedAt > record.ActiveObserved {
			refreshAt := record.RefreshObserved
			if refreshAt == 0 {
				refreshAt = a.Quota.ObservedAt
			}
			successor := observedSuccessorKey(base, current, refreshAt)
			if err := createSuccessorCycle(a.Key, current, a.Quota.ObservedAt, successor); err != nil {
				return "", err
			}
			current = successor
			continue
		}
		if record.ActiveObserved == 0 && record.NextCycleAfter > 0 && validFreshObservation(a.Quota) && a.Quota.ObservedAt > record.NextCycleAfter {
			successor := scheduledSuccessorKey(base, current, record.NextCycleAfter)
			if err := createBoundarySuccessor(a.Key, current, record.NextCycleAfter, successor); err != nil {
				return "", err
			}
			current = successor
			continue
		}
		return current, nil
	}
	return "", errors.New("cycle_generation_limit")
}

func fixedBody(req []byte) bool {
	return bytes.Equal(req, probeRequest) || bytes.Equal(req, minimalRequest)
}
