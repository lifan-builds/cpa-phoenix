# Error Handling

> How errors are handled in this project.

---

## Overview

<!--
Document your project's error handling conventions here.

Questions to answer:
- What error types do you define?
- How are errors propagated?
- How are errors logged?
- How are errors returned to clients?
-->

(To be filled by the team)

---

## Error Types

<!-- Custom error classes/types -->

(To be filled by the team)

---

## Error Handling Patterns

<!-- Try-catch patterns, error propagation -->

(To be filled by the team)

---

## API Error Responses

<!-- Standard error response format -->

(To be filled by the team)

---

## Common Mistakes

<!-- Error handling mistakes your team has made -->

(To be filled by the team)

## Scenario: Phoenix OAuth provider authentication failure

### 1. Scope / Trigger

This contract applies when the Phoenix browser sees OpenAI's authentication-error or ended-session page during a Revive attempt. The browser and queue communicate only through fixed, allowlisted status identifiers; provider page text and OAuth payloads never cross the management boundary.

### 2. Signatures

- `loginPageScript(...) string`: returns a transient browser status.
- `loginBrowser.Run(ctx, oauthURL, email, accountID, requestedAt, report) error`: reports the status once and returns `provider_auth_error` for the recognized provider page.
- `processReviveRow(ctx, runtime, ordinal, ...) string`: maps that browser result to the terminal row/job reason.
- `sanitizeAutomationStatus(status string) string`: allowlists the status before it reaches the dashboard.

### 3. Contracts

- Recognized visible markers (`Authentication Error` in the title, `Your session has ended`, or an authentication-error body with an `error_code`) return `provider_auth_error` before generic manual-login handling.
- `provider_auth_error` is a transient status and terminal reason; the dashboard maps it to actionable retry copy.
- Terminal handling cancels the native OAuth state and leaves the row's opaque quarantine marker intact. Resume creates a fresh attempt for the same workspace identity.
- `GET /v0/management/plugins/cpa-phoenix/state` and Revive polling expose only sanitized status/reason fields; raw URLs, state, codes, credentials, mailbox contents, and provider text are excluded.

### 4. Validation & Error Matrix

| Condition | Result |
| --- | --- |
| Provider title/body contains the observed authentication markers | `provider_auth_error`; no form submission |
| Unknown complete page | `manual_login_required` |
| CAPTCHA/anti-bot challenge | `manual_captcha_required` |
| Browser returns provider error during native poll | Row/job fails promptly with `provider_auth_error`; OAuth is cancelled |
| Browser result is absent or generic | Existing poll, timeout, or manual-intervention behavior |

### 5. Good/Base/Bad Cases

- Good: a fixture page with the observed markers reports exactly once, submits no email/code/workspace field, and the quarantined row remains resumable.
- Base: an ordinary sign-in page continues through the existing email-code/workspace flow and replacement/quota validation.
- Bad: treating a closed tab, an unrecognized page, or an unverified callback as a repaired account.

### 6. Tests Required

- Browser regression asserts marker detection, one-shot reporting, and zero field submissions; an unknown page remains `manual_login_required`.
- Queue regression asserts prompt terminal mapping, native OAuth cancellation, preserved quarantine, and resume eligibility.
- Sanitizer/dashboard tests assert allowlisting and the exact actionable message without provider payload leakage.
- Existing URL, callback, workspace-seat, replacement, and quota tests remain green.

### 7. Wrong vs Correct

#### Wrong

```go
// A manual-login status is allowed to poll until the five-minute deadline.
return "manual_login_required"
```

#### Correct

```go
// The browser owns provider-page recognition; the queue owns terminal state.
if browserErr.Error() == "provider_auth_error" {
	return "provider_auth_error"
}
```
