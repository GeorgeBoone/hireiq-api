# HireIQ API

Go backend for HireIQ — a job seeker's CRM.

## Tech Stack

- **Language:** Go 1.22
- **Framework:** Gin
- **Database:** PostgreSQL (Cloud SQL)
- **Auth:** Firebase Authentication
- **Cloud:** Google Cloud Platform (Cloud Run)
- **AI:** Claude API (Anthropic)

## Project Structure

```
cmd/server/          → Entry point, server startup, route registration
internal/
  config/            → Environment variable loading
  middleware/        → Auth (Firebase), rate limiting, CORS
  handler/           → HTTP request handlers (parse request, return response)
  service/           → Business logic (validation, orchestration, AI calls)
  repository/        → Database queries (SQL via pgx)
  model/             → Domain structs
  worker/            → Async background jobs
migrations/          → SQL migration files
```

## Local Development

### Prerequisites

- Go 1.22+
- PostgreSQL 15+ (local or Cloud SQL proxy)
- Firebase project with Google Auth enabled

### Setup

```bash
# 1. Clone and enter directory
git clone https://github.com/yourusername/hireiq-api.git
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

Push to `main` triggers Cloud Build → builds Docker image → deploys to Cloud Run.

### Manual deploy

```bash
gcloud builds submit --config cloudbuild.yaml
```

## API Routes

| Method | Path | Description |
|--------|------|-------------|
| GET | /health | Health check |
| POST | /auth/google | Sign in / create account |
| GET | /profile | Get user profile |
| PUT | /profile | Update profile |
| PUT | /profile/skills | Update skills |
| GET | /jobs | List saved jobs |
| POST | /jobs | Save a job |
| GET | /jobs/:id | Get job detail |
| PUT | /jobs/:id | Update job |
| DELETE | /jobs/:id | Remove job |
| POST | /jobs/:id/bookmark | Toggle bookmark |

More routes (applications, notes, contacts, AI, resume) are scaffolded in `main.go` and will be implemented in subsequent sprints.
