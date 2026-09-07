---
name: phoenix-repair
description: Repair invalid CPA Phoenix workspace accounts in regular Chrome using Thunderbird email codes. Use for Phoenix login repair or resuming its repair queue.
---

# Phoenix repair

Use native Chrome tools in the user's regular profile, not Phoenix's automated Chrome.
No subagents, source review, build, or service restart for routine repairs.

1. Open `http://127.0.0.1:8317/management.html#/plugin-pages/cpa-phoenix/0`.
   Inside the Phoenix iframe, check `#agent-mode`, then click `#revive`.
   Resume existing agent work; don't race a plugin-owned browser queue.
   Stop if nothing needs repair. Missing checkbox means the plugin needs updating.
2. Read only `#status`, `#current-login` text/href, and
   `#agent-login[data-attempt]`. Open the current login URL in regular Chrome
   once per attempt; retain variables rather than printing URLs.
3. Verify the login's email matches the link. Enter email if needed; choose
   email-code login if offered. Read the displayed `#verification-code` value
   into a variable and fill OpenAI's Code field, then Continue. Recheck the
   attempt before submission. Phoenix fetches mail. Never print codes or credentials.
4. Match the workspace to the link's `seat-XXXXXXXX`: it is `seat-` plus the
   first eight lowercase hex characters of SHA-256 of the trimmed workspace
   ID. Read visible radio values (or `data-workspace-id` / `data-account-id`)
   from the login DOM and compute the labels locally. Click the exact match,
   then Continue. A disabled but checked exact match only needs Continue.
   Don't guess or select Personal; missing IDs require user selection.
5. Let Phoenix validate the callback and quota. A closed login tab alone is
   not success. Repeat for each changed attempt, even with the same email/seat.
   Stop on dashboard completion; report repaired/total and failures.

## Keep token use low

- Reuse handles/locators; one snapshot per navigation, then targeted DOM reads.
- Batch known actions. Return compact status, not full DOM, inboxes, or screenshots.
- Check code/status every 10 seconds, at most six times. Keep updates brief.
- If mail is missing, open Thunderbird once to synchronize the matching
  mailbox. Resend at most once; use only a newly arrived code. Stop on repeated
  failure or the five-minute attempt timeout.
- On CAPTCHA, hand the check to the user and keep the login tab available.
  Stop if it loops. Check whether manual code entry already advanced the page.
