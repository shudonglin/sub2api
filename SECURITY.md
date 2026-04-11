# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in this project, please report
it privately via GitHub's "Report a vulnerability" button on the
Security tab, or by opening a private security advisory.

**Do NOT open a public issue** for security-related concerns.

We aim to:
- Acknowledge reports within 48 hours
- Issue a fix within 7 business days for high/critical severity issues
- Credit reporters in the release notes (unless you prefer to remain anonymous)

## Supported Versions

Only the latest release on the `main` branch is actively maintained.
Security patches are issued against the latest version.

## Security Features

This repository has the following security features enabled:
- Dependabot alerts and security updates
- Secret scanning with push protection
- Static analysis via govulncheck (Go) and pnpm audit (frontend)
- Secret detection via gitleaks on every push

## Responsible Disclosure

We follow responsible disclosure. Please allow us reasonable time to
investigate and patch before making any vulnerability public.
