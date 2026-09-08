package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultIgniteScheduleTime     = "09:00"
	defaultIgniteScheduleTimezone = "America/Los_Angeles"
)

var (
	igniteScheduleTickInterval = 30 * time.Second
	// This seam keeps schedule calculations deterministic in offline tests and
	// follows the plugin-wide clock used by management actions by default.
	igniteScheduleNow = func() time.Time { return timeNow() }
)

var (
	errInvalidIgniteSchedule = errors.New("invalid_schedule")
	errIgniteScheduleStore   = errors.New("schedule_store_unavailable")
)

// igniteScheduleConfig is the complete public schedule projection and the
// exact set of values persisted in the single ignite_schedule row.
type igniteScheduleConfig struct {
	Enabled     bool   `json:"enabled"`
	Time        string `json:"time"`
	Timezone    string `json:"timezone"`
	NextRun     int64  `json:"next_run"`
	LastRun     int64  `json:"last_run"`
	LastJobID   string `json:"last_job_id,omitempty"`
	LastOutcome string `json:"last_outcome"`
}

var igniteScheduleStoreMu sync.Mutex

func readIgniteSchedule() (igniteScheduleConfig, error) {
	db, err := openStore()
	if err != nil {
		return igniteScheduleConfig{}, err
	}
	_, _ = db.Exec(`INSERT INTO ignite_schedule(id,enabled,time,timezone,next_run,last_run,last_job_id,last_outcome) VALUES(1,0,?,?,0,0,'','') ON CONFLICT(id) DO NOTHING`, defaultIgniteScheduleTime, defaultIgniteScheduleTimezone)
	var enabled int
	var cfg igniteScheduleConfig
	err = db.QueryRow(`SELECT enabled,time,timezone,next_run,last_run,last_job_id,last_outcome FROM ignite_schedule WHERE id=1`).Scan(&enabled, &cfg.Time, &cfg.Timezone, &cfg.NextRun, &cfg.LastRun, &cfg.LastJobID, &cfg.LastOutcome)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return igniteScheduleConfig{}, errIgniteScheduleStore
		}
		return igniteScheduleConfig{}, err
	}
	cfg.Enabled = enabled != 0
	return cfg, nil
}

func writeIgniteScheduleConfig(cfg igniteScheduleConfig) error {
	db, err := openStore()
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE ignite_schedule SET enabled=?,time=?,timezone=?,next_run=?,last_run=?,last_job_id=?,last_outcome=? WHERE id=1`, boolInt(cfg.Enabled), cfg.Time, cfg.Timezone, cfg.NextRun, cfg.LastRun, cfg.LastJobID, cfg.LastOutcome)
	return err
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func parseIgniteScheduleTime(value string) (hour, minute int, err error) {
	if len(value) != len(defaultIgniteScheduleTime) {
		return 0, 0, errInvalidIgniteSchedule
	}
	parsed, parseErr := time.Parse("15:04", value)
	if parseErr != nil || parsed.Format("15:04") != value {
		return 0, 0, errInvalidIgniteSchedule
	}
	return parsed.Hour(), parsed.Minute(), nil
}

func validateIgniteSchedule(timeText, timezone string) error {
	if _, _, err := parseIgniteScheduleTime(timeText); err != nil {
		return err
	}
	if strings.TrimSpace(timezone) == "" {
		return errInvalidIgniteSchedule
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return errInvalidIgniteSchedule
	}
	return nil
}

// localIgniteScheduleTime constructs a requested civil time. Go normalizes a
// nonexistent spring-forward time backwards (for example 02:30 to 01:30),
// which would fire early. Shift that normalized value by the DST gap so the
// request runs at the corresponding post-transition wall time. Fall-back
// ambiguity intentionally resolves to the first occurrence, so one daily
// schedule cannot fire twice.
func localIgniteScheduleTime(date time.Time, hour, minute int, location *time.Location) time.Time {
	candidate := time.Date(date.Year(), date.Month(), date.Day(), hour, minute, 0, 0, location)
	local := candidate.In(location)
	if local.Year() == date.Year() && local.Month() == date.Month() && local.Day() == date.Day() && local.Hour() == hour && local.Minute() == minute {
		return candidate
	}
	_, beforeOffset := candidate.Zone()
	_, afterOffset := candidate.Add(4 * time.Hour).Zone()
	if gap := afterOffset - beforeOffset; gap > 0 {
		return candidate.Add(time.Duration(gap) * time.Second)
	}
	return candidate
}

func nextIgniteScheduleRun(cfg igniteScheduleConfig, now time.Time) int64 {
	location, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return 0
	}
	hour, minute, err := parseIgniteScheduleTime(cfg.Time)
	if err != nil {
		return 0
	}
	localNow := now.In(location)
	candidateDate := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
	candidate := localIgniteScheduleTime(candidateDate, hour, minute, location)
	if !candidate.After(now) {
		candidateDate = time.Date(localNow.Year(), localNow.Month(), localNow.Day()+1, 0, 0, 0, 0, location)
		candidate = localIgniteScheduleTime(candidateDate, hour, minute, location)
	}
	return candidate.Unix()
}

func scheduleIgniteUpdate(raw []byte) (igniteScheduleConfig, error) {
	var request struct {
		Enabled     *bool  `json:"enabled"`
		Time        string `json:"time"`
		Timezone    string `json:"timezone"`
		Acknowledge string `json:"acknowledge"`
	}
	if err := json.Unmarshal(raw, &request); err != nil || request.Enabled == nil {
		return igniteScheduleConfig{}, errInvalidIgniteSchedule
	}
	if request.Acknowledge != "CPA_PHOENIX_ONE_CLICK" {
		return igniteScheduleConfig{}, errors.New("acknowledgement_required")
	}
	request.Time = strings.TrimSpace(request.Time)
	request.Timezone = strings.TrimSpace(request.Timezone)
	if err := validateIgniteSchedule(request.Time, request.Timezone); err != nil {
		return igniteScheduleConfig{}, err
	}
	now := igniteScheduleNow()
	igniteScheduleStoreMu.Lock()
	defer igniteScheduleStoreMu.Unlock()
	cfg, err := readIgniteSchedule()
	if err != nil {
		return igniteScheduleConfig{}, err
	}
	cfg.Enabled = *request.Enabled
	cfg.Time = request.Time
	cfg.Timezone = request.Timezone
	if cfg.Enabled {
		cfg.NextRun = nextIgniteScheduleRun(cfg, now)
	} else {
		cfg.NextRun = 0
	}
	if err := writeIgniteScheduleConfig(cfg); err != nil {
		return igniteScheduleConfig{}, err
	}
	return cfg, nil
}

func igniteScheduleResponse(cfg igniteScheduleConfig) managementResponse {
	return jsonResponse(http.StatusOK, cfg)
}

func handleIgniteScheduleManagement(req managementRequest) managementResponse {
	switch req.Method {
	case http.MethodGet:
		igniteScheduleStoreMu.Lock()
		cfg, err := readIgniteSchedule()
		igniteScheduleStoreMu.Unlock()
		if err != nil {
			return jsonResponse(http.StatusServiceUnavailable, map[string]string{"error": "schedule_unavailable"})
		}
		return igniteScheduleResponse(cfg)
	case http.MethodPost:
		cfg, err := scheduleIgniteUpdate(req.Body)
		if err != nil {
			switch {
			case errors.Is(err, errInvalidIgniteSchedule):
				return jsonResponse(http.StatusBadRequest, map[string]string{"error": "invalid_schedule"})
			case err.Error() == "acknowledgement_required":
				return jsonResponse(http.StatusBadRequest, map[string]string{"error": "acknowledgement_required"})
			default:
				return jsonResponse(http.StatusServiceUnavailable, map[string]string{"error": "schedule_unavailable"})
			}
		}
		return igniteScheduleResponse(cfg)
	default:
		return jsonResponse(http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

// claimIgniteSchedule advances a due schedule before any account lookup or
// request. With the in-process mutex and SQLite update predicate, a restart
// cannot observe the same due timestamp and launch it again.
func claimIgniteSchedule(now time.Time) (igniteScheduleConfig, bool, error) {
	igniteScheduleStoreMu.Lock()
	defer igniteScheduleStoreMu.Unlock()
	cfg, err := readIgniteSchedule()
	if err != nil {
		return igniteScheduleConfig{}, false, err
	}
	if !cfg.Enabled || cfg.NextRun <= 0 || cfg.NextRun > now.Unix() {
		return cfg, false, nil
	}
	next := nextIgniteScheduleRun(cfg, now)
	if next <= 0 {
		return cfg, false, errInvalidIgniteSchedule
	}
	db, err := openStore()
	if err != nil {
		return cfg, false, err
	}
	result, err := db.Exec(`UPDATE ignite_schedule SET next_run=? WHERE id=1 AND enabled=1 AND next_run=?`, next, cfg.NextRun)
	if err != nil {
		return cfg, false, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return cfg, false, err
	}
	cfg.NextRun = next
	return cfg, true, nil
}

func updateIgniteScheduleRun(lastRun int64, jobID, outcome string, nextRun int64) {
	igniteScheduleStoreMu.Lock()
	defer igniteScheduleStoreMu.Unlock()
	cfg, err := readIgniteSchedule()
	if err != nil {
		return
	}
	cfg.LastRun = lastRun
	cfg.LastJobID = strings.TrimSpace(jobID)
	cfg.LastOutcome = outcome
	if nextRun >= 0 {
		cfg.NextRun = nextRun
	}
	_ = writeIgniteScheduleConfig(cfg)
}

func markIgniteScheduleStarted(jobID string, lastRun int64) {
	updateIgniteScheduleRun(lastRun, jobID, "running", -1)
}

func finishIgniteSchedule(jobID, outcome string) {
	igniteScheduleStoreMu.Lock()
	defer igniteScheduleStoreMu.Unlock()
	cfg, err := readIgniteSchedule()
	if err != nil || cfg.LastJobID != jobID {
		return
	}
	cfg.LastOutcome = outcome
	_ = writeIgniteScheduleConfig(cfg)
}

func retryIgniteSchedule(now time.Time, claimed ...igniteScheduleConfig) {
	next := now.Unix()
	igniteScheduleStoreMu.Lock()
	defer igniteScheduleStoreMu.Unlock()
	cfg, err := readIgniteSchedule()
	if err != nil || !cfg.Enabled || cfg.NextRun <= 0 {
		return
	}
	// Preserve a concurrent save/disable. The caller leaves the due instant
	// in this row when it can observe a busy global action; the next tick then
	// coalesces that still-due attempt once the action gate is free.
	db, err := openStore()
	if err != nil {
		return
	}
	if len(claimed) > 0 {
		// claimIgniteSchedule has already advanced next_run. Restore that due
		// instant only if the schedule row still has the same configuration;
		// a concurrent POST therefore remains authoritative.
		claimedCfg := claimed[0]
		_, _ = db.Exec(`UPDATE ignite_schedule SET next_run=?,last_run=?,last_job_id='',last_outcome='busy' WHERE id=1 AND enabled=1 AND next_run=? AND time=? AND timezone=?`, next, next, claimedCfg.NextRun, claimedCfg.Time, claimedCfg.Timezone)
		return
	}
	_, _ = db.Exec(`UPDATE ignite_schedule SET last_run=?,last_job_id='',last_outcome='busy' WHERE id=1 AND enabled=1 AND next_run<=?`, next, next)
}

func runIgniteScheduleTickAt(now time.Time) {
	// Avoid a quota refresh when another management action already owns the
	// global gate. Leaving next_run due is the durable deferred attempt.
	igniteScheduleStoreMu.Lock()
	cfg, readErr := readIgniteSchedule()
	igniteScheduleStoreMu.Unlock()
	if readErr != nil || !cfg.Enabled || cfg.NextRun <= 0 || cfg.NextRun > now.Unix() {
		return
	}
	if anyJobActive() {
		retryIgniteSchedule(now)
		return
	}
	cfg, due, err := claimIgniteSchedule(now)
	if err != nil || !due {
		return
	}
	result, err := beginIgnite(true)
	if err != nil {
		switch {
		case errors.Is(err, errNoActionableAccounts):
			updateIgniteScheduleRun(now.Unix(), "", "skipped", -1)
		case errors.Is(err, errIgniteJobActive):
			// An action can start between the inexpensive precheck and the
			// durable claim. Restore the due attempt so it is retried after that
			// action releases the global gate.
			retryIgniteSchedule(now, cfg)
		default:
			updateIgniteScheduleRun(now.Unix(), "", "failed", -1)
		}
		return
	}
	_ = result
}

func runIgniteScheduleTick() {
	runIgniteScheduleTickAt(igniteScheduleNow())
}

type igniteSchedulerRuntime struct {
	mu      sync.Mutex
	cancel  func()
	done    chan struct{}
	running bool
}

var globalIgniteScheduler = &igniteSchedulerRuntime{}

func startIgniteScheduler() {
	globalIgniteScheduler.mu.Lock()
	if globalIgniteScheduler.running {
		globalIgniteScheduler.mu.Unlock()
		return
	}
	cancelSignal := make(chan struct{})
	done := make(chan struct{})
	globalIgniteScheduler.cancel = func() { close(cancelSignal) }
	globalIgniteScheduler.done = done
	globalIgniteScheduler.running = true
	globalIgniteScheduler.mu.Unlock()
	go func() {
		defer close(done)
		interval := igniteScheduleTickInterval
		if interval <= 0 {
			interval = 30 * time.Second
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		runIgniteScheduleTick()
		for {
			select {
			case <-ticker.C:
				runIgniteScheduleTick()
			case <-cancelSignal:
				return
			}
		}
	}()
}

func stopIgniteScheduler() {
	globalIgniteScheduler.mu.Lock()
	if !globalIgniteScheduler.running {
		globalIgniteScheduler.mu.Unlock()
		return
	}
	cancel := globalIgniteScheduler.cancel
	done := globalIgniteScheduler.done
	globalIgniteScheduler.cancel = nil
	globalIgniteScheduler.done = nil
	globalIgniteScheduler.running = false
	globalIgniteScheduler.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}
