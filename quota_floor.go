package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"math"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	quotaFloorDefaultThreshold    = 95.0
	quotaFloorDefaultPoll         = 30
	quotaFloorPrimaryWindow       = int64(5 * 60 * 60)
	quotaFloorObservationMaxAge   = 2 * time.Minute
	quotaFloorStatusUnknown       = "unknown"
	quotaFloorStatusDisableFailed = "disable_failed"
	quotaFloorStatusEnableFailed  = "enable_failed"
)

type quotaFloorConfig struct {
	Enabled          bool    `json:"enabled"`
	TargetEmail      string  `json:"target_email"`
	ThresholdPercent float64 `json:"threshold_percent"`
	PollSeconds      int     `json:"poll_seconds"`
	// TargetName and TargetAuthIndex are the private durable identity used by
	// the controller. They must never cross the management API boundary.
	TargetName            string              `json:"-"`
	TargetAuthIndex       string              `json:"-"`
	TargetAccountID       string              `json:"-"`
	ControllerOwned       bool                `json:"controller_owned"`
	LastStatus            string              `json:"last_status"`
	LastObservedAt        int64               `json:"last_observed_at"`
	LastPrimaryPct        *float64            `json:"last_primary_percent,omitempty"`
	LastSecondaryPct      *float64            `json:"last_secondary_percent,omitempty"`
	LastPrimaryReset      int64               `json:"last_primary_reset_at,omitempty"`
	LastSecondaryReset    int64               `json:"last_secondary_reset_at,omitempty"`
	BlockedPrimaryReset   int64               `json:"blocked_primary_reset_at,omitempty"`
	BlockedSecondaryReset int64               `json:"blocked_secondary_reset_at,omitempty"`
	LastError             string              `json:"last_error,omitempty"`
	TargetSeat            string              `json:"target_seat,omitempty"`
	TargetStatus          string              `json:"target_status,omitempty"`
	Accounts              []quotaFloorAccount `json:"accounts"`
}

type quotaFloorAccount struct {
	Email  string `json:"email"`
	Seat   string `json:"seat"`
	Status string `json:"status"`
}

var quotaFloorStoreMu sync.Mutex
var quotaFloorTickInterval = quotaFloorDefaultPoll * time.Second
var quotaFloorNow = func() time.Time { return timeNow() }
var quotaFloorListAccounts = listAccounts
var quotaFloorRefresh = refreshQuota
var quotaFloorSave = saveAuthDisabled
var quotaFloorWrite = writeQuotaFloor
var quotaFloorHostCall = hostCall

func readQuotaFloor() (quotaFloorConfig, error) {
	db, err := openStore()
	if err != nil {
		return quotaFloorConfig{}, err
	}
	var enabled, owned int
	var cfg quotaFloorConfig
	var primary, secondary sql.NullFloat64
	err = db.QueryRow(`SELECT enabled,target_email,threshold_percent,poll_seconds,target_name,target_auth_index,target_account_id,controller_owned,last_status,last_observed_at,last_primary_percent,last_secondary_percent,last_primary_reset_at,last_secondary_reset_at,blocked_primary_reset_at,blocked_secondary_reset_at,last_error FROM quota_floor WHERE id=1`).Scan(&enabled, &cfg.TargetEmail, &cfg.ThresholdPercent, &cfg.PollSeconds, &cfg.TargetName, &cfg.TargetAuthIndex, &cfg.TargetAccountID, &owned, &cfg.LastStatus, &cfg.LastObservedAt, &primary, &secondary, &cfg.LastPrimaryReset, &cfg.LastSecondaryReset, &cfg.BlockedPrimaryReset, &cfg.BlockedSecondaryReset, &cfg.LastError)
	if err != nil {
		return quotaFloorConfig{}, err
	}
	cfg.Enabled, cfg.ControllerOwned = enabled != 0, owned != 0
	if primary.Valid {
		cfg.LastPrimaryPct = &primary.Float64
	}
	if secondary.Valid {
		cfg.LastSecondaryPct = &secondary.Float64
	}
	return cfg, nil
}

func writeQuotaFloor(cfg quotaFloorConfig) error {
	db, err := openStore()
	if err != nil {
		return err
	}
	var primary, secondary any
	if cfg.LastPrimaryPct != nil {
		primary = *cfg.LastPrimaryPct
	}
	if cfg.LastSecondaryPct != nil {
		secondary = *cfg.LastSecondaryPct
	}
	_, err = db.Exec(`UPDATE quota_floor SET enabled=?,target_email=?,threshold_percent=?,poll_seconds=?,target_name=?,target_auth_index=?,target_account_id=?,controller_owned=?,last_status=?,last_observed_at=?,last_primary_percent=?,last_secondary_percent=?,last_primary_reset_at=?,last_secondary_reset_at=?,blocked_primary_reset_at=?,blocked_secondary_reset_at=?,last_error=? WHERE id=1`, boolInt(cfg.Enabled), strings.TrimSpace(cfg.TargetEmail), cfg.ThresholdPercent, cfg.PollSeconds, strings.TrimSpace(cfg.TargetName), strings.TrimSpace(cfg.TargetAuthIndex), strings.TrimSpace(cfg.TargetAccountID), boolInt(cfg.ControllerOwned), strings.TrimSpace(cfg.LastStatus), cfg.LastObservedAt, primary, secondary, cfg.LastPrimaryReset, cfg.LastSecondaryReset, cfg.BlockedPrimaryReset, cfg.BlockedSecondaryReset, strings.TrimSpace(cfg.LastError))
	return err
}

func quotaFloorWindowCurrent(w quotaWindow, now time.Time) bool {
	if parsePresence(w.Presence) != windowPresent || w.UsedPercent == nil || w.ResetAt == nil || w.LimitWindowSeconds == nil || w.ResetAfterSeconds == nil {
		return false
	}
	// Providers may expose reset_after as the window length rather than the
	// remaining time. In that representation, reset_at still must be close to
	// one full window from now; otherwise a fabricated far-future reset could
	// look valid and authorize quota protection indefinitely.
	if math.IsNaN(*w.UsedPercent) || math.IsInf(*w.UsedPercent, 0) || *w.UsedPercent < 0 || *w.UsedPercent > 100 || *w.ResetAt <= now.Unix() || *w.LimitWindowSeconds <= 0 || *w.ResetAfterSeconds < 0 || *w.ResetAfterSeconds > *w.LimitWindowSeconds || *w.ResetAt > now.Unix()+14*24*60*60 {
		return false
	}
	remaining := *w.ResetAt - now.Unix()
	if *w.ResetAfterSeconds == *w.LimitWindowSeconds {
		const resetAtTolerance = int64((quotaFloorObservationMaxAge + 30*time.Second) / time.Second)
		if absInt64(remaining-*w.LimitWindowSeconds) > resetAtTolerance {
			return false
		}
	} else if absInt64(remaining-*w.ResetAfterSeconds) > 30 {
		return false
	}
	if w.UsedTokens != nil && *w.UsedTokens < 0 {
		return false
	}
	if (w.RemainingTokens == nil) != (w.LimitTokens == nil) {
		return false
	}
	if w.RemainingTokens != nil && (*w.RemainingTokens < 0 || w.LimitTokens == nil || *w.LimitTokens <= 0 || *w.RemainingTokens > *w.LimitTokens) {
		return false
	}
	if w.UsedTokens != nil && w.LimitTokens != nil && (*w.UsedTokens > *w.LimitTokens || *w.UsedTokens != *w.LimitTokens-*w.RemainingTokens) {
		return false
	}
	return true
}

func quotaFloorPrimaryWindowCurrent(w quotaWindow, now time.Time) bool {
	return quotaFloorWindowCurrent(w, now) && w.LimitWindowSeconds != nil && *w.LimitWindowSeconds == quotaFloorPrimaryWindow
}

// quotaFloorDecision protects the provider's five-hour primary window. The
// weekly secondary window is reported for visibility and reset diagnostics,
// but it must never independently disable the selected account.
func quotaFloorDecision(q quotaSnapshot, now time.Time, threshold float64) (status string, primary, secondary *quotaWindow) {
	if q.ObservedAt <= 0 || q.ObservedAt > now.Unix() || now.Sub(time.Unix(q.ObservedAt, 0)) > quotaFloorObservationMaxAge {
		return quotaFloorStatusUnknown, nil, nil
	}
	windows := quotaWindows(q)
	observationNow := time.Unix(q.ObservedAt, 0)
	if observationNow.After(now) {
		observationNow = now
	}
	if math.IsNaN(threshold) || math.IsInf(threshold, 0) || threshold <= 0 || threshold >= 100 || len(windows) < 1 || !quotaFloorPrimaryWindowCurrent(windows[0], observationNow) || *windows[0].ResetAt <= now.Unix() {
		return quotaFloorStatusUnknown, nil, nil
	}
	primary = &windows[0]
	if len(windows) > 1 && quotaFloorWindowCurrent(windows[1], observationNow) && (windows[1].ResetAt == nil || *windows[1].ResetAt > now.Unix()) {
		secondary = &windows[1]
	} else {
		secondary = nil
	}
	if *primary.UsedPercent >= threshold {
		return "block", primary, secondary
	}
	return "allow", primary, secondary
}

func saveAuthDisabled(a account, disabled bool) error {
	raw, err := quotaFloorHostCall("host.auth.get", map[string]string{"auth_index": a.AuthIndex})
	if err != nil {
		return err
	}
	var got struct {
		Name string          `json:"name"`
		JSON json.RawMessage `json:"json"`
	}
	if err := json.Unmarshal(raw, &got); err != nil || len(got.JSON) == 0 {
		return errors.New("auth_json_unavailable")
	}
	// The inventory's physical file name is the durable identity. The host
	// response may expose a display/runtime name, so never let it redirect a
	// disable or re-enable to another same-email record.
	name := strings.TrimSpace(a.AuthFile)
	if name == "" {
		name = strings.TrimSpace(got.Name)
	}
	if name == "" || filepath.Base(name) != name || !strings.EqualFold(filepath.Ext(name), ".json") {
		return errors.New("auth_name_unavailable")
	}
	if a.AuthFile != "" && !strings.EqualFold(filepath.Base(a.AuthFile), name) {
		return errors.New("auth_identity_mismatch")
	}
	var value map[string]any
	if json.Unmarshal(got.JSON, &value) != nil || value == nil {
		return errors.New("auth_json_invalid")
	}
	if strings.TrimSpace(got.Name) == "" || !strings.EqualFold(strings.TrimSpace(got.Name), name) {
		return errors.New("auth_identity_mismatch")
	}
	if a.AccountID != "" {
		_, fetchedAccountID := credentialFromRaw(got.JSON)
		if fetchedAccountID == "" || !strings.EqualFold(strings.TrimSpace(fetchedAccountID), strings.TrimSpace(a.AccountID)) {
			return errors.New("auth_identity_mismatch")
		}
	}
	value["disabled"] = disabled
	updated, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if _, err = quotaFloorHostCall("host.auth.save", map[string]any{"name": name, "json": json.RawMessage(updated)}); err != nil {
		return err
	}
	// Verify both persistence and routing before recording ownership. Older
	// CPA hosts accepted disabled JSON but rebuilt the runtime auth as enabled,
	// allowing their token store to overwrite the file with disabled=false.
	verifiedRaw, err := quotaFloorHostCall("host.auth.get", map[string]string{"auth_index": a.AuthIndex})
	if err != nil {
		return err
	}
	var verified struct {
		JSON json.RawMessage `json:"json"`
	}
	if err := json.Unmarshal(verifiedRaw, &verified); err != nil || len(verified.JSON) == 0 {
		return errors.New("auth_json_unavailable_after_save")
	}
	var verifiedValue struct {
		Disabled *bool `json:"disabled"`
	}
	if err := json.Unmarshal(verified.JSON, &verifiedValue); err != nil {
		return errors.New("auth_json_invalid_after_save")
	}
	if a.AccountID != "" {
		_, verifiedAccountID := credentialFromRaw(verified.JSON)
		if verifiedAccountID == "" || !strings.EqualFold(strings.TrimSpace(verifiedAccountID), strings.TrimSpace(a.AccountID)) {
			return errors.New("auth_identity_mismatch")
		}
	}
	if verifiedValue.Disabled == nil || *verifiedValue.Disabled != disabled {
		return errors.New("auth_disabled_not_persisted")
	}
	runtimeRaw, err := quotaFloorHostCall("host.auth.get_runtime", map[string]string{"auth_index": a.AuthIndex})
	if err != nil {
		return err
	}
	var runtime struct {
		Auth struct {
			AuthIndex *string `json:"auth_index"`
			Name      *string `json:"name"`
			Disabled  *bool   `json:"disabled"`
			Status    *string `json:"status"`
		} `json:"auth"`
	}
	if err := json.Unmarshal(runtimeRaw, &runtime); err != nil || runtime.Auth.AuthIndex == nil || runtime.Auth.Name == nil || runtime.Auth.Disabled == nil || runtime.Auth.Status == nil || *runtime.Auth.AuthIndex != a.AuthIndex || *runtime.Auth.Name != name {
		return errors.New("auth_runtime_identity_unavailable")
	}
	runtimeStatus := strings.ToLower(strings.TrimSpace(*runtime.Auth.Status))
	statusDisabled := runtimeStatus == "disabled"
	if *runtime.Auth.Disabled != disabled || statusDisabled != disabled {
		return errors.New("auth_disabled_not_applied")
	}
	if !disabled && runtimeStatus != "active" && runtimeStatus != "healthy" && runtimeStatus != "ready" && runtimeStatus != "ok" {
		return errors.New("auth_disabled_not_applied")
	}
	return nil
}

func quotaFloorTarget(accounts []account, cfg quotaFloorConfig) (account, bool) {
	name := strings.TrimSpace(cfg.TargetName)
	index := strings.TrimSpace(cfg.TargetAuthIndex)
	email := strings.TrimSpace(cfg.TargetEmail)
	accountID := strings.TrimSpace(cfg.TargetAccountID)
	if (name == "") != (index == "") {
		return account{}, false
	}
	matches := func(a account, allowRebind bool) (account, bool) {
		if !a.Physical || !strings.EqualFold(strings.TrimSpace(a.Email), email) || (name != "" && (a.AuthFile != name || (!allowRebind && a.AuthIndex != index))) {
			return account{}, false
		}
		if name != "" && !allowRebind && a.AuthIndex != index {
			return account{}, false
		}
		if accountID != "" {
			if a.AccountID == "" {
				a = refreshCredential(a)
			}
			if strings.TrimSpace(a.AccountID) != accountID {
				return account{}, false
			}
		}
		return a, true
	}
	if name != "" {
		for _, a := range accounts {
			if target, ok := matches(a, false); ok {
				return target, true
			}
		}
		// CPA can assign a new auth index after host.auth.save rewrites the
		// same physical file. Rebind only when the filename, email, and (when
		// available) immutable provider account ID still identify one record.
		var rebound account
		count := 0
		for _, a := range accounts {
			if target, ok := matches(a, true); ok {
				rebound, count = target, count+1
			}
		}
		return rebound, count == 1
	}
	// A persisted email without the exact physical filename/auth-index pair is
	// legacy or incomplete configuration. Never auto-bind it to whichever
	// current seat happens to have that email; require an explicit seat save.
	return account{}, false
}

func quotaFloorTickAt(now time.Time) error {
	quotaFloorStoreMu.Lock()
	defer quotaFloorStoreMu.Unlock()
	cfg, err := readQuotaFloor()
	if err != nil || !cfg.Enabled {
		return err
	}
	accounts, err := quotaFloorListAccounts()
	if err != nil {
		cfg.LastStatus, cfg.LastError = "inventory_unavailable", "inventory_unavailable"
		return quotaFloorWrite(cfg)
	}
	hadIdentity := cfg.TargetName != "" || cfg.TargetAuthIndex != "" || cfg.TargetAccountID != ""
	target, ok := quotaFloorTarget(accounts, cfg)
	if !ok {
		cfg.LastStatus, cfg.LastError = "target_missing", "target_missing"
		return quotaFloorWrite(cfg)
	}
	if cfg.TargetName == "" {
		cfg.TargetName = target.AuthFile
		if !hadIdentity {
			cfg.ControllerOwned = false
		}
	}
	if cfg.TargetAuthIndex != target.AuthIndex {
		cfg.TargetAuthIndex = target.AuthIndex
	}
	// Force the injected/production probe to publish a new observation; an old
	// in-memory projection must never be reused after a failed request.
	target.Quota = quotaSnapshot{}
	target.QuotaStatusCode = 0
	target.QuotaError = ""
	target = quotaFloorRefresh(target)
	if cfg.TargetAccountID != "" && strings.TrimSpace(target.AccountID) != cfg.TargetAccountID {
		cfg.LastStatus, cfg.LastError = "target_identity_mismatch", "target_identity_mismatch"
		return quotaFloorWrite(cfg)
	}
	if cfg.TargetAccountID == "" && strings.TrimSpace(target.AccountID) != "" {
		cfg.TargetAccountID = strings.TrimSpace(target.AccountID)
	}
	decisionNow := now
	if target.Quota.ObservedAt > decisionNow.Unix() && target.Quota.ObservedAt-decisionNow.Unix() <= int64(quotaFloorObservationMaxAge/time.Second) {
		// The probe can finish just after the scheduler captured its tick time.
		// Evaluate reset/age metadata against the observation timestamp rather
		// than turning a normal network boundary into an unknown result.
		decisionNow = time.Unix(target.Quota.ObservedAt, 0)
	}
	status, primary, secondary := quotaFloorDecision(target.Quota, decisionNow, cfg.ThresholdPercent)
	priorPrimaryReset := cfg.LastPrimaryReset
	cfg.LastObservedAt = target.Quota.ObservedAt
	if cfg.LastObservedAt <= 0 {
		cfg.LastObservedAt = now.Unix()
	}
	cfg.LastPrimaryPct, cfg.LastSecondaryPct = nil, nil
	cfg.LastPrimaryReset, cfg.LastSecondaryReset = 0, 0
	cfg.LastError = strings.TrimSpace(target.QuotaError)
	if primary != nil {
		cfg.LastPrimaryPct, cfg.LastPrimaryReset = primary.UsedPercent, valueOrZero(primary.ResetAt)
	}
	if secondary != nil {
		cfg.LastSecondaryPct, cfg.LastSecondaryReset = secondary.UsedPercent, valueOrZero(secondary.ResetAt)
	}
	if status == quotaFloorStatusUnknown {
		if cfg.LastError == "" {
			cfg.LastError = "quota_observation_invalid"
			if target.Quota.ObservedAt > 0 && now.Unix()-target.Quota.ObservedAt > int64(quotaFloorObservationMaxAge/time.Second) {
				cfg.LastError = "quota_observation_stale"
			}
		}
		// Unknown monitoring is fail-closed for an enabled target. An account
		// already disabled by Phoenix is held without another host.auth.save;
		// externally disabled records remain untouched.
		if target.Disabled {
			if cfg.ControllerOwned {
				cfg.LastStatus = quotaFloorStatusUnknown
			} else {
				cfg.LastStatus = "externally_disabled"
			}
			return quotaFloorWrite(cfg)
		}
		if err := quotaFloorSave(target, true); err != nil {
			cfg.LastStatus, cfg.LastError = quotaFloorStatusDisableFailed, "disable_failed"
			return quotaFloorWrite(cfg)
		}
		cfg.ControllerOwned, cfg.LastStatus = true, quotaFloorStatusUnknown
		if cfg.BlockedPrimaryReset == 0 && priorPrimaryReset > 0 {
			cfg.BlockedPrimaryReset = priorPrimaryReset
		}
		return quotaFloorWrite(cfg)
	}
	cfg.LastError = ""
	if status == "block" {
		if target.Disabled {
			if cfg.ControllerOwned && cfg.BlockedPrimaryReset == 0 {
				cfg.BlockedPrimaryReset = valueOrZero(primary.ResetAt)
			}
			if cfg.ControllerOwned {
				cfg.LastStatus = "disabled"
			} else {
				cfg.LastStatus = "externally_disabled"
			}
			return quotaFloorWrite(cfg)
		}
		if err := quotaFloorSave(target, true); err != nil {
			cfg.LastStatus, cfg.LastError = quotaFloorStatusDisableFailed, "disable_failed"
			return quotaFloorWrite(cfg)
		}
		cfg.ControllerOwned, cfg.LastStatus = true, "disabled"
		cfg.BlockedPrimaryReset = valueOrZero(primary.ResetAt)
	} else if target.Disabled {
		if !cfg.ControllerOwned {
			cfg.LastStatus = "externally_disabled"
			return quotaFloorWrite(cfg)
		}
		if !quotaFloorResetObserved(cfg, primary, secondary) {
			cfg.LastStatus = "disabled_waiting_reset"
			return quotaFloorWrite(cfg)
		}
		if err := quotaFloorSave(target, false); err != nil {
			cfg.LastStatus, cfg.LastError = quotaFloorStatusEnableFailed, "enable_failed"
			return quotaFloorWrite(cfg)
		}
		cfg.ControllerOwned, cfg.LastStatus = false, "enabled"
		cfg.BlockedPrimaryReset, cfg.BlockedSecondaryReset = 0, 0
	} else {
		// Another actor re-enabled a Phoenix-owned record. Clear stale
		// ownership/reset markers so a later external disable cannot be
		// mistaken for a Phoenix-owned state.
		cfg.ControllerOwned = false
		cfg.BlockedPrimaryReset, cfg.BlockedSecondaryReset = 0, 0
		cfg.LastStatus = "allowed"
	}
	return quotaFloorWrite(cfg)
}

func quotaFloorResetObserved(cfg quotaFloorConfig, primary, secondary *quotaWindow) bool {
	if primary == nil || cfg.BlockedPrimaryReset == 0 {
		return false
	}
	return valueOrZero(primary.ResetAt) > cfg.BlockedPrimaryReset
}

func valueOrZero(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

type quotaFloorSchedulerRuntime struct {
	mu      sync.Mutex
	cancel  chan struct{}
	done    chan struct{}
	running bool
}

var globalQuotaFloorScheduler = &quotaFloorSchedulerRuntime{}

func startQuotaFloorScheduler() {
	globalQuotaFloorScheduler.mu.Lock()
	if globalQuotaFloorScheduler.running {
		globalQuotaFloorScheduler.mu.Unlock()
		return
	}
	cancel, done := make(chan struct{}), make(chan struct{})
	globalQuotaFloorScheduler.cancel, globalQuotaFloorScheduler.done, globalQuotaFloorScheduler.running = cancel, done, true
	globalQuotaFloorScheduler.mu.Unlock()
	go func() {
		defer close(done)
		interval := configuredQuotaFloorInterval()
		if err := quotaFloorTickAt(quotaFloorNow()); err != nil {
			log.Printf("cpa-phoenix quota-floor tick failed: %v", err)
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := quotaFloorTickAt(quotaFloorNow()); err != nil {
					log.Printf("cpa-phoenix quota-floor tick failed: %v", err)
				}
				next := configuredQuotaFloorInterval()
				if next != interval {
					ticker.Stop()
					interval = next
					ticker = time.NewTicker(interval)
				}
			case <-cancel:
				return
			}
		}
	}()
}

func configuredQuotaFloorInterval() time.Duration {
	if cfg, err := readQuotaFloor(); err == nil && cfg.PollSeconds >= 5 && cfg.PollSeconds <= 3600 {
		return time.Duration(cfg.PollSeconds) * time.Second
	}
	if quotaFloorTickInterval >= 5*time.Second {
		return quotaFloorTickInterval
	}
	return quotaFloorDefaultPoll * time.Second
}

func stopQuotaFloorScheduler() {
	globalQuotaFloorScheduler.mu.Lock()
	if !globalQuotaFloorScheduler.running {
		globalQuotaFloorScheduler.mu.Unlock()
		return
	}
	cancel, done := globalQuotaFloorScheduler.cancel, globalQuotaFloorScheduler.done
	globalQuotaFloorScheduler.cancel, globalQuotaFloorScheduler.done, globalQuotaFloorScheduler.running = nil, nil, false
	globalQuotaFloorScheduler.mu.Unlock()
	close(cancel)
	<-done
}

func quotaFloorAccountStatus(a account) string {
	if invalidAccount(a, 0) {
		return "invalid"
	}
	if a.Disabled {
		return "disabled"
	}
	if a.Expired {
		return "expired"
	}
	if a.Unavailable {
		return "unavailable"
	}
	return "healthy"
}

func quotaFloorPublicConfig(cfg quotaFloorConfig, accounts []account) quotaFloorConfig {
	public := cfg
	public.TargetSeat = ""
	public.TargetStatus = ""
	public.Accounts = make([]quotaFloorAccount, 0)
	target, ok := quotaFloorTarget(accounts, cfg)
	identityUnavailable := false
	if !ok && strings.TrimSpace(cfg.TargetAccountID) != "" {
		// Keep the configured seat visible during a transient credential lookup
		// failure, but mark its runtime status unknown so the UI cannot claim
		// current protection until the immutable binding is revalidated.
		fallback := cfg
		fallback.TargetAccountID = ""
		target, ok = quotaFloorTarget(accounts, fallback)
		identityUnavailable = ok
	}
	if ok {
		public.TargetSeat = stableSeatLabel(target)
		public.TargetStatus = quotaFloorAccountStatus(target)
		if identityUnavailable {
			public.TargetStatus = "unknown"
		}
	}
	for _, a := range accounts {
		if !a.Physical {
			continue
		}
		// The selected target may have its immutable provider ID hydrated by
		// quotaFloorTarget, while host.auth.list omits that private ID from
		// public inventory rows. Copy it only into the opaque seat label so
		// the selected option remains visible without exposing the ID itself.
		if ok && a.AuthFile == target.AuthFile && a.AuthIndex == target.AuthIndex && strings.EqualFold(strings.TrimSpace(a.Email), strings.TrimSpace(target.Email)) {
			a.AccountID = target.AccountID
		}
		public.Accounts = append(public.Accounts, quotaFloorAccount{Email: a.Email, Seat: stableSeatLabel(a), Status: quotaFloorAccountStatus(a)})
	}
	return public
}

func quotaFloorResponse(cfg quotaFloorConfig, accountSets ...[]account) managementResponse {
	var accounts []account
	if len(accountSets) > 0 {
		accounts = accountSets[0]
	}
	return jsonResponse(http.StatusOK, quotaFloorPublicConfig(cfg, accounts))
}

func quotaFloorTargetBySeat(accounts []account, seat string) (account, bool) {
	// Seat labels are the only browser-visible selector. Resolve them against
	// the current physical inventory, then persist the filename and auth index
	// so future controller ticks remain exact even when emails are shared.
	seat = strings.TrimSpace(seat)
	if seat == "" {
		return account{}, false
	}
	var target account
	matches := 0
	for _, a := range accounts {
		if !a.Physical || stableSeatLabel(a) != seat {
			continue
		}
		target = a
		matches++
	}
	return target, matches == 1
}

func quotaFloorTargetByIdentity(accounts []account, name, index, email string) (account, bool) {
	name, index, email = strings.TrimSpace(name), strings.TrimSpace(index), strings.TrimSpace(email)
	var target account
	matches := 0
	for _, a := range accounts {
		if !a.Physical || a.AuthFile != name || a.AuthIndex != index || (email != "" && !strings.EqualFold(strings.TrimSpace(a.Email), email)) {
			continue
		}
		target, matches = a, matches+1
	}
	return target, matches == 1
}

func quotaFloorTargetByEmail(accounts []account, email string) (account, bool) {
	email = strings.TrimSpace(email)
	var target account
	matches := 0
	for _, a := range accounts {
		if !a.Physical || !strings.EqualFold(strings.TrimSpace(a.Email), email) {
			continue
		}
		target, matches = a, matches+1
	}
	return target, matches == 1
}

func validQuotaFloorIdentity(name, index string) bool {
	name, index = strings.TrimSpace(name), strings.TrimSpace(index)
	if name == "" || index == "" || len(name) > 255 || len(index) > 255 || filepath.Base(name) != name || !strings.EqualFold(filepath.Ext(name), ".json") || strings.ContainsAny(name, `/\\`) || strings.ContainsAny(index, `/\\`) {
		return false
	}
	for _, value := range name + index {
		if value < 0x20 || value == 0x7f {
			return false
		}
	}
	return true
}

func resetQuotaFloorObservation(cfg *quotaFloorConfig) {
	cfg.ControllerOwned = false
	cfg.LastStatus = "unknown"
	cfg.LastObservedAt = 0
	cfg.LastPrimaryPct, cfg.LastSecondaryPct = nil, nil
	cfg.LastPrimaryReset, cfg.LastSecondaryReset = 0, 0
	cfg.BlockedPrimaryReset, cfg.BlockedSecondaryReset = 0, 0
	cfg.LastError = ""
}

func handleQuotaFloorManagement(req managementRequest) managementResponse {
	quotaFloorStoreMu.Lock()
	defer quotaFloorStoreMu.Unlock()
	cfg, err := readQuotaFloor()
	if err != nil {
		return jsonResponse(http.StatusServiceUnavailable, map[string]string{"error": "quota_floor_unavailable"})
	}
	if req.Method == http.MethodGet {
		accounts, inventoryErr := quotaFloorListAccounts()
		if inventoryErr != nil {
			return jsonResponse(http.StatusServiceUnavailable, map[string]string{"error": "inventory_unavailable"})
		}
		return quotaFloorResponse(cfg, accounts)
	}
	if req.Method != http.MethodPost {
		return jsonResponse(http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
	var body struct {
		Enabled          *bool    `json:"enabled"`
		TargetEmail      string   `json:"target_email"`
		TargetSeat       string   `json:"target_seat"`
		TargetName       string   `json:"target_name"`
		TargetAuthIndex  string   `json:"target_auth_index"`
		AdoptOwned       *bool    `json:"adopt_controller_owned"`
		ThresholdPercent *float64 `json:"threshold_percent"`
		PollSeconds      *int     `json:"poll_seconds"`
		Acknowledge      string   `json:"acknowledge"`
	}
	if json.Unmarshal(req.Body, &body) != nil || body.Acknowledge != "CPA_PHOENIX_ONE_CLICK" {
		return jsonResponse(http.StatusBadRequest, map[string]string{"error": "acknowledgement_required"})
	}
	if strings.TrimSpace(body.TargetEmail) != "" && !strings.Contains(body.TargetEmail, "@") {
		return jsonResponse(http.StatusBadRequest, map[string]string{"error": "invalid_target_email"})
	}
	if body.ThresholdPercent != nil && (*body.ThresholdPercent <= 0 || *body.ThresholdPercent >= 100) {
		return jsonResponse(http.StatusBadRequest, map[string]string{"error": "invalid_threshold_percent"})
	}
	if body.PollSeconds != nil && (*body.PollSeconds < 5 || *body.PollSeconds > 3600) {
		return jsonResponse(http.StatusBadRequest, map[string]string{"error": "invalid_poll_seconds"})
	}
	if strings.TrimSpace(body.TargetSeat) != "" && (strings.TrimSpace(body.TargetEmail) != "" || strings.TrimSpace(body.TargetName) != "" || strings.TrimSpace(body.TargetAuthIndex) != "") {
		return jsonResponse(http.StatusBadRequest, map[string]string{"error": "invalid_target_identity"})
	}
	// A Phoenix-owned disabled target must remain under this controller until a
	// valid later five-hour reset is observed. Rejecting a target switch or an
	// off request avoids abandoning an account in a Phoenix-owned disabled state.
	if cfg.ControllerOwned && cfg.LastStatus != "allowed" && cfg.LastStatus != "enabled" && ((body.Enabled != nil && !*body.Enabled) || strings.TrimSpace(body.TargetSeat) != "" || strings.TrimSpace(body.TargetName) != "" || strings.TrimSpace(body.TargetAuthIndex) != "" || strings.TrimSpace(body.TargetEmail) != "") {
		return jsonResponse(http.StatusConflict, map[string]string{"error": "controller_owned_target_requires_reset"})
	}
	if body.Enabled != nil && !*body.Enabled {
		cfg.Enabled = false
	} else if body.Enabled != nil {
		cfg.Enabled = true
	}
	if strings.TrimSpace(body.TargetSeat) != "" {
		accounts, inventoryErr := quotaFloorListAccounts()
		if inventoryErr != nil {
			return jsonResponse(http.StatusServiceUnavailable, map[string]string{"error": "inventory_unavailable"})
		}
		target, ok := quotaFloorTargetBySeat(accounts, body.TargetSeat)
		if !ok {
			return jsonResponse(http.StatusBadRequest, map[string]string{"error": "invalid_target_seat"})
		}
		target = refreshCredential(target)
		if cfg.TargetName == target.AuthFile && cfg.TargetAuthIndex == target.AuthIndex && strings.TrimSpace(cfg.TargetAccountID) != "" && strings.TrimSpace(target.AccountID) != "" && cfg.TargetAccountID != strings.TrimSpace(target.AccountID) {
			return jsonResponse(http.StatusBadRequest, map[string]string{"error": "invalid_target_identity"})
		}
		changed := cfg.TargetEmail != strings.TrimSpace(target.Email) || cfg.TargetName != target.AuthFile || cfg.TargetAuthIndex != target.AuthIndex || (strings.TrimSpace(target.AccountID) != "" && cfg.TargetAccountID != strings.TrimSpace(target.AccountID))
		cfg.TargetEmail, cfg.TargetName, cfg.TargetAuthIndex = strings.TrimSpace(target.Email), target.AuthFile, target.AuthIndex
		if strings.TrimSpace(target.AccountID) != "" {
			cfg.TargetAccountID = strings.TrimSpace(target.AccountID)
		} else if changed {
			cfg.TargetAccountID = ""
		}
		if changed {
			resetQuotaFloorObservation(&cfg)
		}
	} else if strings.TrimSpace(body.TargetName) != "" || strings.TrimSpace(body.TargetAuthIndex) != "" {
		name, index := strings.TrimSpace(body.TargetName), strings.TrimSpace(body.TargetAuthIndex)
		if !validQuotaFloorIdentity(name, index) {
			return jsonResponse(http.StatusBadRequest, map[string]string{"error": "invalid_target_identity"})
		}
		accounts, inventoryErr := quotaFloorListAccounts()
		if inventoryErr != nil {
			return jsonResponse(http.StatusServiceUnavailable, map[string]string{"error": "inventory_unavailable"})
		}
		target, ok := quotaFloorTargetByIdentity(accounts, name, index, strings.TrimSpace(body.TargetEmail))
		if !ok {
			return jsonResponse(http.StatusBadRequest, map[string]string{"error": "invalid_target_identity"})
		}
		target = refreshCredential(target)
		if cfg.TargetName == target.AuthFile && cfg.TargetAuthIndex == target.AuthIndex && strings.TrimSpace(cfg.TargetAccountID) != "" && strings.TrimSpace(target.AccountID) != "" && cfg.TargetAccountID != strings.TrimSpace(target.AccountID) {
			return jsonResponse(http.StatusBadRequest, map[string]string{"error": "invalid_target_identity"})
		}
		changed := cfg.TargetEmail != strings.TrimSpace(target.Email) || cfg.TargetName != target.AuthFile || cfg.TargetAuthIndex != target.AuthIndex || (strings.TrimSpace(target.AccountID) != "" && cfg.TargetAccountID != strings.TrimSpace(target.AccountID))
		cfg.TargetEmail, cfg.TargetName, cfg.TargetAuthIndex = strings.TrimSpace(target.Email), target.AuthFile, target.AuthIndex
		if strings.TrimSpace(target.AccountID) != "" {
			cfg.TargetAccountID = strings.TrimSpace(target.AccountID)
		} else if changed {
			cfg.TargetAccountID = ""
		}
		if changed {
			resetQuotaFloorObservation(&cfg)
		}
		if body.AdoptOwned != nil {
			cfg.ControllerOwned = *body.AdoptOwned
		}
	} else if email := strings.TrimSpace(body.TargetEmail); email != "" {
		accounts, inventoryErr := quotaFloorListAccounts()
		if inventoryErr != nil {
			return jsonResponse(http.StatusServiceUnavailable, map[string]string{"error": "inventory_unavailable"})
		}
		target, ok := quotaFloorTargetByEmail(accounts, email)
		if !ok {
			return jsonResponse(http.StatusBadRequest, map[string]string{"error": "invalid_target_email"})
		}
		target = refreshCredential(target)
		if cfg.TargetName == target.AuthFile && cfg.TargetAuthIndex == target.AuthIndex && strings.TrimSpace(cfg.TargetAccountID) != "" && strings.TrimSpace(target.AccountID) != "" && cfg.TargetAccountID != strings.TrimSpace(target.AccountID) {
			return jsonResponse(http.StatusBadRequest, map[string]string{"error": "invalid_target_identity"})
		}
		changed := cfg.TargetEmail != strings.TrimSpace(target.Email) || cfg.TargetName != target.AuthFile || cfg.TargetAuthIndex != target.AuthIndex || (strings.TrimSpace(target.AccountID) != "" && cfg.TargetAccountID != strings.TrimSpace(target.AccountID))
		cfg.TargetEmail, cfg.TargetName, cfg.TargetAuthIndex = strings.TrimSpace(target.Email), target.AuthFile, target.AuthIndex
		if strings.TrimSpace(target.AccountID) != "" {
			cfg.TargetAccountID = strings.TrimSpace(target.AccountID)
		} else if changed {
			cfg.TargetAccountID = ""
		}
		if changed {
			resetQuotaFloorObservation(&cfg)
		}
	}
	if cfg.Enabled && (strings.TrimSpace(cfg.TargetName) == "" || strings.TrimSpace(cfg.TargetAuthIndex) == "" || strings.TrimSpace(cfg.TargetEmail) == "") {
		return jsonResponse(http.StatusBadRequest, map[string]string{"error": "target_required"})
	}
	if body.ThresholdPercent != nil {
		cfg.ThresholdPercent = *body.ThresholdPercent
	}
	if body.PollSeconds != nil {
		cfg.PollSeconds = *body.PollSeconds
		quotaFloorTickInterval = time.Duration(*body.PollSeconds) * time.Second
	}
	if err := writeQuotaFloor(cfg); err != nil {
		return jsonResponse(http.StatusServiceUnavailable, map[string]string{"error": "quota_floor_unavailable"})
	}
	accounts, inventoryErr := quotaFloorListAccounts()
	if inventoryErr != nil {
		return jsonResponse(http.StatusServiceUnavailable, map[string]string{"error": "inventory_unavailable"})
	}
	return quotaFloorResponse(cfg, accounts)
}
