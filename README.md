# CPA Phoenix

CPA Phoenix is a small CLIProxyAPI plugin with exactly two management actions:

* **Ignite Fresh Accounts** sends one compact request through each currently
  fresh, eligible Codex seat, once per quota cycle.
* **Revive Invalid Accounts** runs sequential native CPA OAuth logins for
  each physically stored Codex record classified as HTTP 401 or invalid.
  Phoenix opens its own Chrome window, enters the email and fresh Thunderbird
  verification code, selects the matching workspace, and advances the queue.

The repair flow decodes one native HTML entity layer, rejects residual
`amp;` query keys, and applies a bounded Butler-compatible prefill: it removes
only existing `login_hint`/`prompt` fields, appends the queued email as an
encoded `login_hint` plus `prompt=login`, and preserves every other native
query segment, order, and escape. It uses a dedicated Chrome profile and leaves
unrecognized challenges, CAPTCHA, and non-email MFA to the user in that window.
When several
queued workspaces share an email, Phoenix identifies the credential actually
changed by OAuth, marks the queued row with that private workspace ID repaired,
and continues prompting for the remaining workspace; the human's selection
order does not need to match the queue order. The credential must still be
available and pass a successful quota probe. Because CPA can report native
OAuth success before its plugin inventory exposes the saved record, Phoenix
waits a short bounded interval for replacement propagation. A later resume
also reconciles an already-healthy replacement without starting OAuth again.
An obsolete record is moved to owner-only quarantine before login and is not
restored automatically. The callback forwarder is temporary and binds only to
`127.0.0.1:1455`. During login, Phoenix enters a fresh matching OpenAI
verification code from the account's Thunderbird mailbox; entering a code
manually in the Chrome window follows the same repair flow.

## Automatic repair

For regular Chrome, use the repo-local
[phoenix-repair skill](.agents/skills/phoenix-repair/SKILL.md): ask Codex to
`Use $phoenix-repair to repair invalid accounts`. The skill selects **Use regular
Chrome with the Phoenix repair skill** before starting Revive. Phoenix still
owns the queue, mail-code lookup, callback, and quota validation; the agent only
operates the login pages. This mode avoids launching a second automated Chrome.
The skill lives in `.agents/skills` for repository discovery.

Install Google Chrome and configure the account mailboxes in Thunderbird with
message synchronization enabled (including downloading message bodies). Phoenix
starts Thunderbird for mail delivery and uses its existing local mail files;
it does not need your mailbox password. Click **Revive Invalid Accounts** in
the existing Management page. The plugin owns the browser and queue, so the
dashboard can be closed and no Codex agent needs to remain running.

Phoenix keeps its Chrome profile under
`~/.cli-proxy-api-state/cpa-phoenix/browser`. This is separate from your normal
Chrome profile. Workspace selection requires an exact matching workspace ID
in OpenAI's page. If the page changes, the matching workspace is absent, or a
challenge needs your help, the dashboard explains that intervention is needed;
finish that step in Phoenix's Chrome window. Successful OAuth and a quota
probe remain the authority for marking a workspace repaired. Each login
attempt has a five-minute timeout; an interrupted queue can be resumed.

This source targets CPA plugin ABI v1 and Go 1.21. It has no scheduler, usage
handler, pricing downloader, analytics, account picker, or background timer;
there is no automatic trigger. The source is version 0.1.0. Installation,
authentication, and upstream traffic remain explicit operator-controlled
actions and are never initiated by a source build.

Email-code logins can complete unattended while Thunderbird is receiving mail.
Other verification challenges may still require your interaction.

## Build and package

The checked-in scripts build the current Darwin shared-library artifact and a
release archive containing only the rebuilt library, README, MIT `LICENSE`,
and `NOTICE`:

```bash
./build.sh
./package-release.sh 0.1.0
```

`build.sh` writes `cpa-phoenix.dylib`; `package-release.sh` writes the matching
versioned zip and SHA-256 file. The scripts do not produce Linux or Windows
artifacts. Both builds trim source paths and strip debug metadata so packaged
binaries do not retain machine-local build paths. Build a matching
CPA/OS/architecture artifact separately before
installing on another platform, and never package runtime state, credentials,
OAuth data, or the SQLite database.

## Offline verification

Run `gofmt -w *.go`, `go test -count=1 ./...`, `go test -race -count=1 ./...`,
`go vet ./...`, `bash -n build.sh package-release.sh`, and parse the embedded
dashboard JavaScript before a release review. The package script creates a
Darwin shared library and release archive only; no runtime state or auth data
is included.

## Local installation

Run `./build.sh`, copy `cpa-phoenix.dylib` into CPA's configured plugin
directory for the current platform, and restart CPA. Phoenix has no background
schedule; Ignite and Revive run only from its Management page. Quarantined auth
files are not restored automatically.
