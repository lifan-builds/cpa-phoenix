# Fix Phoenix OAuth repair handoff

## Goal

Make the CPA Phoenix Revive workflow fail fast and explain itself when the
OpenAI provider returns the observed authentication-error page, while keeping
the exact quarantined workspace safe to resume. The live 401 repair will be
rerun only after the code and tests are complete.

## Background and confirmed facts

- The current live queue has eight rows. Its first row is the 401 workspace
  that was quarantined and ended with `oauth_timeout`; the remaining rows are
  still queued.
- `loginPageScript` classifies an otherwise complete, unrecognized page as
  `manual_login_required` (`browser_login.go:265-279`). The automatic browser
  reports that status, but `processReviveRow` continues polling native OAuth
  until its five-minute deadline (`repair.go:852-880`).
- A direct regular-Chrome check showed the provider page text “Authentication
  Error”, `error_code: unknown_error`, and “Your session has ended”. No
  verification code arrived for that attempt.
- Phoenix already validates the callback, exact workspace identity, credential
  availability, and quota before marking a row repaired. Quarantined rows are
  resumable and must not be quarantined a second time.
- The dashboard receives only allowlisted transient automation statuses and a
  sanitized terminal job reason. Raw OAuth URLs, state, codes, credentials, and
  provider page text must not be persisted or logged.

## Requirements

1. Detect the observed provider authentication-error/session-ended page from
   visible browser text/title and report a stable allowlisted
   `provider_auth_error` status. Unknown pages and challenges retain their
   existing manual-intervention behavior.
2. Propagate that browser result to the Revive state machine so the current row
   ends promptly with terminal reason `provider_auth_error`, the native OAuth
   session is cancelled, and no replacement is marked repaired.
3. Preserve the existing quarantine and resume contract: the failed row keeps
   its opaque quarantine marker, and a later explicit Resume action starts a
   fresh OAuth attempt for the same private workspace identity.
4. Render an actionable dashboard message for `provider_auth_error` that tells
   the operator to complete/retry OpenAI sign-in and resume the queue. Do not
   expose provider URLs, OAuth state, mail contents, or credentials.
5. Add regression coverage for browser-page detection, prompt termination and
   queue persistence/resume semantics, status sanitization, and dashboard copy.
6. Keep the native OAuth URL composition, callback listener, workspace-seat
   matching, and quota/replacement validation behavior unchanged.

## Acceptance Criteria

- [ ] A browser fixture containing the observed provider markers returns
  `provider_auth_error` before the test deadline, reports the status once, and
  does not submit an email/code/workspace field. A generic unknown page still
  reports `manual_login_required`.
- [ ] An automatic Revive fixture whose browser returns `provider_auth_error`
  marks the row and job failed with that reason without waiting five minutes,
  leaves the quarantine marker intact, cancels the native OAuth session, and
  leaves the row eligible for the existing resume path.
- [ ] The allowlist and dashboard map expose the new status only as the
  sanitized message; no raw provider error text or secret fields appear in the
  management payload or embedded dashboard.
- [ ] Existing URL, callback, exact-seat, replacement/quota, and agent-mode
  tests remain green.
- [ ] `go test -count=1 ./...`, `go test -race -count=1 ./...`, `go vet ./...`,
  shell syntax checks, dashboard JavaScript parsing, and `git diff --check`
  pass.
- [ ] After rebuilding/restarting CPA, the Phoenix queue is rerun in regular
  Chrome and the final repaired/failed counts are read from Phoenix state. A
  provider-side failure is reported as unverified rather than claimed as a
  repair.

## Out of scope and deferred risks

- Do not guess why OpenAI produced the provider error, alter OpenAI account
  state, or bypass CAPTCHA/MFA/session controls.
- Do not add automatic retries, credential handling, or a second browser
  profile. The operator remains responsible for completing any provider step.
- The live provider may continue to reject a fresh OAuth URL; that outcome is
  an external limitation and will be recorded separately from code validation.
