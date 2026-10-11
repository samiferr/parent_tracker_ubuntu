# 06 – Security, Privacy & Legal

> **Framing.** This is a parent monitoring their **minor child** on a PC the
> parent owns, administers, and is responsible for. That is generally lawful in
> many jurisdictions. Even so, the design chooses **transparency over stealth**:
> the child knows tracking exists and can see their own data. This reduces
> conflict, is more ethical, and avoids the product becoming generic spyware.

## 6.1 Principles

1. **Transparent by default.** First run shows a plain-language disclosure; the
   child is informed. No "hidden mode".
2. **Least collection.** Collect the minimum needed to answer the parent's
   question; high-sensitivity data is off by default.
3. **Least privilege.** The daemon holds root only where required; most work
   runs as a dedicated user.
4. **Local by default.** No cloud, no telemetry, no phone-home.
5. **Data subject's dignity.** The child can view their data, and reports are
   factual, not punitive.
6. **Reversible.** Everything can be exported and deleted.

## 6.2 Consent & disclosure requirements

| ID | Requirement |
| --- | --- |
| SEC-1 | On install/enable, the admin must acknowledge a disclosure notice. |
| SEC-2 | A visible, non-removable notice is available in the child's session (e.g., an entry in the system menu / a desktop notification at login) stating that activity is recorded and where to view it. |
| SEC-3 | The child can open a **read-only self-view** of their recorded data. |
| SEC-4 | Documentation states clearly what is and is not collected. |
| SEC-5 | Age-appropriate explainer text is included. |

## 6.3 Access & authentication

| ID | Requirement |
| --- | --- |
| SEC-10 | Dashboard binds to `127.0.0.1` by default; LAN exposure requires an explicit change and a warning. |
| SEC-11 | Dashboard requires authentication (password, and ideally TOTP). Credentials stored salted+hashed (argon2/bcrypt). |
| SEC-12 | CLI admin commands require root/sudo or membership in an admin group. |
| SEC-13 | Agent↔daemon socket authenticated by peer credentials (SO_PEERCRED) and restricted to monitored UIDs. |
| SEC-14 | Any change to monitored-user list or collector enablement is written to `audit_log`. |

## 6.4 Data protection

| ID | Requirement |
| --- | --- |
| SEC-20 | DB, config, and export files are mode `0600`/`0640`, owned by the service user/root. |
| SEC-21 | Optional at-rest encryption of the DB and screenshots (key stored in root-only file or keyring). |
| SEC-22 | No secrets (SMTP passwords, tokens) in world-readable files or logs. |
| SEC-23 | Logs redact window titles / URLs when the corresponding collector is privacy-restricted. |
| SEC-24 | Exports are generated with restrictive permissions and a clear warning when moved. |

## 6.5 Tamper resistance & tamper evidence

Scope note: a user with root can ultimately disable anything; we aim for
**evidence**, not impossibility.

| ID | Requirement |
| --- | --- |
| SEC-30 | If the service is stopped/disabled or a collector is turned off, that is logged and surfaced in the parent dashboard (e.g., "service was stopped at 15:04"). |
| SEC-31 | The monitored user's own account cannot edit config, stop the service, or uninstall the package without admin rights. |
| SEC-32 | A "tracking gap" is explicitly visible rather than silently missing. |
| SEC-33 | Optional integrity hash of recent events to detect tampering with the DB. |
| SEC-34 | The service re-enables itself after package upgrades; disabling is a deliberate admin action. |

## 6.6 Privacy controls for the child

| ID | Requirement |
| --- | --- |
| SEC-40 | The child can view exactly what is stored about them. |
| SEC-41 | The child can request deletion; the admin performs it (documented flow). |
| SEC-42 | Sensitive collectors (screenshots, full URLs, search terms) require explicit admin opt-in and are visibly indicated. |

## 6.7 Threat model (lightweight)

| Threat | Mitigation |
| --- | --- |
| Child reads/modifies DB | File permissions; DB owned by root/service account; audit log. |
| Child disables service | Requires sudo; disable events logged and shown to parent. |
| Other LAN devices access dashboard | Loopback default; auth required; explicit opt-in to LAN. |
| Data exfiltration via export | Restrictive file perms; warning. |
| Malicious local user spoofs agent | SO_PEERCRED check; socket root-owned. |
| Physical access / disk theft | Optional encryption at rest. |

## 6.8 Legal considerations (informational, not legal advice)

- **Parental authority:** Monitoring a minor child on a device you own is
  widely permissible; rules differ by country/state.
- **Other users:** Never monitor adult household members or guests without
  their consent. The allowlist design enforces this technically.
- **Data protection regimes:** GDPR/UK GDPR apply to personal data; even for a
  family, keep retention minimal, secure it, and honour access/deletion.
- **School/work devices:** Do **not** install on devices you don't own/control.
- Recommendation: include a short "what the law expects" appendix in user docs
  and keep this project family-scoped.

## 6.9 Anti-abuse statement

The project documentation must state plainly that it is intended for parents
monitoring their own minor children on their own devices, and that it must not
be used to covertly surveil adults or devices the administrator does not own.
This is both ethical and protective for the maintainer.
