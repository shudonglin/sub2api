# AGENTS.md

Instructions for AI coding agents (Claude Code, Codex, Cursor, etc.) working
in this repository. See [CLAUDE.md](./CLAUDE.md) for the full guide.

## Project

Sub2API — AI API gateway. Go backend (`backend/`) + Vue 3 frontend
(`frontend/`). Built on [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api)
(LGPL-3.0). **This repository is public.**

## 🔒 Security rules — MANDATORY

**NEVER commit secrets or environment files.**

- **NEVER stage, commit, or `git add` any `.env` file.** Only `*.env.example`
  files (placeholders, no real values) belong in git. The real `.env` is
  git-ignored and must stay untracked.
- **NEVER hardcode** API keys, tokens, passwords, database/connection URLs,
  or account IDs anywhere — source, configs, CI workflows, Dockerfiles.
  Credentials are injected at runtime via environment variables only.
- **NEVER print or echo secret values** into commits, logs, or PRs.
- Before committing, verify `git diff --cached --name-only` has no `.env` file.
- Enable the guard hook once per clone: `git config core.hooksPath .githooks`.
- CI fails the build on any tracked `.env` or detected secret.

If a secret seems required to proceed, stop and ask the user. Never commit it.

## Build & test

```bash
make build      # backend + frontend
make test       # all tests
make secret-scan
```
