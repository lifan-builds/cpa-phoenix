package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
 auth_path TEXT NOT NULL DEFAULT '',
 auth_dir TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL,
 reason TEXT NOT NULL DEFAULT '',
 quarantine TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(job_id,ordinal)
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
	_, _ = db.Exec(`ALTER TABLE repair_rows ADD COLUMN auth_path TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE repair_rows ADD COLUMN auth_dir TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE repair_rows ADD COLUMN quarantine TEXT NOT NULL DEFAULT ''`)
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
		return "", errors.New("job already active")
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
		return "", errors.New("job already active")
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
	if err := db.QueryRow(`SELECT id,kind,state,total,done,reason,result_json FROM jobs ORDER BY updated_at DESC LIMIT 1`).Scan(&id, &kind, &state, &total, &done, &reason, &result); err != nil {
		return nil
	}
	return map[string]any{"id": id, "kind": kind, "state": state, "total": total, "done": done, "reason": reason, "result": result}
}

func incompleteRepairQueue() []map[string]any {
	db, err := openStore()
	if err != nil {
		return nil
	}
	rows, err := db.Query(`SELECT email,account_id,account_key,state,quarantine FROM repair_rows WHERE state<>'repaired' ORDER BY job_id,ordinal`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	for rows.Next() {
		var email, accountID, accountKey, state, quarantine string
		if rows.Scan(&email, &accountID, &accountKey, &state, &quarantine) != nil {
			continue
		}
		seed := account{AccountID: accountID, Key: accountKey}
		out = append(out, map[string]any{"number": len(out) + 1, "email": email, "seat": stableSeatLabel(seed), "state": state, "quarantined": quarantine != ""})
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
