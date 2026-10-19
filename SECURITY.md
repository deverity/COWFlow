# Security Policy

## Supported versions

COWFlow is pre-1.0. Only the latest released version receives security fixes.

| Version | Supported |
|---|---|
| latest `v0.x` release | ✅ |
| older releases | ❌ |

## Reporting a vulnerability

**Do not open a public issue, discussion, or pull request for security problems.**

Report privately through GitHub:
**[Report a vulnerability](https://github.com/deverity/COWFlow/security/advisories/new)**
(Security tab → "Report a vulnerability").

Please include:

- affected version or commit;
- operating system and filesystem (e.g. Windows 11 Dev Drive/ReFS, Linux btrfs);
- steps to reproduce or a proof of concept;
- impact as you understand it.

## What to expect

| Step | Target |
|---|---|
| Acknowledgement | within 3 business days |
| Initial assessment | within 7 days |
| Fix or mitigation plan | within 30 days for high/critical issues |
| Public disclosure | coordinated with the reporter, at most 90 days after the report |

Fixed vulnerabilities are published as GitHub Security Advisories, with a CVE
when applicable, and credited to the reporter unless they prefer otherwise.

## Scope

In scope: the `cowflow` CLI and daemon, the local HTTP API, the MCP server,
workspace isolation, token handling, and release artifacts.

Especially relevant: escaping a workspace's confined paths, using a session
token outside its scope, the local API being reachable beyond loopback,
command injection via task/branch names, and secrets leaking into published
changes or logs.

Out of scope: vulnerabilities in third-party agent runners (report them
upstream) and issues that require an already-compromised local user account.
