package main

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDailyIgniteSettingsPersistAndValidate(t *testing.T) {
	setupRepairRecoveryStore(t)
	oldNow := igniteScheduleNow
	now := time.Date(2026, 9, 8, 16, 2, 0, 0, time.UTC)
	igniteScheduleNow = func() time.Time { return now }
	t.Cleanup(func() { igniteScheduleNow = oldNow })
	get := func() igniteScheduleConfig {
		t.Helper()
		response := routeManagement(managementRequest{Method: "GET", Path: "/v0/management/plugins/cpa-phoenix/ignite/schedule"})
		var cfg igniteScheduleConfig
		if response.StatusCode != 200 || json.Unmarshal(response.Body, &cfg) != nil {
			t.Fatal("schedule GET failed")
		}
		return cfg
	}
	if cfg := get(); cfg.Enabled || cfg.Time != "09:00" || cfg.NextRun != 0 {
		t.Fatal("unexpected defaults")
	}
	post := func(body string) managementResponse {
		return routeManagement(managementRequest{Method: "POST", Path: "/v0/management/plugins/cpa-phoenix/ignite/schedule", Body: []byte(body)})
	}
	valid := `{"enabled":true,"time":"09:00","timezone":"America/Los_Angeles","acknowledge":"CPA_PHOENIX_ONE_CLICK"}`
	if response := post(valid); response.StatusCode != 200 {
		t.Fatalf("save failed: %s", response.Body)
	}
	want := time.Date(2026, 9, 9, 16, 0, 0, 0, time.UTC).Unix()
	if cfg := get(); !cfg.Enabled || cfg.NextRun != want {
		t.Fatalf("wrong next run: %+v", cfg)
	}
	closeStore()
	if cfg := get(); !cfg.Enabled || cfg.NextRun != want {
		t.Fatal("restart lost schedule")
	}
	for _, bad := range []string{
		`{"enabled":true,"time":"25:00","timezone":"America/Los_Angeles","acknowledge":"CPA_PHOENIX_ONE_CLICK"}`,
		`{"enabled":true,"time":"09:00","timezone":"Not/AZone","acknowledge":"CPA_PHOENIX_ONE_CLICK"}`,
		`{"enabled":true,"time":"09:00","timezone":"UTC"}`,
	} {
		if response := post(bad); response.StatusCode != 400 {
			t.Fatal("accepted invalid schedule")
		}
		if cfg := get(); cfg.NextRun != want {
			t.Fatal("bad save changed schedule")
		}
	}
	if response := post(`{"enabled":false,"time":"09:00","timezone":"America/Los_Angeles","acknowledge":"CPA_PHOENIX_ONE_CLICK"}`); response.StatusCode != 200 {
		t.Fatal("disable failed")
	}
	if cfg := get(); cfg.Enabled || cfg.NextRun != 0 {
		t.Fatal("disable retained a future run")
	}
}

func TestDailyIgniteDueWaitsThenCoalescesMissedDays(t *testing.T) {
	setupRepairRecoveryStore(t)
	now := time.Date(2026, 9, 8, 16, 2, 0, 0, time.UTC)
	cfg := igniteScheduleConfig{Enabled: true, Time: "09:00", Timezone: "America/Los_Angeles", NextRun: now.Add(-72 * time.Hour).Unix()}
	if err := writeIgniteScheduleConfig(cfg); err != nil {
		t.Fatal(err)
	}
	oldInventory := igniteListAccounts
	calls := 0
	igniteListAccounts = func() ([]account, error) { calls++; return nil, nil }
	t.Cleanup(func() { igniteListAccounts = oldInventory })
	id, err := globalJobs.start("revive", 1)
	if err != nil {
		t.Fatal(err)
	}
	runIgniteScheduleTickAt(now)
	busy, _ := readIgniteSchedule()
	if calls != 0 || busy.NextRun > now.Unix() {
		t.Fatal("busy schedule lost due run or queried inventory")
	}
	updateJob(id, "completed", "", 1)
	runIgniteScheduleTickAt(now)
	after, err := readIgniteSchedule()
	if err != nil || after.NextRun <= now.Unix() || after.LastOutcome != "skipped" || calls != 1 {
		t.Fatalf("missed run not coalesced: %+v calls=%d", after, calls)
	}
	runIgniteScheduleTickAt(now)
	closeStore()
	runIgniteScheduleTickAt(now)
	if calls != 1 {
		t.Fatal("same occurrence repeated after tick/restart")
	}
}
