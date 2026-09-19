# Technical design

## Boundary and data flow

`fresh CPA inventory/quota probe -> exact invalid-row snapshot -> latest Revive
job and pending rows -> queue disposition (resume or supersede) -> durable job
transition -> queue start/resume -> sanitized state projection -> dashboard`

Inventory and queue disposition remain backend-owned. The dashboard renders
the sanitized counts and action copy; it does not compare email addresses or
invent queue state.

## Queue disposition contract

Add one exact-identity comparison used by Revive startup:

- Identity is the normalized private `AccountID` plus exact normalized email.
- Host key is a routing hint only and email alone never overlaps a row.
- `running` / `awaiting_user`: active; return existing job-active behavior.
- `failed` / `cancelled` with pending rows:
  - current actionable rows non-empty and zero exact overlap -> persist job
    state `superseded`, reason `current_inventory_replaced_queue`, then snapshot
    and start a new job;
  - any exact overlap -> resume the existing job;
  - no current actionable rows -> resume the existing job.
- `completed` / `superseded`: terminal and never resumable.

The supersede update changes only the selected Revive job. Repair-row state,
reason, and quarantine marker remain untouched. No schema migration is needed
because job states and reasons are strings.

## Backend structure

Refactor Revive startup so the fresh inventory is collected once and reused:

1. Normalize browser mode and reject/retain active-job behavior.
2. List accounts, perform the existing bounded quota probes, and capture only
   invalid rows with private identity.
3. Load the latest Revive job and its pending rows.
4. Apply the disposition contract above.
5. Resume the prior runtime or create the new durable job using the already
   captured fresh rows.

Keep queue selection and display aligned. The incomplete-queue projection joins
repair rows to the latest Revive job and emits rows only when that job is in a
resumable/active state. Historical failed rows older than a completed or
superseded latest job no longer masquerade as the current queue.

## Dashboard behavior

Keep one automatic Revive action. When a prior queue and current invalid count
coexist, label the action around repairing current invalid accounts rather than
promising a resume. The backend remains authoritative: it resumes on exact
overlap and supersedes only on the disjoint terminal case. Active-job and
ordinary no-queue labels stay unchanged.

## Compatibility, privacy, and rollback

- Additive `superseded` job state; no database schema migration.
- Existing interrupted-job recovery remains: restart converts active Revive
  jobs to `failed`, after which the next explicit Revive invocation performs
  the automatic disposition.
- No account IDs, OAuth URLs, tokens, codes, paths, or quarantine filenames are
  added to management projections or logs.
- Rollback is limited to the disposition helper, projection scoping, and
  dashboard copy. Preserved historical rows remain readable by the old build.

## Risks

- Automatic superseding deliberately deprioritizes a disjoint quarantined row;
  it must remain recorded as superseded, never repaired or deleted.
- Two concurrent Revive requests must not create two jobs. Existing global job
  locking remains the final serialization point; tests must cover the durable
  transition before new job creation.
- Fresh invalid probing must not be duplicated in ways that produce different
  snapshots for the disposition and the new queue.
