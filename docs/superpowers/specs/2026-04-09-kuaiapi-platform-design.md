# KuaiAPI Platform Design

## Overview

KuaiAPI (kuaiapi.xyz) is a consumer-facing AI API gateway service built on top of Sub2API. It provides access to 219 AI models from 4 providers (Anthropic, OpenAI, Google, DeepSeek) through a single OpenAI-compatible endpoint. This spec covers the landing page, payment integration, and brand unification across all three repos.

## Brand

- **Name**: KuaiAPI ("Kuai" = 快 = fast in Chinese)
- **Domain**: kuaiapi.xyz
- **Logo**: Double Chevron speed lines — T2 Chartreuse
- **Primary color**: #ccff00 (dark mode), #65a30d (light mode, for contrast)
- **Icon background**: #1a2600
- **Font**: Inter

## Architecture

```
┌─────────────────────────┐  ┌─────────────────────────┐  ┌─────────────────────────┐
│  sub2api-landing-page    │  │       sub2api            │  │      sub2apipay          │
│  Marketing + Docs        │  │  API Gateway + Admin     │  │  Payment Gateway         │
│                          │  │                          │  │                          │
│  Next.js + Tailwind      │  │  Go + Vue 3 + Tailwind   │  │  Next.js + Prisma        │
│  Cloudflare Pages        │  │  VM (Singapore)          │  │  VM (Singapore)          │
│  kuaiapi.xyz             │  │  api.kuaiapi.xyz         │  │  pay.kuaiapi.xyz         │
│  No database             │  │  Supabase PostgreSQL     │  │  Prisma Postgres (SG)    │
└─────────────────────────┘  └─────────────────────────┘  └─────────────────────────┘
```

### Subdomain mapping

| Subdomain | Repo | Purpose |
|---|---|---|
| `kuaiapi.xyz` | sub2api-landing-page | Marketing site, docs, model catalog |
| `api.kuaiapi.xyz` | sub2api | API gateway, admin dashboard, user dashboard |
| `pay.kuaiapi.xyz` | sub2apipay | Payment, top-up, subscription purchase |

### Database strategy

| App | Database | Rationale |
|---|---|---|
| sub2api | Supabase PostgreSQL (existing, Singapore) | High query volume, unlimited queries on free tier, already configured |
| sub2apipay | Prisma Postgres (new, Singapore ap-southeast-1) | Low volume payment system, 100K ops/month free tier is sufficient, native Prisma ORM fit, isolated from gateway data |

No table name collisions exist between the two apps — verified. sub2api uses Ent ORM with tables like `users`, `accounts`, `api_keys`. sub2apipay uses Prisma with tables like `orders`, `audit_logs`, `channels`.

## Landing Page (sub2api-landing-page)

### Tech stack

- **Framework**: Next.js (static export via `output: 'export'`)
- **Styling**: Tailwind CSS
- **Hosting**: Cloudflare Pages
- **Repo**: /Users/shudongl/HubLogic/HanHan/sub2api-landing-page

### Layout: Hybrid (Layout C)

Compact hero with code snippet on left, model pricing table with provider filter tabs below.

### Pages

| Route | Content |
|---|---|
| `/` | Hero + code snippet + model table + features + getting started |
| `/docs` | Getting started guide, API reference, endpoint documentation |
| `/pricing` | Pricing plans or pay-as-you-go rate explanation |
| `/models` | Full searchable model catalog with client-side filtering |

### Page sections (homepage)

1. **Nav**: KuaiAPI logo, links (Models, Pricing, Docs, Dashboard), "Get API Key" CTA
2. **Hero** (split layout): Left — headline, subtitle, dual CTAs (Get Free Key + Docs). Right — Python code snippet showing OpenAI-compatible usage
3. **Provider filter tabs** + **Model pricing table**: Tabs for All (219), Anthropic (27), OpenAI (122), Google (67), DeepSeek (3). Table columns: Model, Input $/M tokens, Output $/M tokens
4. **Features grid**: Unified API, Low Latency (Singapore), Pay-as-you-go, 219 Models
5. **Getting started**: 3-step flow (Get API key → Install SDK → Make first request)
6. **Pricing section**: Plans or pay-as-you-go breakdown
7. **Footer**: Links, copyright

### Model pricing data

Static JSON file sourced from sub2api's `backend/resources/model-pricing/model_prices_and_context_window.json`. Copied manually or via a build script. The model table on the landing page is rendered client-side with search and filter capabilities.

## Theming

Both light and dark themes across all three repos. System preference detection with manual toggle, persisted in localStorage.

### Color tokens

| Token | Dark mode | Light mode |
|---|---|---|
| `--brand` | #ccff00 | #65a30d |
| `--bg` | #0a0a0a | #ffffff |
| `--surface` | #111111 | #f8fafc |
| `--border` | #1e293b | #e2e8f0 |
| `--text` | #e2e8f0 | #0f172a |
| `--text-muted` | #94a3b8 | #64748b |

### Color sync across repos

**sub2api (Vue frontend)**: Update `tailwind.config.js` primary palette from teal (#14b8a6) to lime/chartreuse. Replace gradient-primary. Swap logo to KuaiAPI Double Chevron SVG.

**sub2apipay (Next.js)**: Add brand color tokens to `globals.css` CSS variables. Extend existing `data-theme='dark'` system. Swap logo.

**sub2api-landing-page (Next.js)**: Built from scratch with the full brand palette. Same framework as sub2apipay for consistency.

### Logo SVG (shared across all repos)

Double Chevron icon on dark background (#1a2600). Chartreuse chevrons (#e5ff4d lighter, #ccff00 main). Wordmark: "Kuai" in white/dark + "API" in brand color.

## Payment Integration

sub2apipay is already designed to integrate with sub2api via iframe embedding and admin API calls. The integration path:

1. sub2apipay is deployed at `pay.kuaiapi.xyz`
2. sub2api admin settings point the payment iframe URL to `pay.kuaiapi.xyz`
3. sub2apipay calls sub2api's admin API (`SUB2API_BASE_URL` + `SUB2API_ADMIN_API_KEY`) to credit user balances after successful payment
4. Landing page "Top Up" links point to `pay.kuaiapi.xyz`

### Environment variables for sub2apipay

```
SUB2API_BASE_URL=https://api.kuaiapi.xyz
SUB2API_ADMIN_API_KEY=<admin-key>
ADMIN_TOKEN=<pay-admin-token>
NEXT_PUBLIC_APP_URL=https://pay.kuaiapi.xyz
DATABASE_URL=<prisma-postgres-connection-string>
```

## User flow

1. User visits `kuaiapi.xyz` → sees hero, model catalog, pricing
2. Clicks "Get API Key" → redirected to `api.kuaiapi.xyz` → registers/logs in
3. Gets API key from dashboard
4. Needs credits → clicks "Top Up" in dashboard → iframe loads `pay.kuaiapi.xyz`
5. Completes payment → sub2apipay credits balance via sub2api admin API
6. User makes API calls to `api.kuaiapi.xyz/v1/chat/completions` with their key
