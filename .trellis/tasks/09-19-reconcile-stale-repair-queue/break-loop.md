## Bug Analysis: Stale repair queue outranked current invalid inventory

### 1. Root Cause Category

- **Category**: B/E/D - Cross-layer contract, implicit assumption, and test coverage gap
- **Specific Cause**: Revive startup treated the newest incomplete durable job as the current source of work before probing CPA's live invalid inventory. The management projection then returned non-repaired rows from every historical job. Storage, service, and dashboard therefore agreed on persistence but disagreed on what "current repair queue" meant.
- **Evidence and confidence**: The live dashboard showed one historical queued workspace beside two different current invalid workspaces. Repository tracing showed `beginRevive` resumed first and the queue projection was not scoped to the backend's selectable job. Direct regression tests now reproduce the mismatch and the corrected disposition. Confidence is above 95%.

### 2. Why Fixes Failed

1. Repairing the old credential fixed that credential, but did not define how a durable recovery queue should be reconciled with later live inventory.
2. Showing both queue and inventory counts made the mismatch visible, but the action still promised and executed only a resume.
3. The first implementation compared captured rows but silently dropped invalid rows without private identity; that made a partial snapshot unsafe as proof of zero overlap.
4. Second-resolution timestamps allowed the atomic predecessor/new-job writes to tie, so an otherwise correct queue could still display the superseded job as the latest status.

### 3. Prevention Mechanisms

| Priority | Mechanism | Specific Action | Status |
| --- | --- | --- | --- |
| P0 | Architecture | Reconcile one fresh invalid snapshot with the latest terminal Revive job before choosing resume or supersede | DONE |
| P0 | Runtime safety | Require every actionable row in the snapshot to have a private account ID before allowing disposition | DONE |
| P0 | Database transaction | Supersede the predecessor and insert the new job plus rows in one transaction under the global job lock | DONE |
| P0 | Test coverage | Cover disjoint, overlap, empty, active, incomplete-identity, restart, preservation, and timestamp-tie cases | DONE |
| P1 | Documentation | Record the executable queue reconciliation contract in the backend database spec | DONE |

### 4. Systematic Expansion

- **Similar Issues**: Any durable recovery workflow that presents historical rows beside a freshly probed source of truth can confuse "recoverable" with "currently actionable."
- **Design Improvement**: Keep selection, durable transition, and UI projection anchored to the same ordered job identity. Historical evidence remains queryable but never becomes current work by aggregation.
- **Process Improvement**: For state transitions that create two records in one second, tests must exercise deterministic tie-breaking as well as transaction atomicity.

### 5. Knowledge Capture

- [x] Added the Revive reconciliation contract to `.trellis/spec/backend/database-guidelines.md`.
- [x] Added a cross-layer checklist for durable recovery state versus live inventory.
- [x] Added regression coverage for the corrected behavior and the review-discovered edge cases.
- [x] Confirmed this project has no `src/templates/markdown/spec/` mirror to sync.
