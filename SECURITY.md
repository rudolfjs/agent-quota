# Security Policy

## Supported versions

Security fixes are made on `main` and released through GitHub Releases. This
project supports the latest published release.

| Version | Supported |
| ------- | --------- |
| Latest release | Yes |
| Older releases | No |

Reports that affect older releases are still welcome, but fixes will normally
target the latest release.

## Reporting a vulnerability

Please report security vulnerabilities through GitHub's private vulnerability
reporting flow:

<https://github.com/rudolfjs/agent-quota/security/advisories/new>

Do not open a public GitHub issue, pull request, discussion, or comment with
exploit details or sensitive information. Use the GitHub advisory thread to
share reproduction steps, affected versions, impact, and any suggested fix.

After a report is submitted, maintainers will triage it in GitHub, coordinate
the fix in the private advisory, and publish a release and advisory when
appropriate.

## Automated security checks

This repository uses GitHub security tooling, including Dependabot and CodeQL.
Automated alerts and externally reported vulnerabilities should be handled
through GitHub's security features rather than public issues.

## Scope

Security-relevant areas include credential handling, token storage and
redaction, command execution, update and install flows, GitHub Actions
workflows, and dependency vulnerabilities.

This project does not operate a bug bounty program.
