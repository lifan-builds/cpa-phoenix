# Repository evidence: stale repair queue

- `repair.go:429-472`: Revive resumes before it probes current invalid rows.
- `repair.go:522-574`: failed, cancelled, running, and awaiting-user jobs with
  pending rows are resumable; unknown terminal states are currently rewritten
  to failed.
- `repair.go:725-739`: quarantined rows already reconcile only an exact private
  identity with a successful quota read.
- `inventory.go:373-428`: exact private account ID plus email owns repair
  resolution; email alone is forbidden because multiple teams can share it.
- `store.go:128-135`: restart converts active Revive work to failed/interrupted.
- `store.go:459-478`: the dashboard queue currently reads all non-repaired rows
  from all jobs, diverging from the single latest job used by resume.
- `dashboard.go:161-167`: the single action is labeled Resume whenever any
  incomplete row is projected, even when separate current invalid rows exist.
