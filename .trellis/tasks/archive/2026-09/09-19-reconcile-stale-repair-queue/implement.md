# Implementation and validation plan

1. Add a shared exact pending-row overlap/disposition helper using private
   account ID plus normalized email. Reuse the existing invalid-row capture and
   quota-probe path; do not add an email-only fallback.
2. Refactor `beginRevive` / `resumeRevive` so terminal prior-job disposition is
   evaluated against one fresh actionable snapshot before resume. Persist the
   disjoint old job as `superseded` and start the fresh snapshot; preserve
   active-job and overlapping/no-fresh-work resume behavior.
3. Scope `incompleteRepairQueue` to the latest Revive job only when its state is
   active or resumable. Preserve all historical repair rows in SQLite.
4. Update the dashboard's queue/current-invalid action copy for the automatic
   behavior without adding client-side identity decisions.
5. Add focused regressions for disjoint supersede/new snapshot, exact overlap
   resume, no-current-invalid resume, active-job protection, restart
   persistence, quarantine preservation, latest-job queue projection, and
   dashboard labels.
6. Run formatting and focused tests, then the full quality gate:
   - `gofmt -w` on changed Go files
   - focused `go test -count=1 -run` cases for repair/store/dashboard behavior
   - `go test -count=1 ./...`
   - `go test -race -count=1 ./...`
   - `go vet ./...`
   - `bash -n build.sh package-release.sh`
   - existing dashboard JavaScript parse/presentation tests
   - `git diff --check`
7. Run the Trellis check and bug-retrospective/spec-update gates. Build the
   plugin, restart CPA, and validate that the stale historical job becomes
   superseded while a fresh two-row current queue is created. Do not claim
   either row repaired until Phoenix validates its exact replacement and quota.

## Risk checkpoints and rollback

- Stop if current invalid rows cannot capture private identity; never
  supersede merely because an unsafe row was filtered out.
- Stop if an active Revive or Ignite job owns the global job lock.
- The rollback boundary is the automatic disposition change. Superseded rows
  remain durable and can be reclassified manually if the code is reverted.
