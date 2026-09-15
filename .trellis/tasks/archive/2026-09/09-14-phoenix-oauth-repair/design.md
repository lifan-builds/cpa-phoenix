# Technical design

## Boundary and data flow

`OpenAI page visible text/title → loginPageScript state → loginBrowser.Run
status/error → processReviveRow native-poll loop → durable job/row reason →
dashboard message`

The browser layer owns recognition of a provider page. The repair state
machine owns whether a row is terminal and remains the authority for callback,
replacement, and quota validation. The dashboard only maps an allowlisted
status/reason to copy.

## Design

1. Extend `loginPageScript` with a narrow provider marker check for the
   observed authentication-error and session-ended text. Return
   `provider_auth_error` before the generic manual-login fallback. When the
   state is returned, `loginBrowser.run` reports it and returns the same safe
   error; it performs no field clicks on that page.
2. Give the automatic browser invocation in `processReviveRow` a buffered,
   one-shot result channel. The native poll loop checks that channel around its
   existing poll/wait cycle and returns `provider_auth_error` immediately when
   present. Existing `nil`, cancellation, and unknown browser errors retain
   their current behavior.
3. Add `provider_auth_error` to `sanitizeAutomationStatus` and map it in the
   dashboard's `phoenixAutomationMessage`. The terminal job reason uses the
   same sanitized vocabulary, so the operator sees actionable copy without
   provider payload leakage.
4. Leave the durable schema and resume code unchanged. The existing terminal
   path writes the row failure while preserving its quarantine marker; resume
   creates a new in-memory OAuth state and reuses the exact captured account ID.

## Compatibility and rollback

This is additive status handling with no API or schema change. Old dashboard
clients will display the stable reason string; current clients get the new
message. If the live provider behavior differs, revert only the provider marker
and result-channel changes; URL/callback and queue safety remain independent.

## Security and privacy

Only fixed status identifiers cross the browser/runtime/dashboard boundary.
Visible page text is matched locally and never stored, logged, or returned.
OAuth URLs, state, codes, account IDs, and mailbox contents remain transient.
