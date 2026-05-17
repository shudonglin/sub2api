# CLAUDE.md

Guidance for Claude Code (and other AI agents) working in this repository.

## Project

Sub2API — an AI API gateway that distributes and manages API quotas from AI
product subscriptions. Go backend (`backend/`) + Vue 3 frontend (`frontend/`),
deployed as a single container. Built on [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api)
(LGPL-3.0); this repo carries downstream customizations.

## Common commands

```bash
make build              # build backend + frontend
make test               # backend + frontend tests
make secret-scan        # gitleaks secret scan

cd backend  && make test-unit && make test-integration
cd frontend && pnpm typecheck && pnpm test:run
```

## 🔒 Security rules — MANDATORY

**NEVER commit secrets or environment files.** This repository is public.

- **NEVER stage, commit, or `git add` any `.env` file.** The only env files
  allowed in git are `*.env.example` (placeholders only — no real values).
  Real config lives in `.env`, which is git-ignored and must stay that way.
- **NEVER hardcode** API keys, tokens, passwords, database URLs, connection
  strings, or account IDs in source, configs, workflows, or Dockerfiles.
  All credentials are supplied at runtime via environment variables.
- **NEVER print, echo, or paste secret values** into commits, logs, PR
  descriptions, or this chat.
- Before any commit, confirm `git diff --cached --name-only` contains no
  `.env` file and no credential-bearing change.
- A `pre-commit` hook enforces this — enable it once per clone:
  `git config core.hooksPath .githooks`
- CI (`.github/workflows/security-scan.yml`) runs gitleaks + a tracked-`.env`
  check and fails the build on any violation.

If you believe a secret must be added to make something work, stop and ask
the user — do not commit it.

## Conventions

- Backend: Go, Ent ORM, wire DI. Run `make generate` after schema changes.
- Frontend: Vue 3 + TypeScript + Vite + pnpm.
- Keep changes consistent with surrounding code; match existing patterns.
