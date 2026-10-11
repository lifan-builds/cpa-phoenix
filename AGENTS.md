# CPA Phoenix guidance

- Keep OAuth repair and account registration separate; repair is queue-authoritative and must preserve the SHA-256 seat identity and localhost:1455 callback contract.
- Preserve browser/mail integration boundaries, explicit target and owner scope, and evidence of each repair step. Do not expose credentials, tokens, mail contents, or browser state.
- Run focused deterministic checks for changed behavior and report live OAuth or mailbox checks separately.
