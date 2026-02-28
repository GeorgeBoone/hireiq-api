# HireIQ API

Go backend for HireIQ — an AI-powered job search intelligence platform.

*You bring the talent. We bring the intel.*

## Tech Stack

- **Language:** Go 1.22
- **Framework:** Gin
- **Database:** PostgreSQL (Cloud SQL)
- **Auth:** Firebase Authentication
- **Cloud:** Google Cloud Platform (Cloud Run)
- **AI:** Claude API (Anthropic)
- **Job Data:** JSearch API (RapidAPI)
- **Financial Data:** Yahoo Finance API

## Project Structure

```
cmd/server/          -> Entry point, server startup, route registration
internal/
  config/            -> Environment variable loading
  middleware/        -> Auth (Firebase), rate limiting, CORS
  handler/           -> HTTP request handlers
  service/           -> Business logic (AI calls, job search, finance)
  repository/        -> Database queries (SQL via pgx)
  model/             -> Domain structs
  worker/            -> Async background jobs
migrations/          -> SQL migration files
```

## Features

- **Authentication** — Firebase Google OAuth with auto user creation on first login
- **Profile Management** — Skills, salary range, work style, location preferences
- **Job Tracking** — Full CRUD for saved jobs with bookmarking and status management
- **Job Parsing** — AI-powered parsing of job posting URLs and raw text via Claude
- **Discover Feed** — AI-matched job feed from JSearch API with save/dismiss actions
- **Pipeline Tracking** — Application status tracking (saved -> applied -> interview -> offer) with status history and follow-up management
- **Resume Critique** — AI-powered resume analysis with scoring, issue detection, and fix suggestions
- **Job Comparison** — AI-driven side-by-side comparison of multiple job opportunities
- **Company Intel** — Financial profiles via Yahoo Finance (public companies) and AI estimates (private companies)
- **Network** — Company aggregation from saved jobs with contact management (CRUD)
- **Rate Limiting** — Per-user request rate limiting

## Local Development

### Prerequisites

- Go 1.22+
- PostgreSQL 15+ (local or Cloud SQL proxy)
- Firebase project with Google Auth enabled

### Setup

```bash
# 1. Clone and enter directory
git clone https://github.com/GeorgeBoone/hireiq-api.git
cd hireiq-api

# 2. Copy env file and fill in values
cp .env.example .env

# 3. Create database
createdb hireiq

# 4. Run migrations
psql $DATABASE_URL -f migrations/001_initial_schema.sql

# 5. Install dependencies
go mod download

# 6. Run server
go run ./cmd/server
```

Server starts at `http://localhost:8080`

### Verify

```bash
curl http://localhost:8080/health
```

## Deployment

Push to `main` triggers Cloud Build -> builds Docker image -> deploys to Cloud Run.

### Manual deploy

```bash
gcloud builds submit --config cloudbuild.yaml
```

## API Routes

All authenticated routes require `Authorization: Bearer <firebase-token>` header.

### Auth & Profile

| Method | Path | Description |
|--------|------|-------------|
| GET | /health | Health check (unauthenticated) |
| POST | /auth/google | Sign in / create account |
| GET | /profile | Get user profile |
| PUT | /profile | Update profile fields |
| PUT | /profile/skills | Update skills array |

### Jobs

| Method | Path | Description |
|--------|------|-------------|
| GET | /jobs | List saved jobs (with optional filters) |
| POST | /jobs | Save a job |
| GET | /jobs/:id | Get job detail |
| PUT | /jobs/:id | Update job |
| DELETE | /jobs/:id | Remove job |
| POST | /jobs/:id/bookmark | Toggle bookmark |
| PATCH | /jobs/:id/status | Update job status |
| POST | /jobs/parse | AI-parse job posting (URL or text) |

### Discover Feed

| Method | Path | Description |
|--------|------|-------------|
| GET | /feed | Get AI-matched job feed |
| POST | /feed/refresh | Refresh feed from JSearch API |
| POST | /feed/:id/dismiss | Dismiss a feed job |
| POST | /feed/:id/save | Save a feed job to tracker |

### Applications (Pipeline Tracking)

| Method | Path | Description |
|--------|------|-------------|
| GET | /jobs/:id/application | Get application for a job |
| POST | /jobs/:id/application | Create application tracking |
| PUT | /jobs/:id/application/status | Update application status (with history) |
| PUT | /jobs/:id/application/details | Update follow-up details |
| GET | /jobs/:id/application/history | Get status change history |

### Resume

| Method | Path | Description |
|--------|------|-------------|
| POST | /resume/upload | Upload resume file (PDF/DOCX) |
| POST | /resume/critique | AI-powered resume critique |
| POST | /resume/fix | AI-generated fix suggestions |

### Contacts & Network

| Method | Path | Description |
|--------|------|-------------|
| GET | /contacts | List contacts (optional ?search=) |
| POST | /contacts | Create contact |
| PUT | /contacts/:id | Update contact |
| DELETE | /contacts/:id | Delete contact |
| GET | /network/companies | Aggregated company cards with job/contact counts |
| GET | /network/companies/:company/detail | Company detail (jobs + contacts) |

### AI & Intelligence

| Method | Path | Description |
|--------|------|-------------|
| POST | /ai/compare | AI comparison of multiple jobs |
| GET | /company/intel | Company financial profile (Yahoo Finance / AI estimated) |
