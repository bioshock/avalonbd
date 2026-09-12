# Avalon Shop

Go monolith storefront + admin. See `../docs/superpowers/specs/2026-09-12-avalonshop-design.md`.

## Run locally

    docker run --rm -d --name avalon-pg -e POSTGRES_USER=avalon -e POSTGRES_PASSWORD=avalon -e POSTGRES_DB=avalon -p 5432:5432 postgres:17-alpine
    cp .env.example .env   # edit ADMIN_* at least
    go run .

Open http://localhost:8080. With `SMTP_HOST` empty, emails are printed to stdout.

## Test

    go test ./...                                   # unit tests
    TEST_DATABASE_URL=postgres://avalon:avalon@localhost:5432/avalon?sslmode=disable go test ./...   # + DB tests (drops and recreates the public schema!)

## Deploy on Coolify

See the "Deploy" section at the bottom (filled in by the final task).