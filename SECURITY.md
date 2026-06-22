# Security Policy

`toaster-ready` runs against **untrusted repositories** — it clones and reads
arbitrary repos and parses their contents. Security reports are taken seriously.

## Reporting a vulnerability

Please **do not** open a public issue for security problems. Instead, use GitHub's
[private vulnerability reporting](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability)
on this repository ("Report a vulnerability" under the Security tab).

Please include a description, reproduction steps, and the affected version/commit.
We aim to acknowledge within a few days.

## Scope & design notes

- The scoring core is **deterministic and offline-capable**: no code from the scored
  repo is executed, and `toaster gate` runs without network or secrets.
- File access is **confined to the scored repository root** (path-traversal attempts via
  `@path` imports or otherwise are rejected).
- Remote scans shallow-clone over HTTPS with a timeout; the file walk is bounded.
- The secret scanner reports only **locations** (path + line + rule), never the matched
  value.

If you find a way around any of the above, that's exactly what we want to hear about.
