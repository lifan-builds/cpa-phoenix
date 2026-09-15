# Implementation and validation plan

1. Add the provider-page detector and `provider_auth_error` return/report path
   in `browser_login.go`; preserve the existing manual challenge branches.
2. Add the automatic-browser result handoff and early terminal mapping in
   `repair.go`; extend the status allowlist.
3. Add the dashboard message and README behavior note, keeping the existing
   agent-mode UX and privacy projection intact.
4. Add focused browser, queue, sanitizer, and dashboard regressions. Use short
   fixture deadlines; do not make tests depend on the five-minute production
   timeout.
5. Run `gofmt -w` on changed Go files, then:
   - `go test -count=1 ./...`
   - `go test -race -count=1 ./...`
   - `go vet ./...`
   - `bash -n build.sh package-release.sh`
   - the existing dashboard JavaScript parse/presentation tests
   - `git diff --check`
6. Run `./build.sh`, restart CPA so it loads the rebuilt Darwin dylib, reacquire
   the regular Chrome binding, and rerun/resume Phoenix. Read the final queue
   state from the Phoenix management endpoint; do not treat a closed tab or a
   provider error page as success.

## Risk checkpoints

- Browser result handling must not race or reuse a prior row's status.
- A provider error must cancel only its own native OAuth state and never touch
  Ignite or another queue.
- Existing agent mode has no automatic browser result channel and must remain
  a manual-login path.
