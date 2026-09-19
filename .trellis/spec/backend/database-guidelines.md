# Database Guidelines

> Database patterns and conventions for this project.

---

## Overview

<!--
Document your project's database conventions here.

Questions to answer:
- What ORM/query library do you use?
- How are migrations managed?
- What are the naming conventions for tables/columns?
- How do you handle transactions?
-->

(To be filled by the team)

---

## Query Patterns

<!-- How should queries be written? Batch operations? -->

(To be filled by the team)

---

## Migrations

<!-- How to create and run migrations -->

(To be filled by the team)

---

## Naming Conventions

<!-- Table names, column names, index names -->

(To be filled by the team)

---

## Common Mistakes

<!-- Database-related mistakes your team has made -->

(To be filled by the team)

## Scenario: Revive queue reconciliation with current inventory

### 1. Scope / Trigger

This contract applies when the user invokes Revive while Phoenix has durable
repair history and CPA has just-probed invalid credentials. It covers the
storage/service/UI boundary because selecting a queue, changing job state, and
projecting the visible queue must all name the same current Revive job.

### 2. Signatures

- `currentRepairSnapshot() ([]account, error)`: performs one bounded inventory
  and quota probe, hydrates every actionable row's private account ID, and
  returns `errRepairIdentityUnavailable` if any actionable row cannot be
  identified exactly.
- `latestReviveJob() (reviveJobSnapshot, bool, error)`: loads the deterministically
  latest Revive job and its ordered rows.
- `(*jobManager).startRevive(rows []account, supersedeID string) (string, error)`:
  atomically transitions an optional terminal predecessor and inserts one new
  Revive job plus its rows under the global job lock.
- `incompleteRepairQueue() []map[string]any`: exposes non-repaired rows only for
  the deterministically latest active or resumable Revive job.

No schema migration is required. `jobs.state` admits the additive terminal
value `superseded`; its reason is `current_inventory_replaced_queue`.

### 3. Contracts

- Repair identity is trimmed private `AccountID` plus trimmed, case-normalized
  email. Email, host key, filename, and display order are never identity proof.
- A `running` or `awaiting_user` job owns the global gate and is never probed or
  superseded by another action.
- For a `failed` or `cancelled` job with pending rows:
  - no current actionable invalid rows -> resume;
  - any exact current/pending overlap -> resume;
  - complete non-empty current snapshot with zero overlap -> atomically mark the
    predecessor `superseded` and create the fresh queue;
  - any actionable current row missing private identity -> fail before changing
    either job or repair rows.
- Superseding changes only the selected job state and reason. Historical row
  state, row reason, and opaque quarantine marker remain unchanged.
- `completed` and `superseded` jobs are terminal and never resume.
- Latest-job queries use `updated_at DESC, created_at DESC, rowid DESC`; seconds
  alone are not a deterministic ordering key.

### 4. Validation & Error Matrix

| Condition | Result |
| --- | --- |
| Active Ignite or Revive exists | `job_active`; no inventory probe or write |
| Terminal prior queue overlaps current snapshot | Resume the prior job |
| Terminal prior queue and no current invalid work | Resume the prior job |
| Terminal prior queue and complete disjoint current snapshot | One transaction supersedes prior and inserts fresh job/rows |
| Any current actionable row lacks private identity | `repair_identity_unavailable`; no disposition write |
| Supersede target is absent, not Revive, or not terminal | Transaction rolls back with `revive_job_not_terminal` |
| New job or row insert fails | Transaction rolls back; predecessor remains resumable |

The management API maps private-identity capture failure to sanitized
`inventory_unavailable`; it never returns private IDs, paths, OAuth material,
codes, or quarantine markers.

### 5. Good/Base/Bad Cases

- Good: one failed historical row and two fully identified disjoint current
  invalid rows produce a preserved `superseded` predecessor and one fresh
  two-row queue.
- Base: one exact current/pending identity resumes the old queue, even if other
  current rows exist.
- Bad: filtering an unidentified invalid row, declaring the remainder disjoint,
  and abandoning the historical queue.
- Bad: aggregating non-repaired rows across all jobs into one visible queue.

### 6. Tests Required

- Exact identity comparison: trimming, email case normalization, different
  email, different account ID, and missing account ID.
- Disjoint terminal queue: atomic supersede/new snapshot, preserved historical
  row reason/quarantine marker, and fresh-only visible projection.
- Overlap, no-current-work, and active-job branches: prior resume or lock with
  no extra job.
- Incomplete current identity: return the sentinel error and persist no partial
  queue or predecessor transition.
- Restart: `superseded` remains terminal and absent from the queue projection.
- Ordering: jobs sharing second-resolution timestamps project the later rowid.
- Full Go test, race, vet, dashboard JavaScript, shell syntax, and diff checks.

### 7. Wrong vs Correct

#### Wrong

```go
if resumed, ok := resumeRevive(headers, mode); ok {
	return resumed, nil
}
rows := currentRepairSnapshot()
```

This lets historical state outrank the current source of truth and never asks
whether the queue still describes current work.

#### Correct

```go
prior := latestReviveJob()
rows := currentRepairSnapshot() // complete exact identities or error
if terminal(prior) && disjoint(prior.Pending, rows) {
	return globalJobs.startRevive(rows, prior.ID) // one transaction
}
return resumeOrStart(prior, rows)
```
