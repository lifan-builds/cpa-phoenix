# Quality Guidelines

> Code quality standards for backend development.

---

## Overview

<!--
Document your project's quality standards here.

Questions to answer:
- What patterns are forbidden?
- What linting rules do you enforce?
- What are your testing requirements?
- What code review standards apply?
-->

Phoenix's highest-risk paths cross native browser automation, Thunderbird's
local mail cache, the callback listener, and the durable repair queue. Quality
checks must prove the boundary behavior, not only the individual helper.

---

## Forbidden Patterns

<!-- Patterns that should never be used and why -->

- Do not launch Thunderbird once per controller and assume every later mailbox
  is synchronized. Activate it for every queued login attempt.
- Do not treat a closed OAuth tab, a successful-looking provider page, or a
  healthy inventory total as proof that a repair row completed.
- Do not print verification codes, mailbox contents, OAuth URLs, credentials,
  or private workspace IDs in logs, test failures, or dashboard status.

---

## Required Patterns

<!-- Patterns that must always be used -->

- Keep browser and mail integrations behind injectable seams so tests can prove
  per-attempt activation and freshness without real accounts.
- Keep the queue as the source of truth for repaired/failed/quarantined state;
  callback and quota validation must finish before a row is repaired.
- Preserve bounded retry behavior: at most one resend, then a resumable
  timeout/intervention state.

---

## Testing Requirements

<!-- What level of testing is expected -->

- Run `go test -count=1 ./...`, `go test -race -count=1 ./...`, `go vet ./...`,
  shell syntax checks, dashboard JavaScript parsing, and `git diff --check`.
- Add a regression for every external-source freshness boundary. For the
  Thunderbird path, assert one launcher invocation per login attempt and a
  fresh recipient-scoped code after resend.
- For queue changes, assert terminal dashboard state and exact row identity;
  aggregate healthy counts are not an acceptance criterion.

---

## Code Review Checklist

<!-- What reviewers should check -->

- [ ] Did the change preserve the external source's freshness boundary?
- [ ] Is activation performed on every attempt rather than only at startup?
- [ ] Are stale data, missing data, timeout, and manual intervention distinct?
- [ ] Does the regression test cover the cross-layer path and not just a mock
      helper?
- [ ] Does the final check use Phoenix's queue state as the repair receipt?
