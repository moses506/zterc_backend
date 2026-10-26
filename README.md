# ZETRC Backend (Go)

A Go rewrite of the ZETRC (Zambia Environmental Training and Research Center)
API. It implements the exact HTTP contract the existing [zetrc_web](../zetrc_web)
frontend already speaks — see `../zetrc_web/# API Collection.txt` and
`../zetrc_web/apis.json` — so the frontend can point at this backend with no
code changes, just a new `VITE_API_BASE_URL`.

## Stack

- Go 1.23+ (module targets 1.26 after `go mod tidy`; `GOTOOLCHAIN=auto` will
  fetch it automatically)
- [chi](https://github.com/go-chi/chi) router
- [GORM](https://gorm.io) + PostgreSQL
- JWT (`golang-jwt/jwt/v5`) for portal session tokens, bcrypt for passwords

## Project layout

```
cmd/server/main.go        entrypoint: config, DB connect, migrate, serve
internal/config           env-based configuration
internal/db               GORM connection + AutoMigrate
internal/models           Learner/OTP + generic CRUD models + model registry
internal/auth             bcrypt password hashing, JWT issue/parse
internal/otp              OTP generation/verification (stub delivery: logs the code)
internal/httpx            JSON response helpers, CORS middleware
internal/handlers         portal auth handlers + generic CRUD engine + router
```

## Running locally

### With Docker Compose (Postgres + app)

```bash
cp .env.example .env   # edit JWT_SECRET, ALLOWED_ORIGINS as needed
docker compose up --build
```

The API listens on `http://localhost:8080`.

### Against a local/dockerized Postgres only

```bash
docker compose up -d postgres
export DATABASE_URL="postgres://zetrc:zetrc@localhost:5433/zetrc?sslmode=disable"
go run ./cmd/server
```

(The compose file maps Postgres to host port `5433` to avoid clashing with
any Postgres already running on your machine — adjust if you'd rather use
`5432`.)

Tables are created automatically on boot via `AutoMigrate` — no separate
migration step needed for this build.

### Seeding sample courses

```bash
export DATABASE_URL="postgres://zetrc:zetrc@localhost:5433/zetrc?sslmode=disable"
go run ./cmd/seed
```

Inserts a small, curated set of 5 realistic ZETRC-themed courses
(climate-smart agriculture, agroforestry, conservation farming, soil health,
climate adaptation for cooperatives) — deliberately kept short so the
course-enrollment picker and dashboard don't feel crowded. Idempotent —
matches on course name, so re-running it
won't create duplicates. Edit the list in `cmd/seed/main.go` to add more.

## Pointing the existing frontend at this backend

In `zetrc_web/.env`:

```
VITE_API_BASE_URL=http://localhost:8080
```

## API surface

### Learner portal auth (`/portal/*`)

| Method | Path | Body |
|---|---|---|
| POST | `/portal/validate-signup-data` | `{ "stage_1": { first_name, last_name, email, contact_no, password, password_confirmation } }` |
| POST | `/portal/submit-learner-registration` | same as above — creates the account and sends a signup OTP |
| POST | `/portal/authenticate` | `{ "email", "password" }` → returns learner profile + JWT |
| POST | `/portal/request-otp` | `{ "email" }` → issues a login OTP |
| POST | `/portal/validate-otp` | `{ "email", "otp" }` |
| POST | `/portal/verify-signup-otp` | `{ "email", "otp" }` → marks the account email-verified |

OTPs are 6 digits, expire after `OTP_TTL_MINUTES` (default 10), and are
single-use. **Delivery is currently a stub** — the code is only written to
the server log (`internal/otp/otp.go: Deliver`). Wire up real email/SMS
there before shipping to real users.

### Generic CRUD (`/api/*`)

Every request carries a `model` name (body field for writes, query param or
`model` header for reads) selecting one of the registered models:

`Course`, `Course_Module`, `Student_Enrollment`, `Lesson_Content`,
`Assignment`, `Assignment_Submission`, `Lead`, `Inquiry`, `Proposal_Request`.

| Method | Path | Body |
|---|---|---|
| POST | `/api/post-data/` | `{ "model", "body": { ...fields } }` — `name` auto-generated if omitted |
| GET | `/api/get-data/?model=X` | list all; add `&name=Y` for one record; a `filters` header/query param (JSON object) filters by exact column match |
| PATCH | `/api/patch-data/` | `{ "model", "body": { "name", ...fields to change } }` — partial update |
| DELETE | `/api/delete-doc/` | `{ "model", "body": { "name" } }` |
| PUT | `/api/submit-doc/` | `{ "model", "body": { "name" } }` — sets `docstatus = 1` |
| PUT | `/api/cancel-doc/` | `{ "model", "body": { "name" } }` — sets `docstatus = 2` |

Every record carries the shared fields `name` (string primary key),
`docstatus`, `creation`, `modified` — mirroring the Frappe-style documents
the original system used, which the frontend's parsing helpers
(`extractRows`, `findRecord` in `zetrc_web/src/utils/api.ts` / `auth.ts`)
already expect. Responses are always `{"data": ...}` on success or
`{"error": "message"}` on failure.

`/api/post-data/`, `/api/patch-data/`, `/api/delete-doc/`, `/api/submit-doc/`
and `/api/cancel-doc/` also accept a second calling convention some
frontend code paths use: instead of `{"model", "body"}`, the model name
travels in a `Model` header and the record's fields are the flat top-level
JSON object (see `submitEnrollment`/`submitAssignmentSubmission` in
`zetrc_web/src/utils/api.ts`). Both shapes work interchangeably.

### File uploads (`/api/upload-file/`)

`POST /api/upload-file/` — `multipart/form-data` with one or more files
under the field name `file` or `files`. Used by the course-enrollment
document-upload step (ID copies, certificates, etc.) but works for any
attachment. Returns:

```json
{ "data": [{ "name": "id.pdf", "url": "/uploads/<random>.pdf", "size": 20481 }] }
```

Allowed extensions: `.pdf .png .jpg .jpeg .doc .docx .mp3 .wav .m4a .amr .ogg
.aac`. Max size per file is `MAX_UPLOAD_SIZE_MB` (default 15). Files are
stored under `UPLOAD_DIR` (default `./uploads`) and served back statically
at `/uploads/<filename>`. The returned `url`/`name`/`size` can be attached to
any generic-CRUD record via a `documents` field — `Student_Enrollment`
already has one (`internal/models/enrollment.go`).

**Audio transcoding:** `.amr` (a phone voice-call codec no browser can play)
is automatically transcoded to `.mp3` via `ffmpeg` immediately after upload
— the returned `url` always points at the playable `.mp3`, and the original
`.amr` is deleted. This requires `ffmpeg` to be on `PATH`; it's installed in
the Docker image (`Dockerfile`) but must be installed separately for local
`go run` (`brew install ffmpeg` on macOS). If `ffmpeg` is missing or fails,
the upload still succeeds — it just serves the original untranscoded file
(which may not play in-browser), and a warning is logged.

## Assumptions worth reviewing

The original API collection only sketched `Lead`, `Inquiry`,
`Proposal_Request` and `Course_Module` with a bare `{"name": ""}` example
payload — their real field sets weren't documented anywhere I could find. I
filled in reasonable CRM/training-hub fields for each
(`internal/models/crm.go`, `internal/models/course.go`); adjust the structs
there to match whatever the Django/Frappe system actually stored if it still
exists, or to match what the frontend/mobile app ends up needing.

`/api/*` and `/portal/*` are currently open (no auth middleware) beyond the
JWT issued at login, matching the current frontend, which doesn't send an
`Authorization` header on CRUD calls. If you want the CRUD endpoints
protected, add a bearer-token middleware using `internal/auth.TokenIssuer.Parse`
in `internal/handlers/router.go`.

## Environment variables

See `.env.example`. Key ones: `DATABASE_URL`, `JWT_SECRET` (change this),
`ALLOWED_ORIGINS` (comma-separated, `*` allowed for dev), `UPLOAD_DIR` and
`MAX_UPLOAD_SIZE_MB` (file upload storage).
