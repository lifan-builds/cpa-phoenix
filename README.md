# CPA Phoenix

CPA Phoenix is a small CLIProxyAPI plugin with exactly two management actions:

* **Ignite Fresh Accounts** sends one compact request through each currently
  fresh, eligible Codex seat, once per quota cycle.
* **Revive Invalid Accounts** guides a sequential native CPA OAuth login for
  each physically stored Codex record classified as HTTP 401 or invalid.

The repair flow decodes one native HTML entity layer, rejects residual
`amp;` query keys, and applies a bounded Butler-compatible prefill: it removes
only existing `login_hint`/`prompt` fields, appends the queued email as an
encoded `login_hint` plus `prompt=login`, and preserves every other native
query segment, order, and escape. It never controls an existing browser
profile, and leaves MFA/CAPTCHA/account verification to the user. When several
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
`127.0.0.1:1455`. During login, Phoenix can display a fresh matching OpenAI
verification code from the account's Thunderbird mailbox; entering a code
manually follows the same repair flow.

This source targets CPA plugin ABI v1 and Go 1.21. It has no scheduler, usage
handler, pricing downloader, analytics, account picker, or background timer;
there is no automatic trigger. The source is version 0.1.0. Installation,
authentication, and upstream traffic remain explicit operator-controlled
actions and are never initiated by a source build.

Fully unattended completion is intentionally a future item: Phoenix does not
bypass MFA, CAPTCHA, login prompts, or an existing browser profile.

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
