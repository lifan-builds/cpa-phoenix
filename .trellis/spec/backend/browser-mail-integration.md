# Browser and Mail Integration Guidelines

> Contracts for Phoenix's native Chrome and Thunderbird email-code boundary.

## Scenario: Revive email-code synchronization

### 1. Scope / Trigger

Use this contract whenever Revive asks OpenAI for an email verification code and
Phoenix reads that code from Thunderbird's local mail files. The provider page,
Thunderbird synchronization, browser automation, and Phoenix's durable queue
are separate layers; a successful provider page is not sufficient evidence that
the queued row was repaired.

### 2. Signatures

- `newLoginBrowser(ctx context.Context) (*loginBrowser, error)` creates the
  dedicated Phoenix Chrome controller and installs the production Thunderbird
  launcher.
- `(*loginBrowser).Run(ctx, oauthURL, email, accountID string,
  requestedAt time.Time, report func(string)) error` runs one native OAuth
  attempt and reports only allowlisted transient statuses.
- `detectThunderbirdCode(recipient string, requestedAt time.Time)
  (thunderbirdCode, error)` reads a matching, fresh six-digit code from the
  local Thunderbird mail store.
- `launchThunderbird()` brings Thunderbird forward on macOS. The injected
  `loginBrowser.launchThunderbird` seam is used by tests.

### 3. Contracts

- The production launcher runs at the start of **every** queued login attempt.
  A Thunderbird process that was started in the background once is not proof
  that the next recipient's mailbox has synchronized.
- Thunderbird owns IMAP synchronization; Phoenix only reads local mail files.
  `requestedAt` is the freshness boundary: a code received before that time is
  stale and cannot be submitted.
- Code detection is recipient-scoped and accepts only a six-digit code. The
  code, mailbox contents, OAuth URL, credentials, and private workspace ID
  never enter dashboard status, logs, tests' failure messages, or user-facing
  reports.
- A resend is bounded to one per attempt and resets the freshness boundary.
  A missing code remains a waiting/intervention state until the normal
  five-minute attempt deadline produces `oauth_timeout`.
- Phoenix's queue validates callback, exact workspace identity, credential
  availability, replacement propagation, and quota before marking a row
  repaired.

### 4. Validation & Error Matrix

| Condition | Result |
| --- | --- |
| New queued attempt on macOS | Bring Thunderbird forward before code polling |
| Non-macOS test/runtime | Launcher is a no-op; code detection contract remains unchanged |
| Matching fresh code found | Submit it only for the same `data-attempt` / queue attempt |
| No matching fresh code yet | Report `verification_code_waiting`; allow synchronization to catch up |
| One bounded resend needed | Request it, reset `requestedAt`, and accept only the new code |
| Repeated absence or five-minute deadline | Return/report `oauth_timeout`; leave the row resumable |
| Callback page reached | Return to Phoenix for queue-owned callback/replacement/quota validation |
| Unknown page, CAPTCHA, recipient mismatch, or non-email MFA | Stop for user intervention; never guess or report success |

### 5. Good / Base / Bad Cases

- Good: each queued account activates Thunderbird, receives a recipient-matched
  fresh code, and Phoenix later marks that exact row `repaired`.
- Base: a code is delayed; the attempt waits, performs at most one resend, and
  either continues with a newer code or ends resumably at `oauth_timeout`.
- Bad: launching Thunderbird once with `open -gja`, scanning a stale local
  mailbox, and treating a code that appears later as proof that the attempt
  should have succeeded.
- Bad: reporting a healthy account total or closed OAuth tab as queue repair.

### 6. Tests Required

- `browser_login_test.go` must prove sequential attempts invoke the injected
  Thunderbird launcher once per attempt, not once per controller lifetime.
- `thunderbird_test.go` must cover recipient mismatch, stale timestamps, and
  missing/invalid codes without exposing mailbox contents.
- Browser fixtures must cover one resend, callback detection, provider errors,
  and manual-intervention pages.
- Before release, run the full Go, race, vet, shell, dashboard-parser, and
  `git diff --check` gates; a live repair is accepted only from Phoenix's
  terminal queue state.

### 7. Wrong vs Correct

#### Wrong

```go
sync.Once.Do(func() {
	_ = exec.Command("/usr/bin/open", "-gja", "Thunderbird").Run()
})
code, _ := detectThunderbirdCode(email, requestedAt)
```

This starts Thunderbird only once and can scan a stale mailbox for later queue
rows.

#### Correct

```go
// At the start of every queued attempt:
b.launchThunderbird()
code, _ := b.detectCode(email, requestedAt)
```

The activation and freshness boundary are part of the same attempt contract,
while queue completion remains Phoenix-owned.
