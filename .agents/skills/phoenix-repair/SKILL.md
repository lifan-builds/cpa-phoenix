---
name: phoenix-repair
description: Repair invalid CPA Phoenix workspace accounts in regular Chrome using Agent Autofill email codes. Use for Phoenix login repair or resuming its repair queue.
---

# Phoenix repair

Use the `phoenixctl` terminal client for Phoenix inventory, queue control, status,
and polling. Use native Chrome tools in the user's regular profile only for the
provider's interactive OAuth/login page, not for the Phoenix dashboard.
Phoenix owns the queue, callback, replacement detection, and quota validation;
automatic mode may quarantine an auth file, while regular Chrome agent mode
leaves the file in place so transient failures do not quarantine the account.
The skill only operates the visible login page. Never claim a row
is repaired from a closed tab, a healthy account total, or a successful-looking
provider page.

No subagents, source review, build, or service restart for routine repairs. If
the dashboard is missing a documented control, do not start a second repair
path. When the owner asks to improve the workflow, fix the skill and the
missing plugin behavior, verify the installed version, then resume the same
queue.

## Preflight and queue ownership

0. If Phoenix source or its installed dylib changed, run `./verify.sh`, build
   and install it, and restart CPA **before** clicking Revive. Do not rebuild
   or restart while a Revive attempt is active: the native OAuth tab and
   per-attempt state are lost, and the durable queue will correctly become
   `interrupted`. If a restart is unavoidable, wait for the queue to become
   terminal, then resume the preserved queue once and recheck the exact email
   and seat.

1. Run `phoenixctl status` and `phoenixctl otp` with the CPA management key.
   The client uses the local management API and does not open the dashboard.
   Run `phoenixctl repair` to start or resume one Agent-mode queue. Do not
   invent a separate browser queue or run it twice while a job is active. If
   the queue is empty and the invalid count is zero, stop.
2. Read only the sanitized terminal response: current email/seat, attempt
   token, status, and the transient OAuth URL. Keep login URLs, attempt
   identifiers, and codes in variables; never print them. The client opens the
   current login URL in regular Chrome once per attempt. Phoenix asks Agent
   Autofill for the matching fresh code; if it is unavailable, stop and fix the
   connector rather than switching mail clients.
   If an existing attempt is already displayed, resume that attempt instead
   of starting another one; a plugin restart does not revive an old provider
   tab's OAuth state.

## Per-attempt login

3. Verify that the login email matches the queued row. Enter the email only if
   needed and choose email-code login if offered. Keep the job ID and attempt
   token from `phoenixctl repair` in variables. When the provider shows its
   code form, run `phoenixctl code JOB ATTEMPT`, parse the one fresh code into a
   variable, and fill OpenAI's Code field through the live DOM **value
   property**; do not use the HTML `value` attribute or `innerText`, which can
   be empty for an input. Recheck the attempt token immediately before
   submission so a stale code cannot advance another row. Never print codes,
   credentials, mailbox contents, or OAuth URLs.
4. Prefer the saved workspace display name for this seat when it is shown. If
   the provider exposes a workspace/account ID, Phoenix will use it as a
   stronger hint; you do not need to compute or prove the `seat-XXXXXXXX`
   label before every click. Never select **Personal**.
5. If the saved name and opaque ID are absent, choose a non-Personal workspace
   choice and Continue. Phoenix validates the callback and quota against the
   queued account, remembers the visible name after a successful repair, and
   presents the next choice on a fresh attempt when the first choice was not
   the account needing repair. Keep working through the choices until Phoenix
   marks the queued row `repaired`; do not stop merely because the first choice
   was not the match.

## Provider and challenge handling

6. If the page visibly contains `Authentication Error`, `Your session has
   ended`, or an authentication-error body with an `error_code`, stop entering
   fields. Regular Chrome does not send page errors back to Phoenix: the
   queue may remain `awaiting_user` until timeout. If the visible page is a
   confirmed provider rejection, recheck the saved job ID/attempt token and
   queued email/seat, then run `phoenixctl skip JOB ATTEMPT` once only when
   Phoenix has not already advanced it. Wait for Phoenix to mark that row skipped with
   its quarantine preserved and advance to the next login. Do not mark it
   repaired or retry it in a loop.
   If the skip command is rejected, report the installed plugin mismatch; do
   not repeat `phoenixctl repair` on the same deactivated row. Phoenix automatically marks
   recognized provider rejections and bounded login timeouts as skipped when
   it owns the browser attempt, and ordinary future Revive runs exclude all
   previously skipped identities. An explicit recovery operation may revisit
   a skipped row only when the owner asks for that exact retry.
7. For an unknown complete page, CAPTCHA, anti-bot challenge, recipient
   mismatch, or non-email MFA, stop and ask the user to complete the visible
   challenge in regular Chrome. Keep the tab available. Do not
   relabel an unrecognized page as a provider error or success.
8. Agent Autofill is the only source for every attempt. If it is unavailable or
   cannot return a fresh recipient-scoped code, stop and repair that connector;
   do not open Thunderbird or scan a desktop mailbox. Use only a newly arrived
   code. Stop on a repeated failure or the five-minute attempt timeout; the
   queue state, not a closed tab or a later-arriving email, is authoritative.
   Keep using the same login
   tab after submitting the code; do not reopen the OAuth URL in a new tab,
   because the provider's in-progress session can be tab-scoped. If a tool
   call boundary is needed, keep that exact tab as a handoff and reclaim it.
   After clicking Continue, verify that OpenAI left the code form before
   reading workspace controls; if the code form remains, do not submit the
   same code again. In regular Chrome
   agent mode, Phoenix records a bounded timeout as skipped without a new
   quarantine marker and continues with later queued seats.

### Deactivation appeal

If the provider notice says the account was deleted or deactivated, or the
login page returns `error_code: account_deactivated`, do not retry OAuth for
that row. Keep the row skipped and unresolved; automatic-mode quarantine is
preserved when it already exists, while regular Chrome agent mode does not add
a quarantine marker. Then use the official notice from Agent Autofill (or its
   matching Agent Autofill source) when the owner asks to appeal:

1. Verify the notice recipient matches the queued email and record only the
   case ID, notice date, and provider-supplied User ID needed for the appeal.
2. Follow the notice's **Initiate appeal** link. If it is unavailable, use
   `https://openai.com/form/appeal/` or the Help Center chat. Never invent a
   support address or send the appeal to an unverified recipient.
3. Select **Appeal a warning or account deactivation**, choose the affected
   product that matches the queue (usually **Codex** here), and state only
   verified facts: ownership, authorized use, the notice date/case ID, and
   any remediation already completed. Do not include credentials, codes,
   OAuth URLs, or unrelated mailbox contents.
4. Show the owner the exact destination and completed appeal text. Submitting
   the form or sending an appeal is a high-impact communication; wait for the
   owner's confirmation immediately before the final action.
5. After confirmation, submit once and verify OpenAI's confirmation page (or
   the provider's explicit sent acknowledgement). Keep the Phoenix row
   unresolved/skipped until a later repair run independently marks it
   `repaired`; an appeal submission is not a repair result.

## Completion and resume evidence

9. Let Phoenix validate the callback, credential availability, replacement
   propagation, and quota. A row is repaired only when Phoenix marks that
   queued row `repaired` (or reconciles an already healthy replacement) and the
   job advances. A skipped row remains unresolved; continue with every changed
   attempt, even when the same email owns multiple seats.
10. At the end, read the dashboard's sanitized terminal state and report all of:
    total queued, repaired, skipped, failed/cancelled, quarantined, and the
    reason for every unresolved row. Distinguish `healthy` inventory counts from rows
    actually repaired in this run; never report `12 healthy` as proof that all
    queued repairs completed. If a terminal pass still shows other invalid
    seats outside the skipped queue, click `#revive` once to start the next
    current-inventory queue. Verify its first email/seat differs from the
    skipped account before opening login. Keep the skipped row unresolved for
    a later owner-requested recovery. Stop on dashboard completion or when a row needs
    the user's/provider's intervention.
11. On a later resume, verify the same email and queued row again. A quarantined
    row must not be quarantined a second time. If Phoenix sees a healthy
    replacement for that private workspace, allow reconciliation to finish
    without another OAuth login; otherwise perform a fresh workspace attempt.

If a previous run ended in `oauth_timeout` but the email arrived after the
attempt, treat that as a synchronization-timing failure. Verify the Agent
Autofill source and its Keychain-paired local service first; local services do
not need a per-attempt grant. Restart CPA once if needed, and resume the
preserved queue; do not click Revive twice or invent a second browser queue.

## Keep token use low

- Reuse handles/locators; one snapshot per navigation, then targeted DOM reads.
- Batch known actions. Return compact status, not full DOM, inboxes, URLs, or screenshots.
- Check code/status every 10 seconds, at most six times per wait. Keep updates brief.
- On CAPTCHA, hand the check to the user and keep the login tab available. Stop
  if it loops. Check whether manual code entry already advanced the page before
  attempting another action.
