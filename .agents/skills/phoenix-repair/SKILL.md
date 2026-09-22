---
name: phoenix-repair
description: Repair invalid CPA Phoenix workspace accounts in regular Chrome using Thunderbird email codes. Use for Phoenix login repair or resuming its repair queue.
---

# Phoenix repair

Use native Chrome tools in the user's regular profile, not Phoenix's automated Chrome.
Phoenix owns the queue, quarantine, callback, replacement detection, and quota
validation; the skill only operates the visible login page. Never claim a row
is repaired from a closed tab, a healthy account total, or a successful-looking
provider page.

No subagents, source review, build, or service restart for routine repairs. If
the dashboard is missing the controls or behavior documented here, stop and
report that the installed plugin needs updating instead of starting a second
repair path.

## Preflight and queue ownership

1. Open `http://127.0.0.1:8317/management.html#/plugin-pages/cpa-phoenix/0`.
   Inside the Phoenix iframe, read `#agent-mode`, enable it, and click
   `#revive`. That same action starts a new queue or resumes an incomplete
   queue; do not invent a separate browser queue or click it twice while a job
   is active. If the queue is empty and the invalid count is zero, stop. A
   missing checkbox means the installed plugin needs updating.
2. Read only `#status`, `#current-login` text/href,
   `#agent-login[data-attempt]`, and the visible repair-queue rows. Keep login
   URLs, attempt identifiers, and codes in variables; never print them. Open
   the current login URL in regular Chrome once per attempt. Phoenix also
   brings Thunderbird forward at the start of each attempt so the matching
   mailbox can synchronize before Phoenix reads its local mail files. If an
   existing attempt is already displayed, resume that attempt instead of
   starting another one.

## Per-attempt login

3. Verify that the login email matches the queued row. Enter the email only if
   needed and choose email-code login if offered. Phoenix fetches the code from
   Thunderbird. Read the displayed `#verification-code` into a variable, fill
   OpenAI's Code field, and Continue. Recheck `#agent-login[data-attempt]`
   immediately before submission so a stale code cannot advance another row.
   Never print codes, credentials, mailbox contents, or OAuth URLs.
4. Match the workspace to the link's `seat-XXXXXXXX`: compute `seat-` plus the
   first eight lowercase hex characters of SHA-256 of the trimmed workspace ID.
   Read visible radio values (or `data-workspace-id` / `data-account-id`) from
   the login DOM and compute the labels locally without exposing the full ID.
   Click only the exact match, then Continue. A disabled but checked exact match
   only needs Continue. Never select Personal or another seat.
5. If the exact seat is absent from the visible choices, stop that attempt.
   Do not select the closest label, Personal, or a seat from another queue row.
   Cancel only the current native OAuth attempt if the page offers a safe
   cancel; otherwise leave the login window open and report the missing seat.
   The quarantined row is intentionally resumable, so pause the queue and wait
   for the correct workspace to become available rather than retrying blindly.

## Provider and challenge handling

6. If the page visibly contains `Authentication Error`, `Your session has
   ended`, or an authentication-error body with an `error_code`, stop entering
   fields. Phoenix should report the allowlisted `provider_auth_error`, cancel
   that native OAuth session, and preserve the row's quarantine marker. Return
   to the dashboard and report that OpenAI rejected the session; do not mark a
   replacement repaired and do not loop retries. A later explicit click of
   `#revive`/resume starts a fresh attempt after the user has completed any
   required provider sign-in.
7. For an unknown complete page, CAPTCHA, anti-bot challenge, recipient
   mismatch, or non-email MFA, stop and ask the user to complete the visible
   challenge in Phoenix's Chrome window. Keep the tab available. Do not
   relabel an unrecognized page as a provider error or success.
8. Thunderbird synchronization is part of every attempt: a Thunderbird
   process that was started once in the background is not proof that the
   current recipient's mailbox is fresh. Phoenix's updated plugin brings it
   forward per attempt; if the code is still missing, wait for synchronization
   and resend at most once. Use only a newly arrived code. Stop on a repeated
   failure or the five-minute attempt timeout; the queue state, not a closed
   tab or a later-arriving email, is authoritative.

## Completion and resume evidence

9. Let Phoenix validate the callback, exact workspace identity, credential
   availability, replacement propagation, and quota. A row is repaired only
   when Phoenix marks that exact queue row `repaired` (or reconciles an already
   healthy replacement) and the job advances. Repeat for every changed attempt,
   even when the same email owns multiple seats.
10. At the end, read the dashboard's sanitized terminal state and report all of:
    total queued, repaired, failed/cancelled, quarantined, and the reason for
    every unresolved row. Distinguish `healthy` inventory counts from rows
    actually repaired in this run; never report `12 healthy` as proof that all
    queued repairs completed. Stop on dashboard completion or when a row needs
    the user's/provider's intervention.
11. On a later resume, verify the same email and exact `seat-XXXXXXXX` again.
    A quarantined row must not be quarantined a second time. If Phoenix sees a
    healthy replacement for that private workspace, allow reconciliation to
    finish without another OAuth login; otherwise perform a fresh attempt.

If a previous run ended in `oauth_timeout` but the email appeared in
Thunderbird afterward, treat that as a synchronization-timing failure. Verify
the installed plugin includes per-attempt Thunderbird activation, restart CPA
once if needed, and resume the preserved queue; do not click Revive twice or
invent a second browser queue.

## Keep token use low

- Reuse handles/locators; one snapshot per navigation, then targeted DOM reads.
- Batch known actions. Return compact status, not full DOM, inboxes, URLs, or screenshots.
- Check code/status every 10 seconds, at most six times per wait. Keep updates brief.
- On CAPTCHA, hand the check to the user and keep the login tab available. Stop
  if it loops. Check whether manual code entry already advanced the page before
  attempting another action.
