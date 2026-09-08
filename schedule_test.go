package main

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func scheduleFixtureStore(t *testing.T) {
	t.Helper()
	oldPath := storePath
	storePath = filepath.Join(t.TempDir(), "state.db")
	closeStore()
	stopIgniteScheduler()
	t.Cleanup(func() {
		stopIgniteScheduler()
		globalJobs.stop()
		closeStore()
		storePath = oldPath
	})
}

func scheduleRequestBody(t *testing.T, enabled bool, clock, timezone string) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"enabled": enabled, "time": clock, "timezone": timezone, "acknowledge": "CPA_PHOENIX_ONE_CLICK"})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestIgniteScheduleDefaultsAndAuthenticatedUpdate(t *testing.T) {
	scheduleFixtureStore(t)
	now := time.Date(2026, 9, 8, 16, 2, 0, 0, time.FixedZone("PDT", -7*60*60))
	oldNow := igniteScheduleNow
	igniteScheduleNow = func() time.Time { return now }
	t.Cleanup(func() { igniteScheduleNow = oldNow })

	response := routeManagement(managementRequest{Method: http.MethodGet, Path: "/v0/management/plugins/cpa-phoenix/ignite/schedule"})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("default GET status=%d", response.StatusCode)
	}
	var defaults igniteScheduleConfig
	if err := json.Unmarshal(response.Body, &defaults); err != nil {
		t.Fatal(err)
	}
	if defaults.Enabled || defaults.Time != defaultIgniteScheduleTime || defaults.Timezone != defaultIgniteScheduleTimezone || defaults.NextRun != 0 || defaults.LastRun != 0 {
		t.Fatalf("defaults=%+v", defaults)
	}

	response = routeManagement(managementRequest{Method: http.MethodPost, Path: "/v0/management/plugins/cpa-phoenix/ignite/schedule", Body: scheduleRequestBody(t, true, "09:00", "America/Los_Angeles")})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("update status=%d body=%s", response.StatusCode, response.Body)
	}
	if err := json.Unmarshal(response.Body, &defaults); err != nil {
		t.Fatal(err)
	}
	// 09:00 PDT already passed, so enabling it schedules the next local day.
	want := time.Date(2026, 9, 9, 9, 0, 0, 0, time.FixedZone("PDT", -7*60*60)).Unix()
	if !defaults.Enabled || defaults.NextRun != want || defaults.LastOutcome != "" {
		t.Fatalf("enabled config=%+v want next=%d", defaults, want)
	}

	response = routeManagement(managementRequest{Method: http.MethodPost, Path: "/v0/management/plugins/cpa-phoenix/ignite/schedule", Body: scheduleRequestBody(t, false, "09:00", "America/Los_Angeles")})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("disable status=%d body=%s", response.StatusCode, response.Body)
	}
	if err := json.Unmarshal(response.Body, &defaults); err != nil {
		t.Fatal(err)
	}
	if defaults.Enabled || defaults.NextRun != 0 {
		t.Fatalf("disabled config=%+v", defaults)
	}

	response = routeManagement(managementRequest{Method: http.MethodPost, Path: "/v0/management/plugins/cpa-phoenix/ignite/schedule", Body: []byte(`{"enabled":true,"time":"09:00","timezone":"America/Los_Angeles"}`)})
	if response.StatusCode != http.StatusBadRequest || string(response.Body) != `{"error":"acknowledgement_required"}` {
		t.Fatalf("missing acknowledgement status=%d body=%s", response.StatusCode, response.Body)
	}
}

func TestIgniteScheduleHandlesDSTWallTimes(t *testing.T) {
	zone, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	cfg := igniteScheduleConfig{Enabled: true, Time: "02:30", Timezone: "America/Los_Angeles"}
	// 02:30 does not exist on spring-forward day. It is shifted by the gap to
	// 03:30 PDT instead of being normalized backwards to 01:30 PST.
	spring := time.Date(2024, 3, 10, 1, 0, 0, 0, zone)
	wantSpring := time.Date(2024, 3, 10, 3, 30, 0, 0, zone).Unix()
	if got := nextIgniteScheduleRun(cfg, spring); got != wantSpring {
		t.Fatalf("spring next=%d want=%d", got, wantSpring)
	}
	// Fall-back's repeated 01:30 is selected once, using the first occurrence.
	cfg.Time = "01:30"
	fall := time.Date(2024, 11, 3, 0, 30, 0, 0, zone)
	wantFall := time.Date(2024, 11, 3, 1, 30, 0, 0, zone).Unix()
	if got := nextIgniteScheduleRun(cfg, fall); got != wantFall {
		t.Fatalf("fall next=%d want=%d", got, wantFall)
	}
	// Once the second 01:30 has begun (09:30 UTC), the first occurrence is
	// already past and the schedule advances exactly one local day.
	afterFall := time.Unix(time.Date(2024, 11, 3, 9, 30, 0, 0, time.UTC).Unix(), 0)
	wantNextDay := time.Date(2024, 11, 4, 1, 30, 0, 0, zone).Unix()
	if got := nextIgniteScheduleRun(cfg, afterFall); got != wantNextDay {
		t.Fatalf("after fall next=%d want=%d", got, wantNextDay)
	}
}

func TestIgniteScheduleCoalescesMissedNoActionableRunWithoutJob(t *testing.T) {
	scheduleFixtureStore(t)
	now := time.Date(2026, 9, 8, 17, 0, 0, 0, time.UTC)
	oldInventory := igniteListAccounts
	igniteListAccounts = func() ([]account, error) { return nil, nil }
	t.Cleanup(func() { igniteListAccounts = oldInventory })
	cfg := igniteScheduleConfig{Enabled: true, Time: "09:00", Timezone: "UTC", NextRun: now.Add(-48 * time.Hour).Unix()}
	if err := writeIgniteScheduleConfig(cfg); err != nil {
		t.Fatal(err)
	}
	runIgniteScheduleTickAt(now)
	got, err := readIgniteSchedule()
	if err != nil {
		t.Fatal(err)
	}
	if got.LastOutcome != "skipped" || got.LastRun != now.Unix() || got.LastJobID != "" || got.NextRun <= now.Unix() {
		t.Fatalf("coalesced schedule=%+v", got)
	}
	var jobs int
	db, _ := openStore()
	if err := db.QueryRow(`SELECT COUNT(*) FROM jobs`).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if jobs != 0 {
		t.Fatalf("no-actionable schedule created %d jobs", jobs)
	}
}

func TestIgniteScheduleDefersWhenGlobalJobIsBusy(t *testing.T) {
	scheduleFixtureStore(t)
	now := time.Date(2026, 9, 8, 17, 0, 0, 0, time.UTC)
	db, err := openStore()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE ignite_schedule SET enabled=1,time='09:00',timezone='UTC',next_run=?,last_run=0,last_job_id='',last_outcome='' WHERE id=1`, now.Add(-time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	busyID, err := globalJobs.start("revive", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer globalJobs.stopIf(busyID)
	runIgniteScheduleTickAt(now)
	cfg, err := readIgniteSchedule()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LastOutcome != "busy" || cfg.NextRun > now.Unix() {
		t.Fatalf("busy schedule lost due=%+v", cfg)
	}
}

func TestIgniteScheduleLateBusyRestoreHonorsConcurrentSave(t *testing.T) {
	scheduleFixtureStore(t)
	now := time.Date(2026, 9, 8, 17, 0, 0, 0, time.UTC)
	cfg := igniteScheduleConfig{Enabled: true, Time: "09:00", Timezone: "UTC", NextRun: now.Add(-time.Hour).Unix()}
	if err := writeIgniteScheduleConfig(cfg); err != nil {
		t.Fatal(err)
	}
	claimed, due, err := claimIgniteSchedule(now)
	if err != nil || !due || claimed.NextRun <= now.Unix() {
		t.Fatalf("claim=%+v due=%v err=%v", claimed, due, err)
	}
	// A dashboard save wins over a late busy result and must not be reverted.
	saved := claimed
	saved.Enabled = false
	saved.Time = "10:00"
	saved.NextRun = 0
	if err := writeIgniteScheduleConfig(saved); err != nil {
		t.Fatal(err)
	}
	retryIgniteSchedule(now, claimed)
	got, err := readIgniteSchedule()
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled || got.Time != "10:00" || got.NextRun != 0 {
		t.Fatalf("concurrent save was overwritten: %+v", got)
	}

	// With no concurrent save, the same late busy result restores a due row.
	saved.Enabled = true
	saved.Time = "09:00"
	saved.NextRun = now.Add(-time.Hour).Unix()
	saved.LastOutcome = ""
	if err := writeIgniteScheduleConfig(saved); err != nil {
		t.Fatal(err)
	}
	claimed, due, err = claimIgniteSchedule(now)
	if err != nil || !due {
		t.Fatalf("second claim due=%v err=%v", due, err)
	}
	retryIgniteSchedule(now, claimed)
	got, err = readIgniteSchedule()
	if err != nil || got.NextRun > now.Unix() || got.LastOutcome != "busy" {
		t.Fatalf("late busy did not restore due row: %+v err=%v", got, err)
	}
}

func TestScheduledIgniteUsesSharedWorkerAndPublishesOutcome(t *testing.T) {
	scheduleFixtureStore(t)
	now := time.Date(2026, 9, 8, 17, 0, 0, 0, time.UTC)
	oldNow := timeNow
	timeNow = func() time.Time { return now }
	t.Cleanup(func() { timeNow = oldNow })
	zero, full := float64(0), int64(3600)
	reset := now.Unix() + full
	fixture := account{Key: "scheduled-seat", AuthIndex: "scheduled", Physical: true, AccessTokenValue: "fixture-token", Quota: quotaSnapshot{Windows: []quotaWindow{{Presence: windowPresent, UsedPercent: &zero, ResetAt: &reset, LimitWindowSeconds: &full, ResetAfterSeconds: &full}}}}
	oldInventory := igniteListAccounts
	igniteListAccounts = func() ([]account, error) { return []account{fixture}, nil }
	t.Cleanup(func() { igniteListAccounts = oldInventory })
	oldRequest := upstreamRequest
	calls := 0
	upstreamRequest = func(context.Context, account, []byte) (int, error) {
		calls++
		return http.StatusOK, nil
	}
	t.Cleanup(func() { upstreamRequest = oldRequest })

	db, err := openStore()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE ignite_schedule SET enabled=1,time='09:00',timezone='UTC',next_run=?,last_run=0,last_job_id='',last_outcome='' WHERE id=1`, now.Add(-time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	runIgniteScheduleTickAt(now)
	deadline := time.Now().Add(2 * time.Second)
	var cfg igniteScheduleConfig
	for {
		cfg, err = readIgniteSchedule()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.LastOutcome == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("scheduled worker did not finish: %+v", cfg)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if cfg.LastJobID == "" || cfg.LastRun != now.Unix() || cfg.NextRun <= now.Unix() {
		t.Fatalf("scheduled worker projection=%+v", cfg)
	}
	if calls != 1 {
		t.Fatalf("shared worker dispatched %d requests, want 1", calls)
	}
	job, err := readJob(cfg.LastJobID)
	if err != nil || job.Kind != "ignite" || job.State != "completed" {
		t.Fatalf("shared worker job=%+v err=%v", job, err)
	}
}

func TestIgniteSchedulerLifecycleIsIdempotent(t *testing.T) {
	scheduleFixtureStore(t)
	oldInterval := igniteScheduleTickInterval
	igniteScheduleTickInterval = time.Hour
	t.Cleanup(func() { igniteScheduleTickInterval = oldInterval })
	startIgniteScheduler()
	startIgniteScheduler()
	globalIgniteScheduler.mu.Lock()
	running := globalIgniteScheduler.running
	globalIgniteScheduler.mu.Unlock()
	if !running {
		t.Fatal("scheduler did not start")
	}
	stopIgniteScheduler()
	globalIgniteScheduler.mu.Lock()
	running = globalIgniteScheduler.running
	globalIgniteScheduler.mu.Unlock()
	if running {
		t.Fatal("scheduler did not stop")
	}
}
