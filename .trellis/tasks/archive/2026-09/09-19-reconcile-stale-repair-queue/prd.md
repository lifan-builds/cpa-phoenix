# Reconcile stale Phoenix repair queue

## Goal

Automatically start a fresh Phoenix repair queue for the currently invalid CPA
credentials when a terminal prior queue has no exact identity overlap with the
fresh invalid inventory, without deleting the prior recovery record or
weakening exact-workspace matching.

## Background and confirmed facts

- The live CPA quota inventory showed two currently invalid credentials for
  one account owner, while Phoenix resumed an older quarantined row for a
  different owner and workspace.
- The dashboard accurately exposed one row in the repair queue and two
  credentials still invalid in CPA. The defect is that the single action
  always resumes the prior queue instead of reconciling it with current work.
- `beginRevive` calls `resumeRevive` before scanning current inventory
  (`repair.go:429-472`). A newest job with a non-repaired row in state
  `failed`, `cancelled`, `running`, or `awaiting_user` is resumable
  (`repair.go:522-574`), so even cancellation cannot create a fresh queue.
- Exact private account ID plus email is the repair identity; email alone is
  intentionally unsafe because one email may own multiple workspaces
  (`inventory.go:373-428`). Exact healthy replacements are already reconciled
  before another OAuth attempt (`repair.go:725-739`).
- `incompleteRepairQueue` returns every non-repaired row across all jobs
  (`store.go:459-478`), even though `resumeRevive` can operate only on the
  latest Revive job.
- On process restart, active Revive jobs become terminal `failed` jobs with
  reason `interrupted` (`store.go:128-135`). This provides a safe point for
  automatic reconciliation; live `running` or `awaiting_user` work must not be
  superseded.

## Requirements

1. Before resuming a terminal (`failed` or `cancelled`) Revive job, freshly
   probe the current CPA inventory and capture exact-identity actionable
   invalid rows using the existing quota and private-ID rules.
2. If at least one current actionable invalid row exists and none exactly
   overlaps the pending rows of the terminal prior job, atomically mark the
   prior job `superseded` and create a new queue from the fresh snapshot.
3. If any exact identity overlaps, or if there is no fresh actionable invalid
   work, preserve the existing resume behavior. Never infer overlap from email,
   filename, display order, or host key alone.
4. Never supersede a `running` or `awaiting_user` job. Preserve current global
   job locking and Ignite isolation.
5. Preserve superseded repair rows, reasons, and opaque quarantine markers for
   audit/manual recovery. Do not delete or restore credentials or quarantine
   files, and do not label a superseded row repaired.
6. Exclude `superseded` jobs from future resume selection and from the visible
   incomplete repair queue, including after restart. Scope the queue projection
   to the same latest resumable job that the backend can actually operate.
7. Make the dashboard action copy describe current repair work when both a
   prior queue and current invalid credentials exist; do not promise that the
   old row will be resumed.
8. Keep provider login, Thunderbird, callback, exact-seat selection,
   replacement/quota validation, and secret-redaction contracts unchanged.

## Acceptance Criteria

- [x] Given one terminal failed/quarantined old row and two disjoint current
  invalid credentials, Revive marks only the old job `superseded`, preserves
  its row/quarantine marker, and creates a new two-row queue from fresh probes.
- [x] A superseded job is not resumed and its rows are absent from
  `repair_queue` after an in-process refresh and after reopening the store.
- [x] If a current invalid row has the same private account ID and email as a
  pending old row, Phoenix resumes the old job and does not supersede it.
- [x] If there are no current actionable invalid rows, Phoenix keeps the prior
  incomplete queue resumable rather than silently abandoning it.
- [x] `running` and `awaiting_user` jobs retain current active-job behavior and
  are never superseded by a scan.
- [x] Exact healthy replacements still reconcile as repaired; a healthy
  same-email/different-account-ID workspace does not repair or overlap a row.
- [x] The dashboard no longer labels a disjoint-current-inventory action solely
  as `Resume Repair Queue`; ordinary no-queue and active-job labels remain
  unchanged.
- [x] Focused repair/store/dashboard tests and the full Go test, race, vet,
  shell, JavaScript, and diff checks pass.

## Out of scope

- Do not delete, restore, or expose quarantine files.
- Do not add a general job-history or manual queue-management interface.
- Do not merge pending rows from multiple historical jobs.
- Do not change Thunderbird, provider-login, callback, or workspace-selection
  automation.
