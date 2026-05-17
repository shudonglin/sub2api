# Sub2API Docker Image

Sub2API is an AI API Gateway Platform for distributing and managing AI product subscription API quotas.

## Quick Start

```bash
docker run -d \
  --name sub2api \
  -p 8080:8080 \
  -e DATABASE_URL="postgres://user:pass@host:5432/sub2api" \
  -e REDIS_URL="redis://host:6379" \
  weishaw/sub2api:latest
```

## Docker Compose

```yaml
version: '3.8'

services:
  sub2api:
    image: weishaw/sub2api:latest
    ports:
      - "8080:8080"
    environment:
      - DATABASE_URL=postgres://postgres:postgres@db:5432/sub2api?sslmode=disable
      - REDIS_URL=redis://redis:6379
    depends_on:
      - db
      - redis

  db:
    image: postgres:15-alpine
    environment:
      - POSTGRES_USER=postgres
      - POSTGRES_PASSWORD=postgres
      - POSTGRES_DB=sub2api
    volumes:
      - postgres_data:/var/lib/postgresql/data

  redis:
    image: redis:7-alpine
    volumes:
      - redis_data:/data

volumes:
  postgres_data:
  redis_data:
```

## Environment Variables

| Variable | Description | Required | Default |
|----------|-------------|----------|---------|
| `DATABASE_URL` | PostgreSQL connection string | Yes | - |
| `REDIS_URL` | Redis connection string | Yes | - |
| `PORT` | Server port | No | `8080` |
| `GIN_MODE` | Gin framework mode (`debug`/`release`) | No | `release` |

## Supabase / Hosted PostgreSQL

If you are using Supabase, Neon, Amazon RDS, Google Cloud SQL, or any other hosted
PostgreSQL provider, you can use a single `DATABASE_URL` connection string instead
of specifying individual `DATABASE_HOST`, `DATABASE_PORT`, etc. environment variables.

### Getting the connection string (Supabase)

1. Open the Supabase Dashboard for your project.
2. Go to **Settings > Database > Connection string > URI**.
3. Copy the URI. It looks like:
   ```
   postgresql://postgres.[PROJECT-REF]:[YOUR-PASSWORD]@aws-0-[REGION].pooler.supabase.com:6543/postgres
   ```
4. Append `?sslmode=require` if it is not already present.

### Using docker-compose.supabase.yml

```bash
cd deploy
cp .env.supabase.example .env
# Edit .env and paste your DATABASE_URL
docker compose -f docker-compose.supabase.yml up -d
```

This compose file runs only Sub2API + Redis (no local PostgreSQL container).

### Connection pooling notes

| Port | Mode | When to use |
|------|------|-------------|
| `6543` | Supabase connection pooler (Transaction mode) | Recommended for most workloads |
| `5432` | Direct connection | Needed for migrations or long-lived connections |

When `DATABASE_URL` is detected the application automatically:
- Sets `sslmode=require` (if not specified in the URL).
- Lowers pool defaults to `max_open_conns=20`, `max_idle_conns=10` to respect
  hosted-DB connection limits.
- You can override these via `DATABASE_MAX_OPEN_CONNS` / `DATABASE_MAX_IDLE_CONNS`
  environment variables.

### SSL requirement

All hosted PostgreSQL providers require SSL. Make sure your connection string
includes `?sslmode=require`. The `sslmode=disable` option is only appropriate
for connections inside a Docker network to a co-located PostgreSQL container.

## Supported Architectures

- `linux/amd64`
- `linux/arm64`

## Tags

- `latest` - Latest stable release
- `x.y.z` - Specific version
- `x.y` - Latest patch of minor version
- `x` - Latest minor of major version

## Links

- [GitHub Repository](https://github.com/weishaw/sub2api)
- [Documentation](https://github.com/weishaw/sub2api#readme)
