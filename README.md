<div align="center">

# 🔥 CPA Phoenix

**Make quota resets count. Clear recurring 401s.**

Connect → Check → Fix

[CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) plugin · macOS · MIT

</div>

CPA Phoenix is the CLIProxyAPI plugin for keeping Codex workspaces ready. It
shows account health in one dashboard, warms up fresh seats after a quota reset,
and works through invalid accounts in a queue.

## The two problems it fixes

| Quota reset | Constant 401 / invalid access |
| --- | --- |
| Click **Warm up fresh accounts** to send one request through every eligible fresh seat so those accounts start counting. | Click **Fix invalid accounts** and let Phoenix loop through the queue, or choose **Repair with Agent skill** and let an Agent drive regular Chrome. |

## Install

### You need

- macOS (Darwin)
- Go 1.21
- A C compiler, such as Xcode Command Line Tools
- A local CLIProxyAPI installation with its plugin support enabled
- Google Chrome for account repair
- [Agent Autofill](https://github.com/lifan-builds/agent-autofill) for automatic email-code lookup

Agent Autofill needs Python 3.11+ and either `uv` or `pipx`. It also needs a
connected Gmail or IMAP source that receives the CPA login emails.

Build the plugin and install it into the folder used by CLIProxyAPI:

```bash
git clone https://github.com/lifan-builds/cpa-phoenix.git
cd cpa-phoenix
./build.sh
bash ./install.sh
```

The installer reads `plugins.dir` from the CPA config when it can find one. It
prints the exact destination and the next action. To point it at a known folder:

```bash
bash ./install.sh --plugin-dir /path/to/cli-proxy-api/plugins
```

Before copying anything, you can see what it will use:

```bash
bash ./install.sh --check
```

To print only the next action without changing files:

```bash
bash ./install.sh --print-next-step
```

If the config is not in one of the usual locations, pass it explicitly:

```bash
bash ./install.sh --config /path/to/cli-proxy-api/config.yaml
```

The config should enable plugins and point at the same folder:

```yaml
plugins:
  enabled: true
  dir: "/absolute/path/to/.cli-proxy-api/plugins"
```

Restart CLIProxyAPI after installing, then open the Phoenix dashboard:

`http://127.0.0.1:8317/management.html#/plugin-pages/cpa-phoenix/0`

The installer does not start CPA or touch accounts. It only places
`cpa-phoenix.dylib`; CPA loads it on its next start.

## Let an Agent install it

Paste this into Codex or another local coding Agent:

```text
Install CPA Phoenix into my local CLIProxyAPI installation.

Repository: https://github.com/lifan-builds/cpa-phoenix.git

Use the current directory if it is already this repository. Otherwise clone it
and work from the checkout. On macOS:

1. Confirm Go 1.21, a C compiler, and CLIProxyAPI are available.
2. Run ./build.sh.
3. Run bash ./install.sh --check and show me the resolved CPA config,
   plugin directory, and destination.
4. Run bash ./install.sh. If the destination is not the folder CPA uses,
   update `plugins.dir` in the CPA config to the installed folder. If no CPA
   config can be found, ask me for its path before installing.
5. If `agent-autofill` is missing and I want Agent repairs, clone
   https://github.com/lifan-builds/agent-autofill.git into a temporary
   checkout, install it with `uv tool install`, then run
   `agent-autofill --db ~/.agent-autofill.sqlite3 init` and `doctor`. If no
   mailbox source is connected, tell me what to connect; do not guess one.
6. Tell me the installed path and restart CLIProxyAPI. Do not start repair,
   warmup, quota protection, or account sign-in during installation.

After CPA restarts, open:
http://127.0.0.1:8317/management.html#/plugin-pages/cpa-phoenix/0
```

## Set up Agent Autofill for email codes

Phoenix starts the local Agent Autofill broker when a repair needs a code. The
`agent-autofill` command must be on your `PATH`, and its local database must
contain a mailbox source for each CPA login email.

Phoenix launches the broker as the `cpa-phoenix` local service when it needs a
code; you do not need to keep a separate server window open.

Install it from its checkout (or use `pipx install .`):

```bash
uv tool install /path/to/agent-autofill
agent-autofill --db ~/.agent-autofill.sqlite3 init
agent-autofill --db ~/.agent-autofill.sqlite3 doctor
```

For Gmail or IMAP, add the source with Agent Autofill's
[mail setup guide](https://github.com/lifan-builds/agent-autofill/blob/main/docs/agent-autofill.md),
then run `agent-autofill --db ~/.agent-autofill.sqlite3 doctor` again. Phoenix
passes the queued recipient to Agent Autofill, so you do not need to copy an
opaque mailbox ID into CPA Phoenix. Phoenix fails closed when Agent Autofill
cannot return a fresh code. The dashboard distinguishes an installed binary
from a verified broker/source (`ready`); an installed or needs-attention state
must be resolved before an Agent-mode repair.

## Repair invalid accounts

Phoenix gives you two visible choices on the repair card:

### Repair in Phoenix

Choose **Repair in Phoenix**, then click **Fix invalid accounts**. Phoenix
opens its managed Chrome window and advances through every invalid account in
the queue. When a CAPTCHA, passkey, extra MFA step, or workspace choice appears,
finish that step in the visible Phoenix window and return to the dashboard.

### Repair with Agent skill

Choose **Repair with Agent skill**, then ask your Agent:

```text
Read .agents/skills/phoenix-repair/SKILL.md and use it to repair invalid
accounts in CPA Phoenix. If your Agent supports skills, invoke it as
$phoenix-repair.
Use the selected "Repair with Agent skill" path, keep the current queue, and
work through every queued account. Use Agent Autofill for each fresh email OTP.
Pause when a CAPTCHA, passkey, unmatched workspace, or other human step needs
me; do not start a second queue.
```

The skill drives regular Chrome only for the provider OAuth page while Phoenix
owns the queue and records each result. Agent Autofill finds the matching email
OTP. A completed browser page alone is not a repair result; Phoenix's queue
records repaired, skipped, and failed accounts.

The terminal status view can show unresolved rows from more than one historical
repair job. Phoenix keeps the newest outcome for each exact private workspace;
an unrelated later repair cannot hide another missing seat. Retry one durable
job's paused or timed-out seats at a time, then refresh the inventory for the
next batch. If a terminal queue contains only recoverable timeout or
workspace-mismatch rows and you no longer want to retry them, use
`phoenixctl reset`. Phoenix marks that history
superseded without deleting its audit rows; the next repair scan can queue the
same workspaces again if they are still invalid. Provider-auth or deactivation
rows stay visible for owner review.

For a dashboard-free workflow, use the terminal client:

```bash
./phoenixctl status
./phoenixctl repair
# Optional: clear recoverable timeout/workspace-mismatch history
./phoenixctl reset
```

The client prompts for the management key without echoing it. For automation,
use `CPA_MANAGEMENT_KEY` or a protected `--key-file` instead of putting the key
in shell history.

`phoenixctl repair` polls Phoenix and opens regular Chrome only when the current
OAuth login is required. It never opens the Phoenix dashboard or a desktop mail
client.

Phoenix remembers the provider-visible workspace name after a successful repair
and uses it as a hint on later attempts. If the provider does not expose the
opaque workspace ID, choose a non-Personal workspace; Phoenix validates the
resulting credential and keeps trying the remaining choices until the queued
row is repaired. A wrong choice is never counted as success. `pause` and
`recover` remain available when a human challenge or a later retry is needed.

## First use

1. Restart CLIProxyAPI and open the dashboard URL above.
2. Enter the CPA management key when Phoenix asks for it. The key stays in
   memory for that dashboard session.
3. Let the account list load. Phoenix shows healthy, fresh, invalid, and
   unresolved queue counts.
4. After a quota reset, click **Warm up fresh accounts** so every eligible
   fresh seat starts counting.
5. For 401 or invalid accounts, choose **Repair in Phoenix** or **Repair with
   Agent skill**, then click **Fix invalid accounts** or use the prompt above.
6. Open **Optional tools** only when you want Daily warmup or quota protection.

The **Getting started** guide can be reopened from the dashboard at any time.

## Optional tools

### Daily warmup

Open **Optional tools → Daily warmup**, turn it on, choose a time and an IANA
time zone such as `America/Los_Angeles`, then save. It runs inside CPA while CPA
is running; saving the schedule does not send a request immediately.

### Quota protection

Open **Optional tools → Protect one account's quota**, choose a workspace, and
save. Phoenix watches that workspace's five-hour usage window and applies the
configured protection behavior.

## Troubleshooting

**Phoenix is not visible in CPA**

Run `bash ./install.sh --check`. Make sure the reported destination is inside
the reported `plugins.dir`, that `plugins.enabled` is on, and restart
CLIProxyAPI. If needed, install again with `--plugin-dir PATH`.

**Phoenix asks for a management key**

Enter the key shown by your CLIProxyAPI management page, then refresh the
dashboard.

**Agent Autofill cannot return a code**

Run `agent-autofill --db ~/.agent-autofill.sqlite3 doctor`. Confirm the command
is on `PATH` and that a source for the queued email is present and connected.
Phoenix stops the attempt and leaves it resumable until Agent Autofill is ready.

**A repair pauses in Chrome**

Complete the visible login, CAPTCHA, passkey, workspace selection, or MFA step,
then return to Phoenix. The current queue remains available.

**The build cannot start**

Install Go 1.21 and a macOS C compiler, then run `./build.sh` again.

## For contributors

Phoenix targets the CLIProxyAPI plugin ABI v1 and produces a Darwin shared
library named `cpa-phoenix.dylib`. The source build is the same build used by
the installation steps above.

## License

MIT. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
