package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var storeMu sync.Mutex
var storeDB *sql.DB
var storePath = ""

// Legacy awaiting_user rows were written before Phoenix persisted a private
// quarantine marker. The state is authoritative because Phoenix only enters
// awaiting_user after the predecessor has been moved; this opaque marker
// lets restart/resume preserve that decision without reconstructing a path.
const legacyQuarantineMarker = "confirmed"

func openStore() (*sql.DB, error) {
	storeMu.Lock()
	defer storeMu.Unlock()
	if storeDB != nil {
		return storeDB, nil
	}
	path := storePath
	if path == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(dir, "cli-proxy-api", "plugins", "cpa-phoenix", "state.db")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}
	if _, err = db.Exec(`PRAGMA busy_timeout=1500; PRAGMA journal_mode=WAL;`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err = db.Exec(`
CREATE TABLE IF NOT EXISTS activation_cycles (
 account_key TEXT NOT NULL,
 cycle_key TEXT NOT NULL,
 run_id TEXT NOT NULL DEFAULT '',
 status TEXT NOT NULL,
 reserved_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL,
 next_cycle_after INTEGER NOT NULL DEFAULT 0,
 active_observed_at INTEGER NOT NULL DEFAULT 0,
 refresh_observed_at INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(account_key,cycle_key)
);
CREATE TABLE IF NOT EXISTS jobs (
 id TEXT PRIMARY KEY,
 kind TEXT NOT NULL,
 state TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL,
 total INTEGER NOT NULL DEFAULT 0,
 done INTEGER NOT NULL DEFAULT 0,
 reason TEXT NOT NULL DEFAULT '',
 result_json TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS repair_rows (
 job_id TEXT NOT NULL,
 ordinal INTEGER NOT NULL,
 account_key TEXT NOT NULL,
 auth_id TEXT NOT NULL DEFAULT '',
 auth_index TEXT NOT NULL DEFAULT '',
 email TEXT NOT NULL DEFAULT '',
 account_id TEXT NOT NULL DEFAULT '',
	workspace_name TEXT NOT NULL DEFAULT '',
 auth_path TEXT NOT NULL DEFAULT '',
 auth_dir TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL,
 reason TEXT NOT NULL DEFAULT '',
 quarantine TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(job_id,ordinal)
);
CREATE TABLE IF NOT EXISTS ignite_schedule (
 id INTEGER PRIMARY KEY CHECK(id=1),
 enabled INTEGER NOT NULL DEFAULT 0,
 time TEXT NOT NULL DEFAULT '09:00',
 timezone TEXT NOT NULL DEFAULT 'America/Los_Angeles',
 next_run INTEGER NOT NULL DEFAULT 0,
 last_run INTEGER NOT NULL DEFAULT 0,
 last_job_id TEXT NOT NULL DEFAULT '',
 last_outcome TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS quota_floor (
 id INTEGER PRIMARY KEY CHECK(id=1),
 enabled INTEGER NOT NULL DEFAULT 0,
 target_email TEXT NOT NULL DEFAULT '',
 threshold_percent REAL NOT NULL DEFAULT 95,
 poll_seconds INTEGER NOT NULL DEFAULT 30,
 target_name TEXT NOT NULL DEFAULT '',
 target_auth_index TEXT NOT NULL DEFAULT '',
 target_account_id TEXT NOT NULL DEFAULT '',
 controller_owned INTEGER NOT NULL DEFAULT 0,
 last_status TEXT NOT NULL DEFAULT 'unknown',
 last_observed_at INTEGER NOT NULL DEFAULT 0,
 last_primary_percent REAL,
 last_secondary_percent REAL,
 last_primary_reset_at INTEGER NOT NULL DEFAULT 0,
 last_secondary_reset_at INTEGER NOT NULL DEFAULT 0,
 blocked_primary_reset_at INTEGER NOT NULL DEFAULT 0,
 blocked_secondary_reset_at INTEGER NOT NULL DEFAULT 0,
 last_error TEXT NOT NULL DEFAULT ''
);`); err != nil {
		db.Close()
		return nil, err
	}
	// Additive migration for databases created by early Phoenix builds.
	_, _ = db.Exec(`ALTER TABLE activation_cycles ADD COLUMN run_id TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE activation_cycles ADD COLUMN next_cycle_after INTEGER NOT NULL DEFAULT 0`)
	_, _ = db.Exec(`ALTER TABLE activation_cycles ADD COLUMN active_observed_at INTEGER NOT NULL DEFAULT 0`)
	_, _ = db.Exec(`ALTER TABLE activation_cycles ADD COLUMN refresh_observed_at INTEGER NOT NULL DEFAULT 0`)
	_, _ = db.Exec(`ALTER TABLE jobs ADD COLUMN result_json TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE repair_rows ADD COLUMN auth_id TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE repair_rows ADD COLUMN auth_index TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE repair_rows ADD COLUMN email TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE repair_rows ADD COLUMN account_id TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE repair_rows ADD COLUMN workspace_name TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE repair_rows ADD COLUMN auth_path TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE repair_rows ADD COLUMN auth_dir TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE repair_rows ADD COLUMN quarantine TEXT NOT NULL DEFAULT ''`)
	// Older quota-floor tables may have been created with only the singleton
	// key and enabled flag. Add every later column independently so opening an
	// old store never fails its read projection or silently loses protection
	// state. Duplicate-column errors are expected on fresh/current tables;
	// every other migration error makes the store unavailable rather than
	// allowing a stale enabled controller to run against a partial schema.
	quotaColumns := []string{
		`ALTER TABLE quota_floor ADD COLUMN target_email TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE quota_floor ADD COLUMN threshold_percent REAL NOT NULL DEFAULT 95`,
		`ALTER TABLE quota_floor ADD COLUMN poll_seconds INTEGER NOT NULL DEFAULT 30`,
		`ALTER TABLE quota_floor ADD COLUMN target_name TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE quota_floor ADD COLUMN target_auth_index TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE quota_floor ADD COLUMN target_account_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE quota_floor ADD COLUMN controller_owned INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE quota_floor ADD COLUMN last_status TEXT NOT NULL DEFAULT 'unknown'`,
		`ALTER TABLE quota_floor ADD COLUMN last_observed_at INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE quota_floor ADD COLUMN last_primary_percent REAL`,
		`ALTER TABLE quota_floor ADD COLUMN last_secondary_percent REAL`,
		`ALTER TABLE quota_floor ADD COLUMN last_primary_reset_at INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE quota_floor ADD COLUMN last_secondary_reset_at INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE quota_floor ADD COLUMN blocked_primary_reset_at INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE quota_floor ADD COLUMN blocked_secondary_reset_at INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE quota_floor ADD COLUMN last_error TEXT NOT NULL DEFAULT ''`,
	}
	for _, statement := range quotaColumns {
		if _, err := db.Exec(statement); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
			db.Close()
			return nil, err
		}
	}
	// The scheduler has exactly one persisted configuration. Keep its row
	// present so GET has stable defaults even before the first dashboard save.
	_, _ = db.Exec(`INSERT INTO ignite_schedule(id,enabled,time,timezone,next_run,last_run,last_job_id,last_outcome) VALUES(1,0,'09:00','America/Los_Angeles',0,0,'','') ON CONFLICT(id) DO NOTHING`)
	if _, err := db.Exec(`INSERT INTO quota_floor(id,enabled,target_email,threshold_percent,poll_seconds,target_name,target_auth_index,target_account_id,controller_owned,last_status,last_observed_at,last_primary_reset_at,last_secondary_reset_at,last_error) VALUES(1,0,'',95,30,'','','',0,'unknown',0,0,0,'') ON CONFLICT(id) DO NOTHING`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`UPDATE quota_floor SET enabled=COALESCE(enabled,0),target_email=COALESCE(target_email,''),threshold_percent=COALESCE(threshold_percent,95),poll_seconds=COALESCE(poll_seconds,30),target_name=COALESCE(target_name,''),target_auth_index=COALESCE(target_auth_index,''),target_account_id=COALESCE(target_account_id,''),controller_owned=COALESCE(controller_owned,0),last_status=COALESCE(last_status,'unknown'),last_observed_at=COALESCE(last_observed_at,0),last_primary_reset_at=COALESCE(last_primary_reset_at,0),last_secondary_reset_at=COALESCE(last_secondary_reset_at,0),blocked_primary_reset_at=COALESCE(blocked_primary_reset_at,0),blocked_secondary_reset_at=COALESCE(blocked_secondary_reset_at,0),last_error=COALESCE(last_error,'') WHERE id=1`); err != nil {
		db.Close()
		return nil, err
	}
	// Rows created by the old implicit personal-email default were never
	// explicitly bound to a physical seat. Convert every such unbound row;
	// deliberate exact-seat configurations are preserved. NULL projection values
	// from older schemas are treated as their empty/zero defaults by COALESCE.
	if _, err := db.Exec(`UPDATE quota_floor SET enabled=0,target_email='',target_account_id='',poll_seconds=30 WHERE id=1 AND enabled=1 AND target_email='lifan.builds@gmail.com' AND COALESCE(target_name,'')='' AND COALESCE(target_auth_index,'')='' AND COALESCE(target_account_id,'')='' AND COALESCE(controller_owned,0)=0 AND COALESCE(last_observed_at,0)=0 AND COALESCE(last_primary_reset_at,0)=0 AND COALESCE(last_secondary_reset_at,0)=0 AND COALESCE(blocked_primary_reset_at,0)=0 AND COALESCE(blocked_secondary_reset_at,0)=0`); err != nil {
		db.Close()
		return nil, err
	}
	// Older Phoenix builds briefly persisted absolute auth paths to resume a
	// repair. Paths are runtime-only now; queued rows resolve their current
	// exact file from host.auth.list and quarantined rows no longer need the
	// predecessor path. Clear any legacy values on the additive open path.
	_, _ = db.Exec(`UPDATE repair_rows SET auth_path='',auth_dir='' WHERE auth_path<>'' OR auth_dir<>''`)
	// Rows from the early guided-repair build could be awaiting user with no
	// persisted marker. Preserve their already-quarantined meaning across a
	// restart using a fixed opaque value rather than guessing or exposing a path.
	_, _ = db.Exec(`UPDATE repair_rows SET quarantine=? WHERE state='awaiting_user' AND quarantine=''`, legacyQuarantineMarker)
	// A process crash cannot safely resume an in-flight send or OAuth session.
	// Release stale gates durably without retrying network work: dispatch intent
	// is ambiguous, Ignite jobs are terminal, and Revive remains explicitly
	// resumable through its failed-row recovery path.
	_, _ = db.Exec(`UPDATE activation_cycles SET status='sent_unknown',updated_at=? WHERE status='dispatch_intent'`, time.Now().Unix())
	_, _ = db.Exec(`UPDATE jobs SET state='completed',reason='interrupted',updated_at=? WHERE kind='ignite' AND state IN ('running','awaiting_user')`, time.Now().Unix())
	_, _ = db.Exec(`UPDATE jobs SET state='failed',reason='interrupted',updated_at=? WHERE kind='revive' AND state IN ('running','awaiting_user')`, time.Now().Unix())
	// A process can stop after the scheduler records a job ID but before the
	// worker publishes its terminal outcome. Reconcile that projection with the
	// durable job cleanup above so a restart never leaves "running" forever.
	_, _ = db.Exec(`UPDATE ignite_schedule SET last_outcome='interrupted' WHERE id=1 AND last_outcome='running' AND last_job_id<>'' AND NOT EXISTS (SELECT 1 FROM jobs WHERE jobs.id=ignite_schedule.last_job_id AND jobs.state IN ('running','awaiting_user'))`)
	_ = os.Chmod(path, 0600)
	storeDB = db
	return db, nil
}

func closeStore() {
	storeMu.Lock()
	defer storeMu.Unlock()
	if storeDB != nil {
		_ = storeDB.Close()
		storeDB = nil
	}
}

func reserveCycle(accountKey, cycleKey string) (bool, error) {
	db, err := openStore()
	if err != nil {
		return false, err
	}
	now := time.Now().Unix()
	runID := time.Now().UTC().Format("20060102T150405.000000000Z")
	result, err := db.Exec(`
INSERT INTO activation_cycles(account_key,cycle_key,run_id,status,reserved_at,updated_at)
VALUES(?,?,?,'dispatch_intent',?,?)
ON CONFLICT(account_key,cycle_key) DO UPDATE SET
 run_id=excluded.run_id,status='dispatch_intent',reserved_at=excluded.reserved_at,updated_at=excluded.updated_at
WHERE activation_cycles.status IN ('failed_before_send','successor_available')`, accountKey, cycleKey, runID, now, now)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}

type cycleRecord struct {
	Key             string
	Status          string
	ReservedAt      int64
	NextCycleAfter  int64
	ActiveObserved  int64
	RefreshObserved int64
}

func readCycle(accountKey, cycleKey string) (cycleRecord, error) {
	db, err := openStore()
	if err != nil {
		return cycleRecord{}, err
	}
	var r cycleRecord
	r.Key = cycleKey
	err = db.QueryRow(`SELECT status,reserved_at,next_cycle_after,active_observed_at,refresh_observed_at FROM activation_cycles WHERE account_key=? AND cycle_key=?`, accountKey, cycleKey).Scan(&r.Status, &r.ReservedAt, &r.NextCycleAfter, &r.ActiveObserved, &r.RefreshObserved)
	return r, err
}

func latestCycle(accountKey string) (cycleRecord, error) {
	db, err := openStore()
	if err != nil {
		return cycleRecord{}, err
	}
	var r cycleRecord
	err = db.QueryRow(`SELECT cycle_key,status,reserved_at,next_cycle_after,active_observed_at,refresh_observed_at FROM activation_cycles WHERE account_key=? ORDER BY reserved_at DESC,updated_at DESC LIMIT 1`, accountKey).Scan(&r.Key, &r.Status, &r.ReservedAt, &r.NextCycleAfter, &r.ActiveObserved, &r.RefreshObserved)
	return r, err
}

func updateCycleObservation(accountKey, cycleKey string, active, refresh int64) error {
	db, err := openStore()
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE activation_cycles SET active_observed_at=CASE WHEN active_observed_at=0 THEN ? ELSE active_observed_at END, refresh_observed_at=CASE WHEN refresh_observed_at=0 THEN ? ELSE refresh_observed_at END, updated_at=? WHERE account_key=? AND cycle_key=?`, active, refresh, time.Now().Unix(), accountKey, cycleKey)
	return err
}

func createSuccessorCycle(accountKey, cycleKey string, refreshAt int64, successor string) error {
	db, err := openStore()
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	_, err = db.Exec(`UPDATE activation_cycles SET refresh_observed_at=CASE WHEN refresh_observed_at=0 THEN ? ELSE refresh_observed_at END,updated_at=? WHERE account_key=? AND cycle_key=?`, refreshAt, now, accountKey, cycleKey)
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO activation_cycles(account_key,cycle_key,run_id,status,reserved_at,updated_at) VALUES(?,?,?,'successor_available',?,?) ON CONFLICT(account_key,cycle_key) DO NOTHING`, accountKey, successor, "", refreshAt, now)
	return err
}

func createBoundarySuccessor(accountKey, cycleKey string, boundary int64, successor string) error {
	db, err := openStore()
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	_, err = db.Exec(`INSERT INTO activation_cycles(account_key,cycle_key,run_id,status,reserved_at,updated_at) VALUES(?,?,?,'successor_available',?,?) ON CONFLICT(account_key,cycle_key) DO NOTHING`, accountKey, successor, "", boundary, now)
	return err
}

func setCycleBoundary(accountKey, cycleKey string, boundary int64) error {
	if boundary <= 0 {
		return nil
	}
	db, err := openStore()
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE activation_cycles SET next_cycle_after=CASE WHEN next_cycle_after=0 THEN ? ELSE next_cycle_after END,updated_at=? WHERE account_key=? AND cycle_key=?`, boundary, time.Now().Unix(), accountKey, cycleKey)
	return err
}

// cycleBlocked reports whether a fresh cycle already owns a dispatch intent
// or reached any terminal/ambiguous outcome. Only a durable pre-send failure
// is reusable; this mirrors reserveCycle's conditional retry boundary.
func cycleBlocked(accountKey, cycleKey string) (bool, error) {
	db, err := openStore()
	if err != nil {
		return false, err
	}
	var status string
	err = db.QueryRow(`SELECT status FROM activation_cycles WHERE account_key=? AND cycle_key=?`, accountKey, cycleKey).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return status != "failed_before_send" && status != "successor_available", nil
}

func updateCycle(accountKey, cycleKey, status string) error {
	db, err := openStore()
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE activation_cycles SET status=?,updated_at=? WHERE account_key=? AND cycle_key=?`, status, time.Now().Unix(), accountKey, cycleKey)
	return err
}

type jobManager struct {
	mu     sync.Mutex
	active string
	ctx    context.Context
	cancel context.CancelFunc
}

var globalJobs = &jobManager{}

// anyJobActive fails closed when durable state cannot be read. The dashboard
// uses this projection only to keep consequential buttons disabled; an
// unreadable state store must never make a concurrent action appear safe.
func anyJobActive() bool {
	globalJobs.mu.Lock()
	inMemory := globalJobs.active != ""
	globalJobs.mu.Unlock()
	if inMemory {
		return true
	}
	db, err := openStore()
	if err != nil {
		return true
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE state IN ('running','awaiting_user')`).Scan(&count); err != nil {
		return true
	}
	return count > 0
}

func (m *jobManager) stop() {
	m.mu.Lock()
	id, cancel := m.active, m.cancel
	m.active, m.ctx, m.cancel = "", nil, nil
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if id != "" {
		if db, err := openStore(); err == nil {
			_, _ = db.Exec(`UPDATE jobs SET state='cancelled',reason='cancelled',updated_at=? WHERE id=? AND state IN ('running','awaiting_user')`, time.Now().Unix(), id)
		}
	}
}

// stopIf cancels only the exact in-memory job ID. A management action for a
// stale/foreign job must never stop a different active action.
func (m *jobManager) stopIf(id string) bool {
	m.mu.Lock()
	if strings.TrimSpace(id) == "" || m.active != id {
		m.mu.Unlock()
		return false
	}
	cancel := m.cancel
	m.active, m.ctx, m.cancel = "", nil, nil
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if db, err := openStore(); err == nil {
		_, _ = db.Exec(`UPDATE jobs SET state='cancelled',reason='cancelled',updated_at=? WHERE id=? AND state IN ('running','awaiting_user')`, time.Now().Unix(), id)
	}
	return true
}

func (m *jobManager) start(kind string, total int) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != "" {
		return "", errIgniteJobActive
	}
	db, err := openStore()
	if err != nil {
		return "", err
	}
	var existing int
	if err := db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE state IN ('running','awaiting_user')`).Scan(&existing); err != nil {
		return "", err
	}
	if existing > 0 {
		return "", errIgniteJobActive
	}
	id := time.Now().UTC().Format("20060102T150405.000000000Z")
	now := time.Now().Unix()
	if _, err = db.Exec(`INSERT INTO jobs(id,kind,state,created_at,updated_at,total) VALUES(?,?,?,?,?,?)`, id, kind, "running", now, now, total); err != nil {
		return "", err
	}
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.active = id
	return id, nil
}

// startRevive atomically persists a fresh Revive snapshot. When supersedeID is
// set, the terminal predecessor is transitioned in the same transaction as
// the new job and its rows. Keeping the transition under the global job mutex
// preserves the single-action gate while the transaction prevents a second
// caller from observing a half-created replacement queue.
func (m *jobManager) startRevive(rows []account, supersedeID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != "" {
		return "", errIgniteJobActive
	}
	if len(rows) == 0 {
		return "", errNoActionableAccounts
	}
	db, err := openStore()
	if err != nil {
		return "", err
	}
	tx, err := db.Begin()
	if err != nil {
		return "", err
	}
	rollback := func() {
		_ = tx.Rollback()
	}
	var existing int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM jobs WHERE state IN ('running','awaiting_user')`).Scan(&existing); err != nil {
		rollback()
		return "", err
	}
	if existing > 0 {
		rollback()
		return "", errIgniteJobActive
	}
	if strings.TrimSpace(supersedeID) != "" {
		var kind, state string
		if err := tx.QueryRow(`SELECT kind,state FROM jobs WHERE id=?`, supersedeID).Scan(&kind, &state); err != nil {
			rollback()
			return "", err
		}
		if kind != "revive" || (state != "failed" && state != "cancelled") {
			rollback()
			return "", errors.New("revive_job_not_terminal")
		}
		result, err := tx.Exec(`UPDATE jobs SET state='superseded',reason='current_inventory_replaced_queue',updated_at=? WHERE id=? AND kind='revive' AND state IN ('failed','cancelled')`, time.Now().Unix(), supersedeID)
		if err != nil {
			rollback()
			return "", err
		}
		if changed, err := result.RowsAffected(); err != nil || changed != 1 {
			rollback()
			if err != nil {
				return "", err
			}
			return "", errors.New("revive_job_not_terminal")
		}
	}
	id := time.Now().UTC().Format("20060102T150405.000000000Z")
	now := time.Now().Unix()
	if _, err := tx.Exec(`INSERT INTO jobs(id,kind,state,created_at,updated_at,total) VALUES(?,?,?,?,?,?)`, id, "revive", "running", now, now, len(rows)); err != nil {
		rollback()
		return "", err
	}
	for i, a := range rows {
		// Physical paths and credentials remain runtime-only. The durable row
		// contains only the opaque identity needed for exact resume matching.
		workspaceName := strings.TrimSpace(a.WorkspaceName)
		if workspaceName == "" {
			_ = tx.QueryRow(`SELECT workspace_name FROM repair_rows r JOIN jobs j ON j.id=r.job_id WHERE j.kind='revive' AND TRIM(r.account_id)=? AND LOWER(TRIM(r.email))=LOWER(TRIM(?)) AND TRIM(r.workspace_name)<>'' ORDER BY j.created_at DESC,j.rowid DESC,r.ordinal DESC LIMIT 1`, a.AccountID, a.Email).Scan(&workspaceName)
		}
		if _, err := tx.Exec(`INSERT INTO repair_rows(job_id,ordinal,account_key,auth_id,auth_index,email,account_id,workspace_name,auth_path,auth_dir,state) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, id, i+1, a.Key, a.AuthID, a.AuthIndex, a.Email, a.AccountID, workspaceName, "", "", "queued"); err != nil {
			rollback()
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.active = id
	return id, nil
}

func (m *jobManager) claim(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != "" {
		return errors.New("job already active")
	}
	db, err := openStore()
	if err != nil {
		return err
	}
	var state string
	if err := db.QueryRow(`SELECT state FROM jobs WHERE id=?`, id).Scan(&state); err != nil {
		return err
	}
	if state != "running" && state != "awaiting_user" && state != "failed" && state != "cancelled" {
		return errors.New("job is not resumable")
	}
	if _, err := db.Exec(`UPDATE jobs SET state='running',reason='',updated_at=? WHERE id=?`, time.Now().Unix(), id); err != nil {
		return err
	}
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.active = id
	return nil
}

func (m *jobManager) context(id string) context.Context {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id != m.active {
		// A worker that wakes after its job was cancelled or superseded must
		// observe cancellation. Returning a background context here would let
		// a stale Ignite goroutine outlive the global action gate and send
		// traffic concurrently with the next job.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx
	}
	if m.ctx == nil {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx
	}
	return m.ctx
}

type stateResponse struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	State  string `json:"state"`
	Total  int    `json:"total"`
	Done   int    `json:"done"`
	Reason string `json:"reason,omitempty"`
	Result string `json:"result,omitempty"`
}

func readJob(id string) (stateResponse, error) {
	db, err := openStore()
	if err != nil {
		return stateResponse{}, err
	}
	var s stateResponse
	err = db.QueryRow(`SELECT id,kind,state,total,done,reason,result_json FROM jobs WHERE id=?`, id).Scan(&s.ID, &s.Kind, &s.State, &s.Total, &s.Done, &s.Reason, &s.Result)
	return s, err
}

func activeJobProjection() map[string]any {
	db, err := openStore()
	if err != nil {
		return nil
	}
	var id, kind, state string
	if err := db.QueryRow(`SELECT id,kind,state FROM jobs WHERE state IN ('running','awaiting_user') ORDER BY updated_at DESC LIMIT 1`).Scan(&id, &kind, &state); err != nil {
		return nil
	}
	return map[string]any{"id": id, "kind": kind, "state": state}
}

func latestJobProjection() map[string]any {
	db, err := openStore()
	if err != nil {
		return nil
	}
	var id, kind, state, reason, result string
	var total, done int
	if err := db.QueryRow(`SELECT id,kind,state,total,done,reason,result_json FROM jobs ORDER BY updated_at DESC,created_at DESC,rowid DESC LIMIT 1`).Scan(&id, &kind, &state, &total, &done, &reason, &result); err != nil {
		return nil
	}
	return map[string]any{"id": id, "kind": kind, "state": state, "total": total, "done": done, "reason": reason, "result": result, "summary": repairJobSummary(id)}
}

// repairJobSummary is a compact receipt for the dashboard. It is derived from
// queue rows so a refresh still shows the result of a completed repair run.
func repairJobSummary(jobID string) map[string]any {
	db, err := openStore()
	if err != nil {
		return nil
	}
	rows, err := db.Query(`SELECT state FROM repair_rows WHERE job_id=?`, jobID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	counts := map[string]int{"repaired": 0, "skipped": 0, "failed": 0, "cancelled": 0, "queued": 0, "awaiting_user": 0}
	for rows.Next() {
		var state string
		if rows.Scan(&state) != nil {
			continue
		}
		if _, ok := counts[state]; ok {
			counts[state]++
		} else {
			counts["queued"]++
		}
	}
	counts["unresolved"] = counts["skipped"] + counts["failed"] + counts["cancelled"] + counts["queued"] + counts["awaiting_user"]
	return map[string]any{"repaired": counts["repaired"], "skipped": counts["skipped"], "failed": counts["failed"], "cancelled": counts["cancelled"], "unresolved": counts["unresolved"]}
}

func incompleteRepairQueue() []map[string]any {
	db, err := openStore()
	if err != nil {
		return nil
	}
	// Read every Revive outcome newest-first. The visible queue is a projection,
	// not a second queue owner: the newest row for an exact private identity
	// wins, while unresolved identities from separate terminal jobs remain
	// visible. Superseded jobs are historical replacements and stay excluded.
	rows, err := db.Query(`SELECT j.id,j.state,r.ordinal,r.email,r.account_id,r.account_key,r.workspace_name,r.state,r.reason,r.quarantine
FROM repair_rows r JOIN jobs j ON j.id=r.job_id
WHERE j.kind='revive'
ORDER BY j.updated_at DESC,j.created_at DESC,j.rowid DESC,r.ordinal ASC`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	seen := make(map[string]struct{})
	for rows.Next() {
		var jobID, jobState, email, accountID, accountKey, workspaceName, state, reason, quarantine string
		var ordinal int
		if err := rows.Scan(&jobID, &jobState, &ordinal, &email, &accountID, &accountKey, &workspaceName, &state, &reason, &quarantine); err != nil {
			return nil
		}
		identity := strings.TrimSpace(accountID) + "\x00" + strings.ToLower(strings.TrimSpace(email))
		if strings.TrimSpace(accountID) == "" {
			// Legacy rows without a private identity cannot be safely merged with
			// another row; keep each one distinct and visible for diagnosis.
			identity = jobID + "\x00" + strconv.Itoa(ordinal)
		}
		if _, exists := seen[identity]; exists {
			continue
		}
		seen[identity] = struct{}{}
		if state == "repaired" || jobState == "superseded" {
			continue
		}
		if jobState != "running" && jobState != "awaiting_user" && jobState != "failed" && jobState != "cancelled" && jobState != "completed" {
			continue
		}
		seed := account{AccountID: accountID, Key: accountKey}
		// A timed-out regular-Chrome Agent attempt intentionally leaves the
		// auth file in place, so quarantine is not part of recoverability. The
		// exact reason and skipped state are the durable owner-requested retry
		// boundary; provider-auth skips remain unresolved and unretryable here.
		recoverable := (state == "skipped" || state == "failed") && (reason == "oauth_timeout" || reason == reviveSkipWorkspaceMissingReason)
		out = append(out, map[string]any{"number": len(out) + 1, "job_id": jobID, "email": email, "seat": stableSeatLabel(seed), "workspace_name": workspaceName, "state": state, "reason": reason, "quarantined": quarantine != "", "recoverable": recoverable})
	}
	if err := rows.Err(); err != nil {
		return nil
	}
	return out
}

func updateJob(id, state, reason string, done int) {
	if db, err := openStore(); err == nil {
		_, _ = db.Exec(`UPDATE jobs SET state=?,reason=?,done=?,updated_at=? WHERE id=?`, state, reason, done, time.Now().Unix(), id)
	}
	if state != "running" && state != "awaiting_user" {
		globalJobs.mu.Lock()
		if globalJobs.active == id {
			globalJobs.active, globalJobs.ctx, globalJobs.cancel = "", nil, nil
		}
		globalJobs.mu.Unlock()
	}
}

func updateJobResult(id string, result any) {
	db, err := openStore()
	if err != nil {
		return
	}
	encoded, err := jsonMarshalSanitized(result)
	if err != nil {
		return
	}
	_, _ = db.Exec(`UPDATE jobs SET result_json=?,updated_at=? WHERE id=?`, encoded, time.Now().Unix(), id)
}

func jsonMarshalSanitized(value any) (string, error) {
	// All result structs are intentionally opaque/ordinal-only. Keeping this
	// helper in one place makes it harder to accidentally persist credentials.
	raw, err := json.Marshal(value)
	return string(raw), err
}
