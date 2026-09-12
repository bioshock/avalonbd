# Avalon Shop Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Avalon Shop, a Go monolith storefront plus admin for a small cash-on-delivery catalog, deployable on Coolify from the `avalonshop/` directory.

**Architecture:** One Go binary serves server-rendered HTML (html/template + HTMX) for both storefront and admin, backed by PostgreSQL via pgx. Uploaded images are re-encoded to WebP at three widths at upload time and served from a volume. Sessions, cart, and tokens are HMAC-signed cookies, so there is no session table. Email goes out over stdlib SMTP to Brevo.

**Tech Stack:** Go 1.26, stdlib `net/http` routing, `html/template`, HTMX 2, PostgreSQL 17, `github.com/jackc/pgx/v5`, `golang.org/x/crypto/bcrypt`, `golang.org/x/image/draw`, `github.com/gen2brain/webp`. Docker multi-stage build to distroless. Docker Compose for Coolify.

**Spec:** `docs/superpowers/specs/2026-09-12-avalonshop-design.md`

## Global Constraints

- Go 1.26. Module path `avalonshop`. All code under `avalonshop/` in this repo. Existing root files (`index.html`, `styles.css`, `assets/`) are untouched.
- Third-party Go modules limited to: `github.com/jackc/pgx/v5`, `golang.org/x/crypto`, `golang.org/x/image`, `github.com/gen2brain/webp`. No router, no ORM, no migration tool, no test framework.
- Money is integer BDT. Display as `৳ 1,200` (taka sign, space, comma thousands).
- Times stored UTC, displayed in `Asia/Dhaka`. `main.go` imports `_ "time/tzdata"`.
- Order numbers: `AV-` + 6-digit zero-padded sequence starting at 1001.
- Order status values: `new`, `confirmed`, `shipped`, `delivered`, `cancelled`. Allowed transitions: `new→confirmed|cancelled`, `confirmed→shipped|cancelled`, `shipped→delivered|cancelled`.
- Image widths: 400, 900, 1600. WebP only, quality 82. Never upscale. Filenames `{stem}-{w}.webp` where stem is 16 hex chars.
- Cookies: `HttpOnly`, `SameSite=Lax`, `Path=/`, `Secure` when `BASE_URL` starts with `https://`. Names: `cart`, `sess`, `flash`.
- Session lifetime 30 days. Reset token lifetime 1 hour. Cart max 20 lines, qty 1–99.
- Rate limits (in-memory, per client IP): login 5 failures / 15 min, forgot-password 5 / 15 min, checkout 10 / hour.
- Server timeouts: read header 5s, read 30s, write 60s, idle 120s. Body limit 1 MB except the image upload route (110 MB).
- Security headers on every response: `X-Content-Type-Options: nosniff`, `Referrer-Policy: strict-origin-when-cross-origin`, `Content-Security-Policy: default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; frame-ancestors 'none'; form-action 'self'; base-uri 'self'`.
- CSRF: `http.NewCrossOriginProtection().Handler(mux)`. No CSRF tokens in forms.
- Admin routes return 404 (not 403) to non-admins.
- Templates use `{{define "content"}}`; the layout is executed by name `layout.html`.
- **Performance budget (verified in Task 19):** no third-party requests on any store page; HTML ≤ 30 KB gzipped; CSS ≤ 15 KB; JS ≤ 25 KB gzipped total; every `<img>` has `width`, `height`, `srcset`, `sizes`; the product page's first image has `fetchpriority="high"` and is not lazy; all other images are `loading="lazy" decoding="async"`; fonts self-hosted with `font-display: swap` and Manrope preloaded; static and media served with `Cache-Control: public, max-age=31536000, immutable`; HTML responses gzipped; Lighthouse mobile Performance ≥ 95 and SEO = 100 on home, listing, and product pages.
- **SEO on every store page:** `<title>`, `<meta name="description">`, `<link rel="canonical">`, Open Graph `title/description/url/type/image`. Product pages add JSON-LD `Product` with per-variant `Offer`s. Exactly one `<h1>` per page. Inactive products 404.
- Commit after every task with a conventional message. Run `go vet ./...` and `go test ./...` before each commit.

## File Structure

```
avalonshop/
  go.mod, go.sum
  main.go                          wiring, flags, graceful shutdown, -healthcheck probe
  Dockerfile, docker-compose.yml, .env.example, .dockerignore, README.md
  migrations/0001_init.sql         schema
  templates/
    layout.html                    store shell (SEO head, nav, footer, drawer mount)
    admin/layout.html              admin shell
    store/{home,products,product,cart,checkout,order,login,register,forgot,reset,account}.html
    admin/{dashboard,products,product_form,categories,orders,order,zones}.html
    partials/{cart_badge,cart_drawer,product_card,variant_row,image_list,checkout_totals}.html
    email/{order_confirmation,order_new_admin,order_shipped,order_delivered,password_reset}.txt
  static/app.css, app.js, htmx.min.js, favicon.svg, fonts/{manrope,dmmono-400,dmmono-500}.woff2
  internal/config/config.go        env + .env loading
  internal/store/                  all SQL, one file per area
    store.go (Store, errors, mapErr) migrate.go categories.go zones.go products.go images.go users.go orders.go slug.go
    storetest/storetest.go         test DB helper (not compiled into the binary)
  internal/token/token.go          HMAC signer, cart/session/reset/order tokens
  internal/money/money.go          Format(int) string
  internal/img/img.go              sniff, EXIF orientation, resize, WebP encode
  internal/mail/mail.go            SMTP sender, text templates, async send
  internal/app/                    all HTTP: one package, files by area
    app.go (App, New, Handler, routes) render.go middleware.go cookies.go limiter.go
    store_pages.go cart.go checkout.go auth.go sitemap.go
    admin.go admin_products.go admin_orders.go
```

Deviation from the spec's layout, locked here: storefront and admin handlers share one package `internal/app` (they share rendering, cookies, and middleware), and slugify lives in `internal/store/slug.go`. Everything else matches the spec.

---

### Task 1: Scaffold, config, health endpoint, Docker

**Files:**
- Create: `avalonshop/go.mod`, `avalonshop/main.go`, `avalonshop/internal/config/config.go`, `avalonshop/internal/config/config_test.go`, `avalonshop/Dockerfile`, `avalonshop/docker-compose.yml`, `avalonshop/.env.example`, `avalonshop/.dockerignore`, `avalonshop/README.md`
- Modify: `.gitignore` (repo root)

**Interfaces:**
- Produces: `config.Config` struct and `config.Load() (Config, error)`, `config.LoadDotEnv(path string) error`, `Config.Secure() bool`. The binary supports `-healthcheck`.

- [x] **Step 1: Create the module and gitignore**

```bash
mkdir -p avalonshop/internal/config && cd avalonshop
cat > go.mod <<'EOF'
module avalonshop

go 1.26
EOF
cd .. && printf '.superpowers/\navalonshop/data/\navalonshop/.env\n' > .gitignore
```

- [x] **Step 2: Write the failing config test**

`avalonshop/internal/config/config_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func good() map[string]string {
	return map[string]string{
		"DATABASE_URL":       "postgres://x",
		"BASE_URL":           "https://shop.example.com/",
		"SESSION_SECRET":     strings.Repeat("ab", 32),
		"MAIL_FROM":          "shop@example.com",
		"ORDER_NOTIFY_EMAIL": "owner@example.com",
	}
}

func TestLoadDefaults(t *testing.T) {
	c, err := load(env(good()))
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":8080" || c.SMTPPort != "587" || c.UploadDir != "./data/uploads" {
		t.Fatalf("defaults wrong: %+v", c)
	}
	if c.BaseURL != "https://shop.example.com" {
		t.Fatalf("trailing slash not trimmed: %q", c.BaseURL)
	}
	if !c.Secure() || len(c.SessionSecret) != 32 {
		t.Fatalf("secure=%v secretlen=%d", c.Secure(), len(c.SessionSecret))
	}
}

func TestLoadMissing(t *testing.T) {
	m := good()
	delete(m, "DATABASE_URL")
	if _, err := load(env(m)); err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("expected DATABASE_URL error, got %v", err)
	}
	m = good()
	m["SESSION_SECRET"] = "abcd"
	if _, err := load(env(m)); err == nil || !strings.Contains(err.Error(), "SESSION_SECRET") {
		t.Fatalf("expected SESSION_SECRET error, got %v", err)
	}
}

func TestLoadDotEnv(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(p, []byte("# comment\nFOO_TEST=bar\nQUOTED_TEST=\"a b\"\n\n"), 0o600)
	t.Setenv("FOO_TEST", "")
	os.Unsetenv("FOO_TEST")
	if err := LoadDotEnv(p); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("FOO_TEST") != "bar" || os.Getenv("QUOTED_TEST") != "a b" {
		t.Fatalf("got %q %q", os.Getenv("FOO_TEST"), os.Getenv("QUOTED_TEST"))
	}
	if err := LoadDotEnv(filepath.Join(t.TempDir(), "missing")); err != nil {
		t.Fatalf("missing file should be fine: %v", err)
	}
}
```

- [x] **Step 3: Run the test to verify it fails**

Run: `cd avalonshop && go test ./internal/config/`
Expected: FAIL, `undefined: load`

- [x] **Step 4: Write config.go**

```go
// Package config reads settings from the environment.
package config

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Addr             string
	DatabaseURL      string
	BaseURL          string
	SessionSecret    []byte
	UploadDir        string
	SMTPHost         string
	SMTPPort         string
	SMTPUser         string
	SMTPPass         string
	MailFrom         string
	OrderNotifyEmail string
	AdminEmail       string
	AdminPassword    string
}

// Secure reports whether cookies should carry the Secure flag.
func (c Config) Secure() bool { return strings.HasPrefix(c.BaseURL, "https://") }

func Load() (Config, error) { return load(os.Getenv) }

func load(get func(string) string) (Config, error) {
	def := func(v, d string) string {
		if v == "" {
			return d
		}
		return v
	}
	c := Config{
		Addr:             def(get("ADDR"), ":8080"),
		DatabaseURL:      get("DATABASE_URL"),
		BaseURL:          strings.TrimRight(get("BASE_URL"), "/"),
		UploadDir:        def(get("UPLOAD_DIR"), "./data/uploads"),
		SMTPHost:         get("SMTP_HOST"),
		SMTPPort:         def(get("SMTP_PORT"), "587"),
		SMTPUser:         get("SMTP_USER"),
		SMTPPass:         get("SMTP_PASS"),
		MailFrom:         get("MAIL_FROM"),
		OrderNotifyEmail: get("ORDER_NOTIFY_EMAIL"),
		AdminEmail:       get("ADMIN_EMAIL"),
		AdminPassword:    get("ADMIN_PASSWORD"),
	}
	for _, kv := range [][2]string{
		{"DATABASE_URL", c.DatabaseURL}, {"BASE_URL", c.BaseURL},
		{"MAIL_FROM", c.MailFrom}, {"ORDER_NOTIFY_EMAIL", c.OrderNotifyEmail},
	} {
		if kv[1] == "" {
			return c, fmt.Errorf("config: %s is required", kv[0])
		}
	}
	sec, err := hex.DecodeString(get("SESSION_SECRET"))
	if err != nil || len(sec) < 32 {
		return c, errors.New("config: SESSION_SECRET must be at least 64 hex characters")
	}
	c.SessionSecret = sec
	return c, nil
}

// LoadDotEnv sets KEY=VALUE lines from path into the environment unless already set.
// A missing file is not an error. ponytail: 20-line parser instead of a dotenv module.
func LoadDotEnv(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
	return sc.Err()
}
```

- [x] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/config/`
Expected: PASS

- [x] **Step 6: Write main.go with /healthz and the -healthcheck probe**

```go
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	"avalonshop/internal/config"
)

func main() {
	healthcheck := flag.Bool("healthcheck", false, "probe the running server and exit 0 if healthy")
	flag.Parse()
	if *healthcheck {
		os.Exit(probe())
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// probe is used as the Docker HEALTHCHECK; the distroless image has no curl.
func probe() int {
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://127.0.0.1:8080/healthz")
	if err != nil || resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func run(log *slog.Logger) error {
	if err := config.LoadDotEnv(".env"); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(sctx)
	}()
	log.Info("listening", "addr", cfg.Addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
```

- [x] **Step 7: Write Dockerfile, compose, .env.example, .dockerignore, README**

`avalonshop/Dockerfile`:

```dockerfile
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /avalonshop . \
 && mkdir -p /data/uploads

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /avalonshop /avalonshop
COPY --from=build --chown=nonroot:nonroot /data/uploads /data/uploads
ENV UPLOAD_DIR=/data/uploads ADDR=:8080
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=5 CMD ["/avalonshop", "-healthcheck"]
ENTRYPOINT ["/avalonshop"]
```

`avalonshop/docker-compose.yml`:

```yaml
services:
  app:
    build: .
    environment:
      - SERVICE_FQDN_APP_8080
      - BASE_URL=https://${SERVICE_FQDN_APP}
      - DATABASE_URL=postgres://avalon:${SERVICE_PASSWORD_POSTGRES}@db:5432/avalon?sslmode=disable
      - SESSION_SECRET=${SERVICE_HEX_64_SESSION}
      - UPLOAD_DIR=/data/uploads
      - SMTP_HOST=${SMTP_HOST:-smtp-relay.brevo.com}
      - SMTP_PORT=${SMTP_PORT:-587}
      - SMTP_USER=${SMTP_USER}
      - SMTP_PASS=${SMTP_PASS}
      - MAIL_FROM=${MAIL_FROM}
      - ORDER_NOTIFY_EMAIL=${ORDER_NOTIFY_EMAIL}
      - ADMIN_EMAIL=${ADMIN_EMAIL}
      - ADMIN_PASSWORD=${ADMIN_PASSWORD}
    volumes:
      - uploads:/data/uploads
    depends_on:
      db:
        condition: service_healthy
  db:
    image: postgres:17-alpine
    environment:
      - POSTGRES_USER=avalon
      - POSTGRES_DB=avalon
      - POSTGRES_PASSWORD=${SERVICE_PASSWORD_POSTGRES}
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U avalon"]
      interval: 5s
      timeout: 3s
      retries: 10
volumes:
  uploads:
  pgdata:
```

`avalonshop/.env.example`:

```
ADDR=:8080
DATABASE_URL=postgres://avalon:avalon@localhost:5432/avalon?sslmode=disable
BASE_URL=http://localhost:8080
SESSION_SECRET=0000000000000000000000000000000000000000000000000000000000000000
UPLOAD_DIR=./data/uploads
SMTP_HOST=
SMTP_PORT=587
SMTP_USER=
SMTP_PASS=
MAIL_FROM=shop@avalonbd.com
ORDER_NOTIFY_EMAIL=you@avalonbd.com
ADMIN_EMAIL=admin@avalonbd.com
ADMIN_PASSWORD=change-me-now
```

`avalonshop/.dockerignore`:

```
data/
.env
*_test.go
```

`avalonshop/README.md`:

```markdown
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
```

- [x] **Step 8: Verify build and health**

Run: `cd avalonshop && go vet ./... && go build -o /tmp/avalonshop . && echo ok`
Expected: `ok`

Run (in one shell): `DATABASE_URL=x BASE_URL=http://localhost:8080 SESSION_SECRET=$(printf '0%.0s' {1..64}) MAIL_FROM=a@b.c ORDER_NOTIFY_EMAIL=a@b.c /tmp/avalonshop & sleep 1; /tmp/avalonshop -healthcheck; echo "exit=$?"; kill %1`
Expected: `exit=0`

- [x] **Step 9: Commit**

```bash
git add .gitignore avalonshop
git commit -m "feat(shop): scaffold Go module, config, health endpoint, Docker"
```

---

### Task 2: Schema and migration runner

**Files:**
- Create: `avalonshop/migrations/0001_init.sql`, `avalonshop/internal/store/store.go`, `avalonshop/internal/store/migrate.go`, `avalonshop/internal/store/migrate_test.go`, `avalonshop/internal/store/storetest/storetest.go`
- Modify: `avalonshop/main.go`

**Interfaces:**
- Produces: `store.Migrate(ctx, db *pgxpool.Pool, fsys fs.FS) error` (reads `migrations/*.sql` from fsys), `store.New(db *pgxpool.Pool) *Store`, errors `store.ErrNotFound`, `store.ErrInUse`, `store.ErrDuplicate`, `storetest.Pool(t testing.TB) *pgxpool.Pool` (skips without `TEST_DATABASE_URL`, resets schema, runs migrations).

- [x] **Step 1: Add pgx**

Run: `cd avalonshop && go get github.com/jackc/pgx/v5@latest`. Run `go mod tidy` after Step 4 introduces imports; otherwise it removes the unused dependency (approved implementation correction).

- [x] **Step 2: Write the schema**

`avalonshop/migrations/0001_init.sql`:

```sql
create table categories (
  id bigint generated always as identity primary key,
  slug text not null unique,
  name text not null,
  sort int not null default 0
);

create table products (
  id bigint generated always as identity primary key,
  slug text not null unique,
  name text not null,
  description text not null default '',
  category_id bigint references categories(id),
  meta_description text not null default '',
  active boolean not null default true,
  featured boolean not null default false,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);
create index products_category_idx on products(category_id);

create table variants (
  id bigint generated always as identity primary key,
  product_id bigint not null references products(id) on delete cascade,
  name text not null,
  sku text unique,
  price int not null check (price >= 0),
  stock int not null default 0 check (stock >= 0),
  sort int not null default 0
);
create index variants_product_idx on variants(product_id);

create table product_images (
  id bigint generated always as identity primary key,
  product_id bigint not null references products(id) on delete cascade,
  file text not null,
  alt text not null default '',
  width int not null,
  height int not null,
  sort int not null default 0
);
create index product_images_product_idx on product_images(product_id);

create table delivery_zones (
  id bigint generated always as identity primary key,
  name text not null,
  fee int not null check (fee >= 0),
  active boolean not null default true,
  sort int not null default 0
);

create table users (
  id bigint generated always as identity primary key,
  email text not null,
  password_hash text not null,
  name text not null default '',
  phone text not null default '',
  address text not null default '',
  role text not null default 'customer' check (role in ('customer','admin')),
  created_at timestamptz not null default now()
);
create unique index users_email_idx on users (lower(email));

create sequence order_number_seq start 1001;

create table orders (
  id bigint generated always as identity primary key,
  number text not null unique,
  user_id bigint references users(id),
  name text not null,
  phone text not null,
  email text not null,
  address text not null,
  zone_name text not null,
  delivery_fee int not null,
  subtotal int not null,
  total int not null,
  status text not null default 'new' check (status in ('new','confirmed','shipped','delivered','cancelled')),
  note text not null default '',
  admin_note text not null default '',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);
create index orders_status_idx on orders(status, created_at desc);
create index orders_user_idx on orders(user_id);

create table order_items (
  id bigint generated always as identity primary key,
  order_id bigint not null references orders(id) on delete cascade,
  variant_id bigint references variants(id) on delete set null,
  product_name text not null,
  variant_name text not null,
  unit_price int not null,
  qty int not null check (qty > 0)
);
create index order_items_order_idx on order_items(order_id);
```

- [x] **Step 3: Write store.go and the test helper**

`avalonshop/internal/store/store.go`:

```go
// Package store is the only place SQL lives.
package store

import (
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound   = errors.New("not found")
	ErrInUse      = errors.New("in use")
	ErrDuplicate  = errors.New("duplicate")
	ErrTransition = errors.New("invalid status transition")
)

type Store struct{ db *pgxpool.Pool }

func New(db *pgxpool.Pool) *Store { return &Store{db: db} }

// mapErr turns pgx sentinel and constraint errors into package errors.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "23505":
			return ErrDuplicate
		case "23503":
			return ErrInUse
		}
	}
	return err
}
```

`avalonshop/internal/store/storetest/storetest.go`:

```go
// Package storetest opens a throwaway test database. Not imported by the binary.
package storetest

import (
	"context"
	"os"
	"testing"

	"avalonshop/internal/store"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool skips the test unless TEST_DATABASE_URL is set, then DROPS the public
// schema, recreates it, and runs migrations. Never point it at real data.
func Pool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `drop schema public cascade; create schema public`); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, pool, os.DirFS(root(t))); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// root walks up from the test's working directory to the directory holding go.mod.
func root(t testing.TB) string {
	dir, _ := os.Getwd()
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(dir + "/go.mod"); err == nil {
			return dir
		}
		dir += "/.."
	}
	t.Fatal("go.mod not found above test dir")
	return ""
}
```

- [x] **Step 4: Write the failing migration test**

`avalonshop/internal/store/migrate_test.go`:

```go
package store_test

import (
	"context"
	"os"
	"testing"

	"avalonshop/internal/store"
	"avalonshop/internal/store/storetest"
)

func TestMigrateIdempotent(t *testing.T) {
	db := storetest.Pool(t)
	ctx := context.Background()
	if err := store.Migrate(ctx, db, os.DirFS("../..")); err != nil {
		t.Fatalf("second run: %v", err)
	}
	var n int
	if err := db.QueryRow(ctx, `select count(*) from schema_migrations`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("want 1 migration recorded, got %d err=%v", n, err)
	}
	for _, tbl := range []string{"categories", "products", "variants", "product_images", "delivery_zones", "users", "orders", "order_items"} {
		var ok bool
		db.QueryRow(ctx, `select to_regclass($1) is not null`, tbl).Scan(&ok)
		if !ok {
			t.Fatalf("table %s missing", tbl)
		}
	}
}
```

- [x] **Step 5: Run to verify it fails**

Run: `TEST_DATABASE_URL=postgres://avalon:avalon@localhost:5432/avalon?sslmode=disable go test ./internal/store/`
Expected: FAIL, `undefined: store.Migrate`

- [x] **Step 6: Write migrate.go**

```go
package store

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Migrate applies every migrations/*.sql in fsys that is not yet recorded in
// schema_migrations, in filename order, each in its own transaction.
func Migrate(ctx context.Context, db *pgxpool.Pool, fsys fs.FS) error {
	if _, err := db.Exec(ctx, `create table if not exists schema_migrations (version text primary key)`); err != nil {
		return err
	}
	entries, err := fs.ReadDir(fsys, "migrations")
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		var done bool
		if err := db.QueryRow(ctx, `select exists(select 1 from schema_migrations where version=$1)`, name).Scan(&done); err != nil {
			return err
		}
		if done {
			continue
		}
		sqlText, err := fs.ReadFile(fsys, "migrations/"+name)
		if err != nil {
			return err
		}
		tx, err := db.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(sqlText)); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `insert into schema_migrations(version) values($1)`, name); err != nil {
			tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}
```

- [x] **Step 7: Run to verify it passes**

Run: `TEST_DATABASE_URL=postgres://avalon:avalon@localhost:5432/avalon?sslmode=disable go test ./internal/store/...`
Expected: PASS (and `go test ./internal/store/` without the env var prints SKIP)

- [x] **Step 8: Wire migrations into main.go**

Add to imports: `"embed"`, `"avalonshop/internal/store"`, `"github.com/jackc/pgx/v5/pgxpool"`. Add above `main`:

```go
//go:embed migrations/*.sql
var migrationsFS embed.FS
```

In `run`, after `defer stop()`:

```go
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := store.Migrate(ctx, pool, migrationsFS); err != nil {
		return err
	}
	_ = store.New(pool) // used from Task 10 on
```

- [x] **Step 9: Verify and commit**

Run: `go vet ./... && go build ./... && echo ok`
Expected: `ok`

```bash
git add avalonshop
git commit -m "feat(shop): schema and embedded migration runner"
```

---

### Task 3: Token package (HMAC cart, session, reset, order tokens)

**Files:**
- Create: `avalonshop/internal/token/token.go`, `avalonshop/internal/token/token_test.go`

**Interfaces:**
- Produces:
  - `token.New(key []byte) Signer`
  - `type CartLine struct { VariantID int64 \`json:"v"\`; Qty int \`json:"q"\` }`, `const MaxCartLines = 20`
  - `(Signer) EncodeCart([]CartLine) string`, `(Signer) DecodeCart(string) []CartLine` (nil on tamper)
  - `(Signer) EncodeSession(userID int64, exp time.Time) string`, `(Signer) DecodeSession(v string, now time.Time) (int64, bool)`
  - `(Signer) ResetToken(userID int64, exp time.Time, passwordHash string) string`, `(Signer) ParseReset(tok string, now time.Time, lookupHash func(int64) (string, bool)) (int64, bool)`
  - `(Signer) OrderToken(number string) string` (16 hex chars), `(Signer) VerifyOrderToken(number, t string) bool`

**Implementation corrections (approved minor-correction policy):** Bind reset signatures to the entire password hash, not `hashPrefix`, so any password hash change invalidates the token. Reject session/reset tokens at `now.Unix() >= exp`, and reject nonpositive reset user IDs before lookup. Tests use fixed times and add expiry-boundary, shared-hash-prefix, malformed-input, wrong-key, and cart-filtering coverage; interfaces, architecture, and task order are unchanged.

**Verification:** `go test ./internal/token/` first failed with `undefined: New`; it passed after implementation. All 13 token tests passed with 100% statement coverage using `go test -count=1 -cover ./internal/token/`. `go vet ./...` and `go test -count=1 -v ./...` passed; the migration test skipped because `TEST_DATABASE_URL` was unset, as permitted by the latest instruction. Parent will perform final database-backed validation.

- [x] **Step 1: Write the failing tests**

`avalonshop/internal/token/token_test.go`:

```go
package token

import (
	"strings"
	"testing"
	"time"
)

var s = New([]byte(strings.Repeat("k", 32)))

func TestCartRoundTrip(t *testing.T) {
	in := []CartLine{{VariantID: 7, Qty: 2}, {VariantID: 9, Qty: 1}}
	out := s.DecodeCart(s.EncodeCart(in))
	if len(out) != 2 || out[0] != in[0] || out[1] != in[1] {
		t.Fatalf("got %+v", out)
	}
}

func TestCartTamperAndGarbage(t *testing.T) {
	v := s.EncodeCart([]CartLine{{VariantID: 7, Qty: 2}})
	if s.DecodeCart(v[:len(v)-2]+"zz") != nil {
		t.Fatal("tampered signature accepted")
	}
	if s.DecodeCart("not-a-cookie") != nil || s.DecodeCart("") != nil {
		t.Fatal("garbage accepted")
	}
	other := New([]byte(strings.Repeat("x", 32)))
	if other.DecodeCart(v) != nil {
		t.Fatal("wrong key accepted")
	}
}

func TestCartCaps(t *testing.T) {
	var in []CartLine
	for i := 1; i <= 25; i++ {
		in = append(in, CartLine{VariantID: int64(i), Qty: 1})
	}
	in = append(in, CartLine{VariantID: 99, Qty: 0}, CartLine{VariantID: 98, Qty: 100}, CartLine{VariantID: 0, Qty: 1})
	out := s.DecodeCart(s.EncodeCart(in))
	if len(out) != MaxCartLines {
		t.Fatalf("want %d lines, got %d", MaxCartLines, len(out))
	}
	for _, l := range out {
		if l.Qty < 1 || l.Qty > 99 || l.VariantID <= 0 {
			t.Fatalf("invalid line kept: %+v", l)
		}
	}
}

func TestSession(t *testing.T) {
	now := time.Now()
	v := s.EncodeSession(42, now.Add(time.Hour))
	if id, ok := s.DecodeSession(v, now); !ok || id != 42 {
		t.Fatalf("got %d %v", id, ok)
	}
	if _, ok := s.DecodeSession(v, now.Add(2*time.Hour)); ok {
		t.Fatal("expired session accepted")
	}
	if _, ok := s.DecodeSession("1.2.3", now); ok {
		t.Fatal("bad signature accepted")
	}
}

func TestReset(t *testing.T) {
	now := time.Now()
	hash := "$2a$12$abcdefghijklmnopqrstuv"
	tok := s.ResetToken(5, now.Add(time.Hour), hash)
	lookup := func(id int64) (string, bool) { return hash, id == 5 }
	if id, ok := s.ParseReset(tok, now, lookup); !ok || id != 5 {
		t.Fatalf("valid token rejected: %d %v", id, ok)
	}
	if _, ok := s.ParseReset(tok, now.Add(2*time.Hour), lookup); ok {
		t.Fatal("expired token accepted")
	}
	changed := func(int64) (string, bool) { return "$2a$12$zzzzzzzzzzzzzzzzzzzzzz", true }
	if _, ok := s.ParseReset(tok, now, changed); ok {
		t.Fatal("token survived password change")
	}
}

func TestOrderToken(t *testing.T) {
	tok := s.OrderToken("AV-001001")
	if len(tok) != 16 || !s.VerifyOrderToken("AV-001001", tok) {
		t.Fatalf("bad token %q", tok)
	}
	if s.VerifyOrderToken("AV-001002", tok) {
		t.Fatal("token valid for another order")
	}
}
```

- [x] **Step 2: Run to verify it fails**

Run: `go test ./internal/token/`
Expected: FAIL, `undefined: New`

- [x] **Step 3: Write token.go**

```go
// Package token signs small values so they can live in cookies and URLs.
package token

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const MaxCartLines = 20

type Signer struct{ key []byte }

func New(key []byte) Signer { return Signer{key: key} }

func (s Signer) sign(msg string) string {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(msg))
	return hex.EncodeToString(m.Sum(nil))
}

func (s Signer) verify(msg, sig string) bool {
	return hmac.Equal([]byte(s.sign(msg)), []byte(sig))
}

type CartLine struct {
	VariantID int64 `json:"v"`
	Qty       int   `json:"q"`
}

func (s Signer) EncodeCart(lines []CartLine) string {
	b, _ := json.Marshal(lines)
	enc := base64.RawURLEncoding.EncodeToString(b)
	return enc + "." + s.sign(enc)
}

// DecodeCart returns nil for a missing, malformed, or tampered value. Invalid
// lines are dropped and the result is capped at MaxCartLines.
func (s Signer) DecodeCart(v string) []CartLine {
	enc, sig, ok := strings.Cut(v, ".")
	if !ok || !s.verify(enc, sig) {
		return nil
	}
	b, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return nil
	}
	var lines []CartLine
	if json.Unmarshal(b, &lines) != nil {
		return nil
	}
	out := lines[:0]
	for _, l := range lines {
		if l.VariantID > 0 && l.Qty >= 1 && l.Qty <= 99 {
			out = append(out, l)
		}
	}
	if len(out) > MaxCartLines {
		out = out[:MaxCartLines]
	}
	return out
}

func (s Signer) EncodeSession(userID int64, exp time.Time) string {
	msg := fmt.Sprintf("%d.%d", userID, exp.Unix())
	return msg + "." + s.sign(msg)
}

func (s Signer) DecodeSession(v string, now time.Time) (int64, bool) {
	p := strings.Split(v, ".")
	if len(p) != 3 || !s.verify(p[0]+"."+p[1], p[2]) {
		return 0, false
	}
	id, err1 := strconv.ParseInt(p[0], 10, 64)
	exp, err2 := strconv.ParseInt(p[1], 10, 64)
	if err1 != nil || err2 != nil || id <= 0 || now.Unix() > exp {
		return 0, false
	}
	return id, true
}

func hashPrefix(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

// ResetToken binds the token to the current password hash, so it stops
// working once the password changes. That makes it single-use without a table.
func (s Signer) ResetToken(userID int64, exp time.Time, passwordHash string) string {
	msg := fmt.Sprintf("%d.%d", userID, exp.Unix())
	return msg + "." + s.sign(msg+"."+hashPrefix(passwordHash))
}

func (s Signer) ParseReset(tok string, now time.Time, lookupHash func(int64) (string, bool)) (int64, bool) {
	p := strings.Split(tok, ".")
	if len(p) != 3 {
		return 0, false
	}
	id, err1 := strconv.ParseInt(p[0], 10, 64)
	exp, err2 := strconv.ParseInt(p[1], 10, 64)
	if err1 != nil || err2 != nil || now.Unix() > exp {
		return 0, false
	}
	hash, ok := lookupHash(id)
	if !ok || !s.verify(p[0]+"."+p[1]+"."+hashPrefix(hash), p[2]) {
		return 0, false
	}
	return id, true
}

func (s Signer) OrderToken(number string) string { return s.sign("order:" + number)[:16] }

func (s Signer) VerifyOrderToken(number, t string) bool {
	return hmac.Equal([]byte(s.OrderToken(number)), []byte(t))
}
```

- [x] **Step 4: Run to verify it passes**

Run: `go test ./internal/token/`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add avalonshop/internal/token
git commit -m "feat(shop): HMAC token package for cart, session, reset, order links"
```

---

### Task 4: Store: categories, zones, slugs, money

**Files:**
- Create: `avalonshop/internal/store/categories.go`, `avalonshop/internal/store/zones.go`, `avalonshop/internal/store/slug.go`, `avalonshop/internal/store/slug_test.go`, `avalonshop/internal/store/categories_test.go`, `avalonshop/internal/money/money.go`, `avalonshop/internal/money/money_test.go`

**Interfaces:**
- Produces:
  - `store.Category{ID int64; Slug, Name string; Sort int}`; `ListCategories(ctx) ([]Category, error)`, `GetCategoryBySlug(ctx, slug) (Category, error)`, `GetCategory(ctx, id) (Category, error)`, `CreateCategory(ctx, c) (int64, error)`, `UpdateCategory(ctx, c) error`, `DeleteCategory(ctx, id) error` (ErrInUse when products reference it)
  - `store.Zone{ID int64; Name string; Fee int; Active bool; Sort int}`; `ListZones(ctx, activeOnly bool) ([]Zone, error)`, `GetZone(ctx, id) (Zone, error)`, `CreateZone(ctx, z) (int64, error)`, `UpdateZone(ctx, z) error`, `DeleteZone(ctx, id) error`
  - `store.Slugify(s string) string`; `(*Store) UniqueSlug(ctx, table, base string, excludeID int64) (string, error)` where table is `"products"` or `"categories"`
  - `money.Format(n int) string` → `"৳ 1,200"`

- [ ] **Step 1: Write failing unit tests for slug and money**

`avalonshop/internal/store/slug_test.go`:

```go
package store

import "testing"

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Wild Forest Honey":   "wild-forest-honey",
		"  Himsagar   Mango! ": "himsagar-mango",
		"Café Crème":          "cafe-creme",
		"আম (Mango) 5kg":      "mango-5kg",
		"---":                 "item",
		"":                    "item",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}
```

`avalonshop/internal/money/money_test.go`:

```go
package money

import "testing"

func TestFormat(t *testing.T) {
	for n, want := range map[int]string{0: "৳ 0", 650: "৳ 650", 1200: "৳ 1,200", 1234567: "৳ 1,234,567"} {
		if got := Format(n); got != want {
			t.Errorf("Format(%d) = %q, want %q", n, got, want)
		}
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/store/ ./internal/money/`
Expected: FAIL, `undefined: Slugify` and `undefined: Format`

- [ ] **Step 3: Write slug.go and money.go**

`avalonshop/internal/store/slug.go`:

```go
package store

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Slugify lowercases, strips accents, keeps [a-z0-9], and joins with hyphens.
// Non-Latin scripts are dropped; the admin can edit the slug by hand.
func Slugify(s string) string {
	s = norm.NFD.String(strings.ToLower(s))
	var b strings.Builder
	dash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case unicode.Is(unicode.Mn, r):
			// combining mark from NFD: drop it
		default:
			if b.Len() > 0 && !dash {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "item"
	}
	return out
}

// UniqueSlug returns base, or base-2, base-3, ... until no row in table other
// than excludeID has it. table must be "products" or "categories".
func (s *Store) UniqueSlug(ctx context.Context, table, base string, excludeID int64) (string, error) {
	if table != "products" && table != "categories" {
		return "", fmt.Errorf("UniqueSlug: bad table %q", table)
	}
	q := `select exists(select 1 from ` + table + ` where slug = $1 and id <> $2)`
	for i := 1; ; i++ {
		cand := base
		if i > 1 {
			cand = fmt.Sprintf("%s-%d", base, i)
		}
		var taken bool
		if err := s.db.QueryRow(ctx, q, cand, excludeID).Scan(&taken); err != nil {
			return "", err
		}
		if !taken {
			return cand, nil
		}
	}
}
```

Note: `golang.org/x/text` is pulled in transitively by pgx already (`go mod tidy` will confirm it is in go.sum). It stays within the dependency budget because it is not a new module tree.

`avalonshop/internal/money/money.go`:

```go
// Package money formats integer BDT amounts.
package money

import "strconv"

// Format renders 1200 as "৳ 1,200" (taka sign, space, comma thousands).
func Format(n int) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := strconv.Itoa(n)
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	if neg {
		return "-৳ " + string(out)
	}
	return "৳ " + string(out)
}
```

- [ ] **Step 4: Run to verify they pass**

Run: `go mod tidy && go test ./internal/store/ ./internal/money/`
Expected: PASS

- [ ] **Step 5: Write the failing DB tests for categories and zones**

`avalonshop/internal/store/categories_test.go`:

```go
package store_test

import (
	"context"
	"errors"
	"testing"

	"avalonshop/internal/store"
	"avalonshop/internal/store/storetest"
)

func TestCategoriesCRUD(t *testing.T) {
	st := store.New(storetest.Pool(t))
	ctx := context.Background()
	id, err := st.CreateCategory(ctx, store.Category{Slug: "honey", Name: "Honey", Sort: 2})
	if err != nil || id == 0 {
		t.Fatal(err)
	}
	if _, err := st.CreateCategory(ctx, store.Category{Slug: "honey", Name: "Dup"}); !errors.Is(err, store.ErrDuplicate) {
		t.Fatalf("want ErrDuplicate, got %v", err)
	}
	st.CreateCategory(ctx, store.Category{Slug: "mango", Name: "Mango", Sort: 1})
	list, _ := st.ListCategories(ctx)
	if len(list) != 2 || list[0].Slug != "mango" {
		t.Fatalf("order by sort wrong: %+v", list)
	}
	c, err := st.GetCategoryBySlug(ctx, "honey")
	if err != nil || c.ID != id {
		t.Fatal(err)
	}
	c.Name = "Raw Honey"
	if err := st.UpdateCategory(ctx, c); err != nil {
		t.Fatal(err)
	}
	c2, _ := st.GetCategory(ctx, id)
	if c2.Name != "Raw Honey" {
		t.Fatalf("update lost: %+v", c2)
	}
	if _, err := st.GetCategoryBySlug(ctx, "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	slug, _ := st.UniqueSlug(ctx, "categories", "honey", 0)
	if slug != "honey-2" {
		t.Fatalf("UniqueSlug = %q", slug)
	}
	slug, _ = st.UniqueSlug(ctx, "categories", "honey", id)
	if slug != "honey" {
		t.Fatalf("UniqueSlug excluding self = %q", slug)
	}
	if err := st.DeleteCategory(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteCategory(ctx, id); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

func TestZonesCRUD(t *testing.T) {
	st := store.New(storetest.Pool(t))
	ctx := context.Background()
	a, _ := st.CreateZone(ctx, store.Zone{Name: "Rajshahi", Fee: 60, Active: true, Sort: 1})
	b, _ := st.CreateZone(ctx, store.Zone{Name: "Rest of BD", Fee: 120, Active: false, Sort: 2})
	all, _ := st.ListZones(ctx, false)
	active, _ := st.ListZones(ctx, true)
	if len(all) != 2 || len(active) != 1 || active[0].ID != a {
		t.Fatalf("all=%d active=%d", len(all), len(active))
	}
	z, _ := st.GetZone(ctx, b)
	z.Active = true
	z.Fee = 130
	if err := st.UpdateZone(ctx, z); err != nil {
		t.Fatal(err)
	}
	z2, _ := st.GetZone(ctx, b)
	if !z2.Active || z2.Fee != 130 {
		t.Fatalf("update lost: %+v", z2)
	}
	if err := st.DeleteZone(ctx, a); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetZone(ctx, a); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
```

- [ ] **Step 6: Run to verify it fails**

Run: `TEST_DATABASE_URL=postgres://avalon:avalon@localhost:5432/avalon?sslmode=disable go test ./internal/store/ -run 'Categories|Zones'`
Expected: FAIL, `undefined: store.Category`

- [ ] **Step 7: Write categories.go and zones.go**

`avalonshop/internal/store/categories.go`:

```go
package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type Category struct {
	ID   int64
	Slug string
	Name string
	Sort int
}

const categoryCols = `id, slug, name, sort`

func (s *Store) ListCategories(ctx context.Context) ([]Category, error) {
	rows, err := s.db.Query(ctx, `select `+categoryCols+` from categories order by sort, name`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[Category])
}

func (s *Store) GetCategoryBySlug(ctx context.Context, slug string) (Category, error) {
	rows, _ := s.db.Query(ctx, `select `+categoryCols+` from categories where slug = $1`, slug)
	c, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[Category])
	return c, mapErr(err)
}

func (s *Store) GetCategory(ctx context.Context, id int64) (Category, error) {
	rows, _ := s.db.Query(ctx, `select `+categoryCols+` from categories where id = $1`, id)
	c, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[Category])
	return c, mapErr(err)
}

func (s *Store) CreateCategory(ctx context.Context, c Category) (int64, error) {
	var id int64
	err := s.db.QueryRow(ctx, `insert into categories (slug, name, sort) values ($1, $2, $3) returning id`,
		c.Slug, c.Name, c.Sort).Scan(&id)
	return id, mapErr(err)
}

func (s *Store) UpdateCategory(ctx context.Context, c Category) error {
	tag, err := s.db.Exec(ctx, `update categories set slug = $2, name = $3, sort = $4 where id = $1`,
		c.ID, c.Slug, c.Name, c.Sort)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteCategory(ctx context.Context, id int64) error {
	tag, err := s.db.Exec(ctx, `delete from categories where id = $1`, id)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
```

`avalonshop/internal/store/zones.go`:

```go
package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type Zone struct {
	ID     int64
	Name   string
	Fee    int
	Active bool
	Sort   int
}

const zoneCols = `id, name, fee, active, sort`

func (s *Store) ListZones(ctx context.Context, activeOnly bool) ([]Zone, error) {
	rows, err := s.db.Query(ctx, `select `+zoneCols+` from delivery_zones where (not $1 or active) order by sort, name`, activeOnly)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[Zone])
}

func (s *Store) GetZone(ctx context.Context, id int64) (Zone, error) {
	rows, _ := s.db.Query(ctx, `select `+zoneCols+` from delivery_zones where id = $1`, id)
	z, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[Zone])
	return z, mapErr(err)
}

func (s *Store) CreateZone(ctx context.Context, z Zone) (int64, error) {
	var id int64
	err := s.db.QueryRow(ctx, `insert into delivery_zones (name, fee, active, sort) values ($1, $2, $3, $4) returning id`,
		z.Name, z.Fee, z.Active, z.Sort).Scan(&id)
	return id, mapErr(err)
}

func (s *Store) UpdateZone(ctx context.Context, z Zone) error {
	tag, err := s.db.Exec(ctx, `update delivery_zones set name = $2, fee = $3, active = $4, sort = $5 where id = $1`,
		z.ID, z.Name, z.Fee, z.Active, z.Sort)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteZone(ctx context.Context, id int64) error {
	tag, err := s.db.Exec(ctx, `delete from delivery_zones where id = $1`, id)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
```

- [ ] **Step 8: Run to verify it passes**

Run: `TEST_DATABASE_URL=postgres://avalon:avalon@localhost:5432/avalon?sslmode=disable go test ./internal/store/...`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add avalonshop
git commit -m "feat(shop): categories, zones, slugify, money formatting"
```

---

### Task 5: Store: products, variants, images, cart lookup, sitemap

**Files:**
- Create: `avalonshop/internal/store/products.go`, `avalonshop/internal/store/images.go`, `avalonshop/internal/store/products_test.go`

**Interfaces:**
- Produces (all methods on `*Store`, first arg `ctx context.Context`):
  - Types: `Product`, `Variant`, `Image`, `ProductFull{Product; Category *Category; Variants []Variant; Images []Image}`, `ProductCard`, `ListOpts`, `AdminProductRow`, `SitemapEntry{Slug string; UpdatedAt time.Time}`, `CartVariant`
  - `ListProductCards(opts ListOpts) ([]ProductCard, error)`
  - `GetProductBySlug(slug string, activeOnly bool) (ProductFull, error)`, `GetProduct(id int64) (ProductFull, error)`
  - `ListProductsAdmin() ([]AdminProductRow, error)`
  - `CreateProduct(p Product, vs []Variant) (int64, error)`, `UpdateProduct(p Product, vs []Variant) error`, `DeleteProduct(id int64) ([]Image, error)` (returns images so the caller can delete files; ErrInUse when any order item references its variants)
  - `ActiveProductsForSitemap() ([]SitemapEntry, error)`
  - `AddImage(img Image) (int64, error)`, `ListImages(productID int64) ([]Image, error)`, `UpdateImageAlt(id int64, alt string) error`, `MoveImage(id int64, up bool) error`, `DeleteImage(id int64) (Image, error)`
  - `VariantsForCart(ids []int64) ([]CartVariant, error)`

- [ ] **Step 1: Write the failing tests**

`avalonshop/internal/store/products_test.go`:

```go
package store_test

import (
	"context"
	"errors"
	"testing"

	"avalonshop/internal/store"
	"avalonshop/internal/store/storetest"
)

func seedProduct(t *testing.T, st *store.Store, slug string, active bool, prices ...int) int64 {
	t.Helper()
	ctx := context.Background()
	var vs []store.Variant
	for i, p := range prices {
		vs = append(vs, store.Variant{Name: []string{"250g", "500g", "1kg"}[i], Price: p, Stock: 5, Sort: i})
	}
	id, err := st.CreateProduct(ctx, store.Product{Slug: slug, Name: "Product " + slug, Description: "desc", Active: active, Featured: slug == "honey"}, vs)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestProductsListAndGet(t *testing.T) {
	st := store.New(storetest.Pool(t))
	ctx := context.Background()
	cat, _ := st.CreateCategory(ctx, store.Category{Slug: "honey", Name: "Honey"})
	honey := seedProduct(t, st, "honey", true, 900, 650)
	seedProduct(t, st, "mango", true, 1200)
	seedProduct(t, st, "hidden", false, 10)
	p, _ := st.GetProduct(ctx, honey)
	p.CategoryID = &cat
	if err := st.UpdateProduct(ctx, p.Product, p.Variants); err != nil {
		t.Fatal(err)
	}
	st.AddImage(ctx, store.Image{ProductID: honey, File: "abc", Alt: "jar", Width: 1600, Height: 1200})

	cards, err := st.ListProductCards(ctx, store.ListOpts{})
	if err != nil || len(cards) != 2 {
		t.Fatalf("want 2 active cards, got %d err=%v", len(cards), err)
	}
	var h store.ProductCard
	for _, c := range cards {
		if c.Slug == "honey" {
			h = c
		}
	}
	if h.MinPrice != 650 || h.VariantCount != 2 || !h.InStock || h.ImageFile == nil || *h.ImageFile != "abc" {
		t.Fatalf("card wrong: %+v", h)
	}
	cards, _ = st.ListProductCards(ctx, store.ListOpts{CategorySlug: "honey"})
	if len(cards) != 1 {
		t.Fatalf("category filter: %d", len(cards))
	}
	cards, _ = st.ListProductCards(ctx, store.ListOpts{Query: "MANG"})
	if len(cards) != 1 || cards[0].Slug != "mango" {
		t.Fatalf("search: %+v", cards)
	}
	cards, _ = st.ListProductCards(ctx, store.ListOpts{FeaturedOnly: true})
	if len(cards) != 1 || cards[0].Slug != "honey" {
		t.Fatalf("featured: %+v", cards)
	}
	cards, _ = st.ListProductCards(ctx, store.ListOpts{CategoryID: cat, ExcludeID: honey, Limit: 4})
	if len(cards) != 0 {
		t.Fatalf("related should exclude self: %+v", cards)
	}

	full, err := st.GetProductBySlug(ctx, "honey", true)
	if err != nil || len(full.Variants) != 2 || full.Variants[0].Name != "250g" || len(full.Images) != 1 || full.Category == nil || full.Category.Slug != "honey" {
		t.Fatalf("full: %+v err=%v", full, err)
	}
	if _, err := st.GetProductBySlug(ctx, "hidden", true); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("inactive should be not found for store, got %v", err)
	}
	if _, err := st.GetProductBySlug(ctx, "hidden", false); err != nil {
		t.Fatalf("inactive should be visible to admin: %v", err)
	}
	entries, _ := st.ActiveProductsForSitemap(ctx)
	if len(entries) != 2 {
		t.Fatalf("sitemap: %+v", entries)
	}
	rows, _ := st.ListProductsAdmin(ctx)
	if len(rows) != 3 {
		t.Fatalf("admin rows: %d", len(rows))
	}
}

func TestProductUpdateVariantsAndDelete(t *testing.T) {
	st := store.New(storetest.Pool(t))
	ctx := context.Background()
	id := seedProduct(t, st, "honey", true, 900, 650)
	p, _ := st.GetProduct(ctx, id)
	// keep first (renamed), drop second, add third
	vs := []store.Variant{{ID: p.Variants[0].ID, Name: "250 g", Price: 950, Stock: 3, Sort: 0}, {Name: "2kg", Price: 2400, Stock: 1, Sort: 1}}
	p.Name = "Wild Honey"
	if err := st.UpdateProduct(ctx, p.Product, vs); err != nil {
		t.Fatal(err)
	}
	p2, _ := st.GetProduct(ctx, id)
	if p2.Name != "Wild Honey" || len(p2.Variants) != 2 || p2.Variants[0].Name != "250 g" || p2.Variants[0].Price != 950 || p2.Variants[1].Name != "2kg" {
		t.Fatalf("variants wrong: %+v", p2.Variants)
	}
	if !p2.UpdatedAt.After(p.UpdatedAt) {
		t.Fatal("updated_at not bumped")
	}
	st.AddImage(ctx, store.Image{ProductID: id, File: "img1", Width: 400, Height: 300})
	imgs, err := st.DeleteProduct(ctx, id)
	if err != nil || len(imgs) != 1 || imgs[0].File != "img1" {
		t.Fatalf("delete: %+v %v", imgs, err)
	}
	if _, err := st.GetProduct(ctx, id); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("product still exists")
	}
}

func TestImagesOrderAndCartLookup(t *testing.T) {
	st := store.New(storetest.Pool(t))
	ctx := context.Background()
	id := seedProduct(t, st, "honey", true, 650)
	a, _ := st.AddImage(ctx, store.Image{ProductID: id, File: "a", Width: 400, Height: 400})
	b, _ := st.AddImage(ctx, store.Image{ProductID: id, File: "b", Width: 400, Height: 400})
	imgs, _ := st.ListImages(ctx, id)
	if imgs[0].ID != a || imgs[1].ID != b {
		t.Fatalf("initial order: %+v", imgs)
	}
	if err := st.MoveImage(ctx, b, true); err != nil {
		t.Fatal(err)
	}
	imgs, _ = st.ListImages(ctx, id)
	if imgs[0].ID != b {
		t.Fatalf("move up failed: %+v", imgs)
	}
	if err := st.MoveImage(ctx, b, true); err != nil {
		t.Fatalf("moving the first image up should be a no-op: %v", err)
	}
	st.UpdateImageAlt(ctx, a, "jar of honey")
	del, err := st.DeleteImage(ctx, a)
	if err != nil || del.Alt != "jar of honey" {
		t.Fatalf("delete image: %+v %v", del, err)
	}
	p, _ := st.GetProduct(ctx, id)
	cv, err := st.VariantsForCart(ctx, []int64{p.Variants[0].ID, 999999})
	if err != nil || len(cv) != 1 || cv[0].ProductSlug != "honey" || cv[0].Price != 650 || cv[0].ImageFile == nil || *cv[0].ImageFile != "b" {
		t.Fatalf("cart lookup: %+v %v", cv, err)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `TEST_DATABASE_URL=postgres://avalon:avalon@localhost:5432/avalon?sslmode=disable go test ./internal/store/ -run 'Product|Images'`
Expected: FAIL, `undefined: store.Product`

- [ ] **Step 3: Write products.go**

```go
package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type Product struct {
	ID              int64
	Slug            string
	Name            string
	Description     string
	CategoryID      *int64 `db:"category_id"`
	MetaDescription string `db:"meta_description"`
	Active          bool
	Featured        bool
	CreatedAt       time.Time `db:"created_at"`
	UpdatedAt       time.Time `db:"updated_at"`
}

type Variant struct {
	ID        int64
	ProductID int64 `db:"product_id"`
	Name      string
	SKU       *string `db:"sku"`
	Price     int
	Stock     int
	Sort      int
}

type ProductFull struct {
	Product
	Category *Category
	Variants []Variant
	Images   []Image
}

// ProductCard is one tile in a grid: product plus cheapest price and first image.
type ProductCard struct {
	ID           int64
	Slug         string
	Name         string
	MinPrice     int     `db:"min_price"`
	VariantCount int     `db:"variant_count"`
	InStock      bool    `db:"in_stock"`
	ImageFile    *string `db:"image_file"`
	ImageAlt     *string `db:"image_alt"`
	ImageWidth   *int    `db:"image_width"`
	ImageHeight  *int    `db:"image_height"`
}

type ListOpts struct {
	CategorySlug string
	CategoryID   int64 // 0 = any
	Query        string
	FeaturedOnly bool
	ExcludeID    int64
	Limit        int // 0 = 200
}

type AdminProductRow struct {
	ID           int64
	Slug         string
	Name         string
	Category     string
	VariantCount int  `db:"variant_count"`
	Active       bool
	Featured     bool
	ImageFile    *string `db:"image_file"`
}

type SitemapEntry struct {
	Slug      string
	UpdatedAt time.Time `db:"updated_at"`
}

const productCols = `id, slug, name, description, category_id, meta_description, active, featured, created_at, updated_at`
const variantCols = `id, product_id, name, sku, price, stock, sort`

func (s *Store) ListProductCards(ctx context.Context, o ListOpts) ([]ProductCard, error) {
	if o.Limit == 0 {
		o.Limit = 200
	}
	rows, err := s.db.Query(ctx, `
		select p.id, p.slug, p.name,
		       coalesce(v.min_price, 0) as min_price,
		       coalesce(v.n, 0)::int as variant_count,
		       coalesce(v.in_stock, false) as in_stock,
		       i.file as image_file, i.alt as image_alt, i.width as image_width, i.height as image_height
		from products p
		left join lateral (select min(price) min_price, count(*) n, bool_or(stock > 0) in_stock
		                   from variants where product_id = p.id) v on true
		left join lateral (select file, alt, width, height from product_images
		                   where product_id = p.id order by sort, id limit 1) i on true
		where p.active
		  and ($1 = '' or p.category_id = (select id from categories where slug = $1))
		  and ($2 = 0 or p.category_id = $2)
		  and ($3 = '' or p.name ilike '%' || $3 || '%')
		  and (not $4 or p.featured)
		  and p.id <> $5
		order by p.name
		limit $6`,
		o.CategorySlug, o.CategoryID, o.Query, o.FeaturedOnly, o.ExcludeID, o.Limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[ProductCard])
}

func (s *Store) getProductWhere(ctx context.Context, where string, arg any, activeOnly bool) (ProductFull, error) {
	var f ProductFull
	rows, _ := s.db.Query(ctx, `select `+productCols+` from products where `+where+` and (not $2 or active)`, arg, activeOnly)
	p, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[Product])
	if err != nil {
		return f, mapErr(err)
	}
	f.Product = p
	if p.CategoryID != nil {
		c, err := s.GetCategory(ctx, *p.CategoryID)
		if err == nil {
			f.Category = &c
		}
	}
	vrows, err := s.db.Query(ctx, `select `+variantCols+` from variants where product_id = $1 order by sort, id`, p.ID)
	if err != nil {
		return f, err
	}
	if f.Variants, err = pgx.CollectRows(vrows, pgx.RowToStructByName[Variant]); err != nil {
		return f, err
	}
	f.Images, err = s.ListImages(ctx, p.ID)
	return f, err
}

func (s *Store) GetProductBySlug(ctx context.Context, slug string, activeOnly bool) (ProductFull, error) {
	return s.getProductWhere(ctx, "slug = $1", slug, activeOnly)
}

func (s *Store) GetProduct(ctx context.Context, id int64) (ProductFull, error) {
	return s.getProductWhere(ctx, "id = $1", id, false)
}

func (s *Store) ListProductsAdmin(ctx context.Context) ([]AdminProductRow, error) {
	rows, err := s.db.Query(ctx, `
		select p.id, p.slug, p.name, coalesce(c.name, '') as category,
		       (select count(*) from variants where product_id = p.id)::int as variant_count,
		       p.active, p.featured,
		       (select file from product_images where product_id = p.id order by sort, id limit 1) as image_file
		from products p left join categories c on c.id = p.category_id
		order by p.name`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[AdminProductRow])
}

func (s *Store) CreateProduct(ctx context.Context, p Product, vs []Variant) (int64, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var id int64
	err = tx.QueryRow(ctx, `insert into products (slug, name, description, category_id, meta_description, active, featured)
		values ($1, $2, $3, $4, $5, $6, $7) returning id`,
		p.Slug, p.Name, p.Description, p.CategoryID, p.MetaDescription, p.Active, p.Featured).Scan(&id)
	if err != nil {
		return 0, mapErr(err)
	}
	if err := syncVariants(ctx, tx, id, vs); err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}

// UpdateProduct saves the product and makes its variants match vs exactly:
// rows with ID > 0 are updated, ID == 0 inserted, and existing rows not in vs deleted.
func (s *Store) UpdateProduct(ctx context.Context, p Product, vs []Variant) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `update products set slug = $2, name = $3, description = $4, category_id = $5,
		meta_description = $6, active = $7, featured = $8, updated_at = clock_timestamp() where id = $1`,
		p.ID, p.Slug, p.Name, p.Description, p.CategoryID, p.MetaDescription, p.Active, p.Featured)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := syncVariants(ctx, tx, p.ID, vs); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func syncVariants(ctx context.Context, tx pgx.Tx, productID int64, vs []Variant) error {
	keep := []int64{0}
	for i, v := range vs {
		var sku *string
		if v.SKU != nil && *v.SKU != "" {
			sku = v.SKU
		}
		if v.ID > 0 {
			if _, err := tx.Exec(ctx, `update variants set name = $3, sku = $4, price = $5, stock = $6, sort = $7 where id = $1 and product_id = $2`,
				v.ID, productID, v.Name, sku, v.Price, v.Stock, i); err != nil {
				return mapErr(err)
			}
			keep = append(keep, v.ID)
		} else {
			var id int64
			if err := tx.QueryRow(ctx, `insert into variants (product_id, name, sku, price, stock, sort) values ($1, $2, $3, $4, $5, $6) returning id`,
				productID, v.Name, sku, v.Price, v.Stock, i).Scan(&id); err != nil {
				return mapErr(err)
			}
			keep = append(keep, id)
		}
	}
	_, err := tx.Exec(ctx, `delete from variants where product_id = $1 and id <> all($2)`, productID, keep)
	return mapErr(err)
}

// DeleteProduct removes the product and returns its image rows so the caller
// can delete files. Refuses with ErrInUse if any order references its variants.
func (s *Store) DeleteProduct(ctx context.Context, id int64) ([]Image, error) {
	var used bool
	if err := s.db.QueryRow(ctx, `select exists(select 1 from order_items oi join variants v on v.id = oi.variant_id where v.product_id = $1)`, id).Scan(&used); err != nil {
		return nil, err
	}
	if used {
		return nil, ErrInUse
	}
	imgs, err := s.ListImages(ctx, id)
	if err != nil {
		return nil, err
	}
	tag, err := s.db.Exec(ctx, `delete from products where id = $1`, id)
	if err != nil {
		return nil, mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return imgs, nil
}

func (s *Store) ActiveProductsForSitemap(ctx context.Context) ([]SitemapEntry, error) {
	rows, err := s.db.Query(ctx, `select slug, updated_at from products where active order by slug`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[SitemapEntry])
}

// CartVariant is everything the cart needs to show one line.
type CartVariant struct {
	VariantID   int64   `db:"variant_id"`
	ProductID   int64   `db:"product_id"`
	ProductSlug string  `db:"product_slug"`
	ProductName string  `db:"product_name"`
	VariantName string  `db:"variant_name"`
	Price       int
	Stock       int
	Active      bool
	ImageFile   *string `db:"image_file"`
	ImageAlt    *string `db:"image_alt"`
	ImageWidth  *int    `db:"image_width"`
	ImageHeight *int    `db:"image_height"`
}

func (s *Store) VariantsForCart(ctx context.Context, ids []int64) ([]CartVariant, error) {
	rows, err := s.db.Query(ctx, `
		select v.id as variant_id, p.id as product_id, p.slug as product_slug, p.name as product_name,
		       v.name as variant_name, v.price, v.stock, p.active,
		       i.file as image_file, i.alt as image_alt, i.width as image_width, i.height as image_height
		from variants v join products p on p.id = v.product_id
		left join lateral (select file, alt, width, height from product_images where product_id = p.id order by sort, id limit 1) i on true
		where v.id = any($1)`, ids)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[CartVariant])
}
```

- [ ] **Step 4: Write images.go**

```go
package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type Image struct {
	ID        int64
	ProductID int64 `db:"product_id"`
	File      string
	Alt       string
	Width     int
	Height    int
	Sort      int
}

const imageCols = `id, product_id, file, alt, width, height, sort`

func (s *Store) ListImages(ctx context.Context, productID int64) ([]Image, error) {
	rows, err := s.db.Query(ctx, `select `+imageCols+` from product_images where product_id = $1 order by sort, id`, productID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[Image])
}

func (s *Store) AddImage(ctx context.Context, img Image) (int64, error) {
	var id int64
	err := s.db.QueryRow(ctx, `insert into product_images (product_id, file, alt, width, height, sort)
		values ($1, $2, $3, $4, $5, (select coalesce(max(sort), 0) + 1 from product_images where product_id = $1)) returning id`,
		img.ProductID, img.File, img.Alt, img.Width, img.Height).Scan(&id)
	if err != nil {
		return 0, mapErr(err)
	}
	_, err = s.db.Exec(ctx, `update products set updated_at = clock_timestamp() where id = $1`, img.ProductID)
	return id, err
}

func (s *Store) UpdateImageAlt(ctx context.Context, id int64, alt string) error {
	tag, err := s.db.Exec(ctx, `update product_images set alt = $2 where id = $1`, id, alt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MoveImage swaps sort with the previous (up) or next (down) image of the same
// product. Moving past the end is a no-op.
func (s *Store) MoveImage(ctx context.Context, id int64, up bool) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, _ := tx.Query(ctx, `select `+imageCols+` from product_images where product_id = (select product_id from product_images where id = $1) order by sort, id for update`, id)
	imgs, err := pgx.CollectRows(rows, pgx.RowToStructByName[Image])
	if err != nil {
		return err
	}
	idx := -1
	for i, im := range imgs {
		if im.ID == id {
			idx = i
		}
	}
	if idx == -1 {
		return ErrNotFound
	}
	j := idx + 1
	if up {
		j = idx - 1
	}
	if j < 0 || j >= len(imgs) {
		return nil
	}
	imgs[idx], imgs[j] = imgs[j], imgs[idx]
	for i, im := range imgs { // renumber 1..n so ties from AddImage never matter
		if _, err := tx.Exec(ctx, `update product_images set sort = $2 where id = $1`, im.ID, i+1); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) DeleteImage(ctx context.Context, id int64) (Image, error) {
	rows, _ := s.db.Query(ctx, `delete from product_images where id = $1 returning `+imageCols, id)
	img, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[Image])
	if err != nil {
		return img, mapErr(err)
	}
	_, err = s.db.Exec(ctx, `update products set updated_at = clock_timestamp() where id = $1`, img.ProductID)
	return img, err
}
```

- [ ] **Step 5: Run to verify it passes**

Run: `TEST_DATABASE_URL=postgres://avalon:avalon@localhost:5432/avalon?sslmode=disable go test ./internal/store/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add avalonshop/internal/store
git commit -m "feat(shop): product, variant, image queries and cart lookup"
```

---

### Task 6: Store: users and admin seed

**Files:**
- Create: `avalonshop/internal/store/users.go`, `avalonshop/internal/store/users_test.go`
- Modify: `avalonshop/main.go`

**Interfaces:**
- Produces: `store.User{ID int64; Email, PasswordHash, Name, Phone, Address, Role string; CreatedAt time.Time}`; `CreateUser(ctx, email, passwordHash, name, role string) (User, error)` (ErrDuplicate on same email, case-insensitive), `GetUserByEmail(ctx, email) (User, error)`, `GetUser(ctx, id) (User, error)`, `UpdateProfile(ctx, id, name, phone, address) error`, `UpdatePassword(ctx, id, hash) error`, `SeedAdmin(ctx, email, password string) error` (no-op if either is empty or an admin already exists), `ListOrdersByUser` lives in Task 7.

- [ ] **Step 1: Add bcrypt**

Run: `go get golang.org/x/crypto@latest && go mod tidy`

- [ ] **Step 2: Write the failing test**

`avalonshop/internal/store/users_test.go`:

```go
package store_test

import (
	"context"
	"errors"
	"testing"

	"avalonshop/internal/store"
	"avalonshop/internal/store/storetest"

	"golang.org/x/crypto/bcrypt"
)

func TestUsers(t *testing.T) {
	st := store.New(storetest.Pool(t))
	ctx := context.Background()
	u, err := st.CreateUser(ctx, "Ana@Example.com", "hash1", "Ana", "customer")
	if err != nil || u.ID == 0 || u.Role != "customer" {
		t.Fatalf("%+v %v", u, err)
	}
	if _, err := st.CreateUser(ctx, "ana@example.com", "x", "Dup", "customer"); !errors.Is(err, store.ErrDuplicate) {
		t.Fatalf("want ErrDuplicate, got %v", err)
	}
	got, err := st.GetUserByEmail(ctx, "ANA@example.com")
	if err != nil || got.ID != u.ID {
		t.Fatalf("case-insensitive lookup failed: %v", err)
	}
	if err := st.UpdateProfile(ctx, u.ID, "Ana B", "01712345678", "Road 1, Rajshahi"); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdatePassword(ctx, u.ID, "hash2"); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetUser(ctx, u.ID)
	if got.Name != "Ana B" || got.Phone != "01712345678" || got.PasswordHash != "hash2" {
		t.Fatalf("updates lost: %+v", got)
	}
	if _, err := st.GetUser(ctx, 999); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestSeedAdmin(t *testing.T) {
	st := store.New(storetest.Pool(t))
	ctx := context.Background()
	if err := st.SeedAdmin(ctx, "", ""); err != nil {
		t.Fatal("empty seed should be a no-op")
	}
	if err := st.SeedAdmin(ctx, "admin@example.com", "secret123"); err != nil {
		t.Fatal(err)
	}
	a, err := st.GetUserByEmail(ctx, "admin@example.com")
	if err != nil || a.Role != "admin" || bcrypt.CompareHashAndPassword([]byte(a.PasswordHash), []byte("secret123")) != nil {
		t.Fatalf("admin not seeded correctly: %+v %v", a, err)
	}
	if err := st.SeedAdmin(ctx, "second@example.com", "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetUserByEmail(ctx, "second@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("second admin should not be seeded once one exists")
	}
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `TEST_DATABASE_URL=postgres://avalon:avalon@localhost:5432/avalon?sslmode=disable go test ./internal/store/ -run 'Users|Seed'`
Expected: FAIL, `undefined: st.CreateUser`

- [ ] **Step 4: Write users.go**

```go
package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID           int64
	Email        string
	PasswordHash string `db:"password_hash"`
	Name         string
	Phone        string
	Address      string
	Role         string
	CreatedAt    time.Time `db:"created_at"`
}

const userCols = `id, email, password_hash, name, phone, address, role, created_at`

func (s *Store) CreateUser(ctx context.Context, email, passwordHash, name, role string) (User, error) {
	rows, _ := s.db.Query(ctx, `insert into users (email, password_hash, name, role) values ($1, $2, $3, $4) returning `+userCols,
		email, passwordHash, name, role)
	u, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[User])
	return u, mapErr(err)
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (User, error) {
	rows, _ := s.db.Query(ctx, `select `+userCols+` from users where lower(email) = lower($1)`, email)
	u, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[User])
	return u, mapErr(err)
}

func (s *Store) GetUser(ctx context.Context, id int64) (User, error) {
	rows, _ := s.db.Query(ctx, `select `+userCols+` from users where id = $1`, id)
	u, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[User])
	return u, mapErr(err)
}

func (s *Store) UpdateProfile(ctx context.Context, id int64, name, phone, address string) error {
	tag, err := s.db.Exec(ctx, `update users set name = $2, phone = $3, address = $4 where id = $1`, id, name, phone, address)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdatePassword(ctx context.Context, id int64, hash string) error {
	tag, err := s.db.Exec(ctx, `update users set password_hash = $2 where id = $1`, id, hash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SeedAdmin creates the first admin from env on first boot. Does nothing if
// email or password is empty, or if any admin already exists.
func (s *Store) SeedAdmin(ctx context.Context, email, password string) error {
	if email == "" || password == "" {
		return nil
	}
	var n int
	if err := s.db.QueryRow(ctx, `select count(*) from users where role = 'admin'`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return err
	}
	_, err = s.CreateUser(ctx, email, string(hash), "Admin", "admin")
	return err
}
```

- [ ] **Step 5: Run to verify it passes**

Run: `TEST_DATABASE_URL=postgres://avalon:avalon@localhost:5432/avalon?sslmode=disable go test ./internal/store/...`
Expected: PASS

- [ ] **Step 6: Call SeedAdmin from main.go**

Replace `_ = store.New(pool) // used from Task 10 on` with:

```go
	st := store.New(pool)
	if err := st.SeedAdmin(ctx, cfg.AdminEmail, cfg.AdminPassword); err != nil {
		return err
	}
	_ = st // handed to the app in Task 10
```

- [ ] **Step 7: Commit**

```bash
git add avalonshop
git commit -m "feat(shop): users store and first-admin seed"
```

---

### Task 7: Store: orders with atomic stock

**Files:**
- Create: `avalonshop/internal/store/orders.go`, `avalonshop/internal/store/orders_test.go`

**Interfaces:**
- Produces:
  - `OrderLine{VariantID int64; Qty int}`, `NewOrder{UserID *int64; Name, Phone, Email, Address, Note string; ZoneID int64; Lines []OrderLine}`
  - `Order{ID int64; Number string; UserID *int64; Name, Phone, Email, Address, ZoneName string; DeliveryFee, Subtotal, Total int; Status, Note, AdminNote string; CreatedAt, UpdatedAt time.Time}`, `OrderItem{ID, OrderID int64; VariantID *int64; ProductName, VariantName string; UnitPrice, Qty int}`, `OrderFull{Order; Items []OrderItem}`
  - `type ErrOutOfStock struct{ VariantID int64; Name string; Available int }` implementing `error`
  - `PlaceOrder(ctx, NewOrder) (OrderFull, error)`, `GetOrderByNumber(ctx, number) (OrderFull, error)`, `GetOrder(ctx, id) (OrderFull, error)`, `ListOrders(ctx, status string, limit int) ([]Order, error)` (status "" = all), `ListOrdersByUser(ctx, userID) ([]Order, error)`, `UpdateOrderStatus(ctx, id, to string) (OrderFull, error)` (ErrTransition; restores stock on cancel), `SetAdminNote(ctx, id, note) error`, `CanTransition(from, to string) bool`, `Dashboard(ctx) (Dashboard, error)` with `Dashboard{NewOrders int; LowStock []LowStock; Recent []Order}`, `LowStock{ProductName, VariantName string; Stock int}`

- [ ] **Step 1: Write the failing tests**

`avalonshop/internal/store/orders_test.go`:

```go
package store_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"avalonshop/internal/store"
	"avalonshop/internal/store/storetest"
)

func orderFixture(t *testing.T) (*store.Store, store.NewOrder, int64) {
	t.Helper()
	st := store.New(storetest.Pool(t))
	ctx := context.Background()
	pid := seedProduct(t, st, "honey", true, 650)
	p, _ := st.GetProduct(ctx, pid)
	vid := p.Variants[0].ID
	zid, _ := st.CreateZone(ctx, store.Zone{Name: "Rajshahi", Fee: 60, Active: true})
	return st, store.NewOrder{Name: "Ana", Phone: "01712345678", Email: "ana@example.com", Address: "Road 1", ZoneID: zid,
		Lines: []store.OrderLine{{VariantID: vid, Qty: 2}}}, vid
}

func stock(t *testing.T, st *store.Store, vid int64) int {
	t.Helper()
	cv, _ := st.VariantsForCart(context.Background(), []int64{vid})
	return cv[0].Stock
}

func TestPlaceOrderDecrementsAndSnapshots(t *testing.T) {
	st, in, vid := orderFixture(t)
	ctx := context.Background()
	o, err := st.PlaceOrder(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if o.Number != "AV-001001" || o.Subtotal != 1300 || o.DeliveryFee != 60 || o.Total != 1360 || o.Status != "new" || o.ZoneName != "Rajshahi" {
		t.Fatalf("order wrong: %+v", o.Order)
	}
	if len(o.Items) != 1 || o.Items[0].ProductName != "Product honey" || o.Items[0].VariantName != "250g" || o.Items[0].UnitPrice != 650 || o.Items[0].Qty != 2 {
		t.Fatalf("items wrong: %+v", o.Items)
	}
	if stock(t, st, vid) != 3 {
		t.Fatalf("stock = %d, want 3", stock(t, st, vid))
	}
	o2, _ := st.PlaceOrder(ctx, in)
	if o2.Number != "AV-001002" {
		t.Fatalf("second number %q", o2.Number)
	}
	got, err := st.GetOrderByNumber(ctx, "AV-001001")
	if err != nil || got.ID != o.ID || len(got.Items) != 1 {
		t.Fatalf("get by number: %v", err)
	}
	list, _ := st.ListOrders(ctx, "new", 10)
	if len(list) != 2 || list[0].ID != o2.ID {
		t.Fatalf("list newest first: %+v", list)
	}
}

func TestPlaceOrderOversell(t *testing.T) {
	st, in, vid := orderFixture(t)
	in.Lines[0].Qty = 6 // stock is 5
	_, err := st.PlaceOrder(context.Background(), in)
	var oos store.ErrOutOfStock
	if !errors.As(err, &oos) || oos.Available != 5 || oos.VariantID != vid {
		t.Fatalf("want ErrOutOfStock{Available:5}, got %v", err)
	}
	if stock(t, st, vid) != 5 {
		t.Fatal("stock changed on failed order")
	}
}

func TestPlaceOrderConcurrentLastUnit(t *testing.T) {
	st, in, vid := orderFixture(t)
	in.Lines[0].Qty = 5
	var wg sync.WaitGroup
	okCount := 0
	var mu sync.Mutex
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := st.PlaceOrder(context.Background(), in); err == nil {
				mu.Lock()
				okCount++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if okCount != 1 || stock(t, st, vid) != 0 {
		t.Fatalf("ok=%d stock=%d", okCount, stock(t, st, vid))
	}
}

func TestStatusTransitionsAndCancelRestores(t *testing.T) {
	st, in, vid := orderFixture(t)
	ctx := context.Background()
	o, _ := st.PlaceOrder(ctx, in)
	if _, err := st.UpdateOrderStatus(ctx, o.ID, "delivered"); !errors.Is(err, store.ErrTransition) {
		t.Fatalf("new→delivered should fail, got %v", err)
	}
	o2, err := st.UpdateOrderStatus(ctx, o.ID, "confirmed")
	if err != nil || o2.Status != "confirmed" {
		t.Fatal(err)
	}
	st.UpdateOrderStatus(ctx, o.ID, "shipped")
	if _, err := st.UpdateOrderStatus(ctx, o.ID, "cancelled"); err != nil {
		t.Fatal(err)
	}
	if stock(t, st, vid) != 5 {
		t.Fatalf("stock after cancel = %d", stock(t, st, vid))
	}
	if _, err := st.UpdateOrderStatus(ctx, o.ID, "confirmed"); !errors.Is(err, store.ErrTransition) {
		t.Fatal("cancelled is terminal")
	}
	if err := st.SetAdminNote(ctx, o.ID, "called, wrong number"); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetOrder(ctx, o.ID)
	if got.AdminNote != "called, wrong number" {
		t.Fatal("note lost")
	}
}

func TestCanTransitionTable(t *testing.T) {
	ok := [][2]string{{"new", "confirmed"}, {"new", "cancelled"}, {"confirmed", "shipped"}, {"confirmed", "cancelled"}, {"shipped", "delivered"}, {"shipped", "cancelled"}}
	bad := [][2]string{{"new", "shipped"}, {"new", "delivered"}, {"delivered", "cancelled"}, {"cancelled", "new"}, {"confirmed", "new"}, {"new", "bogus"}}
	for _, c := range ok {
		if !store.CanTransition(c[0], c[1]) {
			t.Errorf("%s→%s should be allowed", c[0], c[1])
		}
	}
	for _, c := range bad {
		if store.CanTransition(c[0], c[1]) {
			t.Errorf("%s→%s should be rejected", c[0], c[1])
		}
	}
}

func TestDashboard(t *testing.T) {
	st, in, _ := orderFixture(t)
	ctx := context.Background()
	st.PlaceOrder(ctx, in) // leaves stock at 3 (<= 5 → low)
	d, err := st.Dashboard(ctx)
	if err != nil || d.NewOrders != 1 || len(d.Recent) != 1 || len(d.LowStock) != 1 || d.LowStock[0].Stock != 3 {
		t.Fatalf("%+v %v", d, err)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `TEST_DATABASE_URL=postgres://avalon:avalon@localhost:5432/avalon?sslmode=disable go test ./internal/store/ -run 'Order|Transition|Dashboard'`
Expected: FAIL, `undefined: store.NewOrder`

- [ ] **Step 3: Write orders.go**

```go
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type OrderLine struct {
	VariantID int64
	Qty       int
}

type NewOrder struct {
	UserID  *int64
	Name    string
	Phone   string
	Email   string
	Address string
	Note    string
	ZoneID  int64
	Lines   []OrderLine
}

type Order struct {
	ID          int64
	Number      string
	UserID      *int64 `db:"user_id"`
	Name        string
	Phone       string
	Email       string
	Address     string
	ZoneName    string `db:"zone_name"`
	DeliveryFee int    `db:"delivery_fee"`
	Subtotal    int
	Total       int
	Status      string
	Note        string
	AdminNote   string    `db:"admin_note"`
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
}

type OrderItem struct {
	ID          int64
	OrderID     int64  `db:"order_id"`
	VariantID   *int64 `db:"variant_id"`
	ProductName string `db:"product_name"`
	VariantName string `db:"variant_name"`
	UnitPrice   int    `db:"unit_price"`
	Qty         int
}

type OrderFull struct {
	Order
	Items []OrderItem
}

type ErrOutOfStock struct {
	VariantID int64
	Name      string
	Available int
}

func (e ErrOutOfStock) Error() string {
	return fmt.Sprintf("only %d of %s available", e.Available, e.Name)
}

var transitions = map[string][]string{
	"new":       {"confirmed", "cancelled"},
	"confirmed": {"shipped", "cancelled"},
	"shipped":   {"delivered", "cancelled"},
}

func CanTransition(from, to string) bool {
	for _, t := range transitions[from] {
		if t == to {
			return true
		}
	}
	return false
}

const orderCols = `id, number, user_id, name, phone, email, address, zone_name, delivery_fee, subtotal, total, status, note, admin_note, created_at, updated_at`
const itemCols = `id, order_id, variant_id, product_name, variant_name, unit_price, qty`

// PlaceOrder locks each variant row, verifies stock, decrements it, and
// inserts the order with snapshotted names and prices, all in one transaction.
func (s *Store) PlaceOrder(ctx context.Context, in NewOrder) (OrderFull, error) {
	var out OrderFull
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)

	var zoneName string
	var fee int
	if err := tx.QueryRow(ctx, `select name, fee from delivery_zones where id = $1 and active`, in.ZoneID).Scan(&zoneName, &fee); err != nil {
		return out, mapErr(err)
	}
	type snap struct {
		productName, variantName string
		price                    int
	}
	snaps := make([]snap, len(in.Lines))
	subtotal := 0
	for i, l := range in.Lines {
		var sn snap
		var stock int
		var active bool
		err := tx.QueryRow(ctx, `select p.name, v.name, v.price, v.stock, p.active from variants v join products p on p.id = v.product_id where v.id = $1 for update of v`, l.VariantID).
			Scan(&sn.productName, &sn.variantName, &sn.price, &stock, &active)
		if err != nil {
			return out, mapErr(err)
		}
		if !active {
			return out, ErrNotFound
		}
		if stock < l.Qty {
			return out, ErrOutOfStock{VariantID: l.VariantID, Name: sn.productName + " " + sn.variantName, Available: stock}
		}
		if _, err := tx.Exec(ctx, `update variants set stock = stock - $2 where id = $1`, l.VariantID, l.Qty); err != nil {
			return out, err
		}
		snaps[i] = sn
		subtotal += sn.price * l.Qty
	}
	rows, _ := tx.Query(ctx, `insert into orders (number, user_id, name, phone, email, address, zone_name, delivery_fee, subtotal, total, note)
		values ('AV-' || lpad(nextval('order_number_seq')::text, 6, '0'), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10) returning `+orderCols,
		in.UserID, in.Name, in.Phone, in.Email, in.Address, zoneName, fee, subtotal, subtotal+fee, in.Note)
	out.Order, err = pgx.CollectOneRow(rows, pgx.RowToStructByName[Order])
	if err != nil {
		return out, err
	}
	for i, l := range in.Lines {
		irows, _ := tx.Query(ctx, `insert into order_items (order_id, variant_id, product_name, variant_name, unit_price, qty) values ($1, $2, $3, $4, $5, $6) returning `+itemCols,
			out.ID, l.VariantID, snaps[i].productName, snaps[i].variantName, snaps[i].price, l.Qty)
		item, err := pgx.CollectOneRow(irows, pgx.RowToStructByName[OrderItem])
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, item)
	}
	return out, tx.Commit(ctx)
}

func (s *Store) getOrderWhere(ctx context.Context, where string, arg any) (OrderFull, error) {
	var out OrderFull
	rows, _ := s.db.Query(ctx, `select `+orderCols+` from orders where `+where, arg)
	o, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[Order])
	if err != nil {
		return out, mapErr(err)
	}
	out.Order = o
	irows, err := s.db.Query(ctx, `select `+itemCols+` from order_items where order_id = $1 order by id`, o.ID)
	if err != nil {
		return out, err
	}
	out.Items, err = pgx.CollectRows(irows, pgx.RowToStructByName[OrderItem])
	return out, err
}

func (s *Store) GetOrderByNumber(ctx context.Context, number string) (OrderFull, error) {
	return s.getOrderWhere(ctx, "number = $1", number)
}

func (s *Store) GetOrder(ctx context.Context, id int64) (OrderFull, error) {
	return s.getOrderWhere(ctx, "id = $1", id)
}

func (s *Store) ListOrders(ctx context.Context, status string, limit int) ([]Order, error) {
	rows, err := s.db.Query(ctx, `select `+orderCols+` from orders where ($1 = '' or status = $1) order by created_at desc, id desc limit $2`, status, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[Order])
}

func (s *Store) ListOrdersByUser(ctx context.Context, userID int64) ([]Order, error) {
	rows, err := s.db.Query(ctx, `select `+orderCols+` from orders where user_id = $1 order by created_at desc, id desc`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[Order])
}

// UpdateOrderStatus enforces the transition table and restores stock when cancelling.
func (s *Store) UpdateOrderStatus(ctx context.Context, id int64, to string) (OrderFull, error) {
	var out OrderFull
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	var from string
	if err := tx.QueryRow(ctx, `select status from orders where id = $1 for update`, id).Scan(&from); err != nil {
		return out, mapErr(err)
	}
	if !CanTransition(from, to) {
		return out, ErrTransition
	}
	if to == "cancelled" {
		if _, err := tx.Exec(ctx, `update variants v set stock = v.stock + oi.qty from order_items oi where oi.order_id = $1 and oi.variant_id = v.id`, id); err != nil {
			return out, err
		}
	}
	if _, err := tx.Exec(ctx, `update orders set status = $2, updated_at = clock_timestamp() where id = $1`, id, to); err != nil {
		return out, err
	}
	if err := tx.Commit(ctx); err != nil {
		return out, err
	}
	return s.GetOrder(ctx, id)
}

func (s *Store) SetAdminNote(ctx context.Context, id int64, note string) error {
	tag, err := s.db.Exec(ctx, `update orders set admin_note = $2, updated_at = clock_timestamp() where id = $1`, id, note)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

type LowStock struct {
	ProductName string `db:"product_name"`
	VariantName string `db:"variant_name"`
	Stock       int
}

type Dashboard struct {
	NewOrders int
	LowStock  []LowStock
	Recent    []Order
}

func (s *Store) Dashboard(ctx context.Context) (Dashboard, error) {
	var d Dashboard
	if err := s.db.QueryRow(ctx, `select count(*) from orders where status = 'new'`).Scan(&d.NewOrders); err != nil {
		return d, err
	}
	rows, err := s.db.Query(ctx, `select p.name as product_name, v.name as variant_name, v.stock from variants v join products p on p.id = v.product_id where v.stock <= 5 and p.active order by v.stock, p.name`)
	if err != nil {
		return d, err
	}
	if d.LowStock, err = pgx.CollectRows(rows, pgx.RowToStructByName[LowStock]); err != nil {
		return d, err
	}
	d.Recent, err = s.ListOrders(ctx, "", 10)
	return d, err
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `TEST_DATABASE_URL=postgres://avalon:avalon@localhost:5432/avalon?sslmode=disable go test ./internal/store/... -count=1`
Expected: PASS (including the concurrent test)

- [ ] **Step 5: Commit**

```bash
git add avalonshop/internal/store
git commit -m "feat(shop): orders with row-locked stock decrement and status transitions"
```

---

### Task 8: Image pipeline (sniff, EXIF orientation, resize, WebP)

**Files:**
- Create: `avalonshop/internal/img/img.go`, `avalonshop/internal/img/exif.go`, `avalonshop/internal/img/img_test.go`

**Interfaces:**
- Produces:
  - `img.ErrUnsupported`, `img.Widths = []int{400, 900, 1600}`
  - `img.Result{Stem string; Width, Height int; Widths []int}` (Widths actually produced, ascending)
  - `img.Process(data []byte, dir string) (Result, error)` writes `{stem}-{w}.webp` files into dir
  - `img.Filename(stem string, w int) string`
  - `img.WidthsFor(width int) []int` — the widths that exist for an image whose largest variant is `width`: every standard width `< width`, plus `width` itself. Templates use this to build `srcset`.
  - `img.Remove(dir, stem string, width int)` deletes all files for the stem (best effort)
  - `img.Orientation(jpeg []byte) int` (1–8; 1 when absent)

- [ ] **Step 1: Add dependencies**

Run: `cd avalonshop && go get golang.org/x/image@latest github.com/gen2brain/webp@latest && go mod tidy`

- [ ] **Step 2: Write the failing tests**

`avalonshop/internal/img/img_test.go`:

```go
package img

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/gen2brain/webp"
)

func gradient(w, h int) *image.NRGBA {
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			m.Set(x, y, color.NRGBA{uint8(x * 255 / w), uint8(y * 255 / h), 90, 255})
		}
	}
	return m
}

func pngBytes(t *testing.T, m image.Image) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, m); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func decodeWebP(t *testing.T, path string) image.Image {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	m, err := webp.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestProcessLarge(t *testing.T) {
	dir := t.TempDir()
	r, err := Process(pngBytes(t, gradient(2000, 1500)), dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Width != 1600 || r.Height != 1200 || len(r.Stem) != 16 {
		t.Fatalf("result %+v", r)
	}
	if len(r.Widths) != 3 || r.Widths[0] != 400 || r.Widths[2] != 1600 {
		t.Fatalf("widths %v", r.Widths)
	}
	for _, w := range r.Widths {
		m := decodeWebP(t, filepath.Join(dir, Filename(r.Stem, w)))
		if m.Bounds().Dx() != w || m.Bounds().Dy() != w*3/4 {
			t.Fatalf("variant %d has bounds %v", w, m.Bounds())
		}
	}
}

func TestProcessSmallNeverUpscales(t *testing.T) {
	dir := t.TempDir()
	r, err := Process(pngBytes(t, gradient(600, 600)), dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Width != 600 || r.Height != 600 || len(r.Widths) != 2 || r.Widths[0] != 400 || r.Widths[1] != 600 {
		t.Fatalf("result %+v", r)
	}
	if got := WidthsFor(600); len(got) != 2 || got[1] != 600 {
		t.Fatalf("WidthsFor(600) = %v", got)
	}
	if got := WidthsFor(1600); len(got) != 3 {
		t.Fatalf("WidthsFor(1600) = %v", got)
	}
	Remove(dir, r.Stem, r.Width)
	if _, err := os.Stat(filepath.Join(dir, Filename(r.Stem, 400))); !os.IsNotExist(err) {
		t.Fatal("Remove left files behind")
	}
}

func TestProcessRejectsNonImage(t *testing.T) {
	if _, err := Process([]byte("%PDF-1.4 not an image"), t.TempDir()); err != ErrUnsupported {
		t.Fatalf("want ErrUnsupported, got %v", err)
	}
}

// buildJPEGWithOrientation wraps a real JPEG with an APP1 EXIF segment carrying tag 0x0112.
func buildJPEGWithOrientation(t *testing.T, m image.Image, o uint16) []byte {
	var raw bytes.Buffer
	jpeg.Encode(&raw, m, nil)
	j := raw.Bytes()
	tiff := []byte("MM\x00\x2A\x00\x00\x00\x08") // big-endian, IFD0 at offset 8
	ifd := make([]byte, 2+12+4)
	binary.BigEndian.PutUint16(ifd[0:], 1)      // one entry
	binary.BigEndian.PutUint16(ifd[2:], 0x0112) // Orientation
	binary.BigEndian.PutUint16(ifd[4:], 3)      // SHORT
	binary.BigEndian.PutUint32(ifd[6:], 1)      // count
	binary.BigEndian.PutUint16(ifd[10:], o)     // value
	payload := append([]byte("Exif\x00\x00"), append(tiff, ifd...)...)
	seg := []byte{0xFF, 0xE1, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}
	seg = append(seg, payload...)
	out := append([]byte{}, j[:2]...) // SOI
	out = append(out, seg...)
	return append(out, j[2:]...)
}

func TestOrientationParseAndApply(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	// mark top-left red, top-right blue
	src.Set(0, 0, color.NRGBA{255, 0, 0, 255})
	src.Set(3, 0, color.NRGBA{0, 0, 255, 255})
	data := buildJPEGWithOrientation(t, src, 6)
	if got := Orientation(data); got != 6 {
		t.Fatalf("Orientation = %d", got)
	}
	if got := Orientation(pngBytes(t, src)); got != 1 {
		t.Fatalf("PNG should report 1, got %d", got)
	}
	rot := applyOrientation(src, 6) // 90° clockwise: 4x2 → 2x4, top-left goes to top-right
	if rot.Bounds().Dx() != 2 || rot.Bounds().Dy() != 4 {
		t.Fatalf("bounds %v", rot.Bounds())
	}
	if r, _, _, _ := rot.At(1, 0).RGBA(); r>>8 != 255 {
		t.Fatalf("top-left red should now be at (1,0), got %v", rot.At(1, 0))
	}
	if _, _, b, _ := rot.At(1, 3).RGBA(); b>>8 != 255 {
		t.Fatalf("top-right blue should now be at (1,3), got %v", rot.At(1, 3))
	}
	// end-to-end: a rotated-tag JPEG comes out portrait
	dir := t.TempDir()
	r, err := Process(buildJPEGWithOrientation(t, gradient(800, 400), 6), dir)
	if err != nil || r.Width != 400 || r.Height != 800 {
		t.Fatalf("expected 400x800 after orientation, got %+v err=%v", r, err)
	}
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `go test ./internal/img/`
Expected: FAIL, `undefined: Process`

- [ ] **Step 4: Write exif.go**

```go
package img

import "encoding/binary"

// Orientation returns the EXIF orientation (1–8) of a JPEG, or 1 if the file
// is not a JPEG or carries no orientation tag. ponytail: 50-line APP1/TIFF
// walker instead of an EXIF library; we only ever need tag 0x0112.
func Orientation(b []byte) int {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return 1
	}
	i := 2
	for i+4 <= len(b) {
		if b[i] != 0xFF {
			return 1
		}
		marker := b[i+1]
		switch {
		case marker == 0xD8, marker == 0x01, marker >= 0xD0 && marker <= 0xD7:
			i += 2 // standalone markers have no length
			continue
		case marker == 0xDA, marker == 0xD9:
			return 1 // start of scan / end: no APP1 ahead
		}
		size := int(b[i+2])<<8 | int(b[i+3])
		if size < 2 || i+2+size > len(b) {
			return 1
		}
		if marker == 0xE1 {
			return tiffOrientation(b[i+4 : i+2+size])
		}
		i += 2 + size
	}
	return 1
}

func tiffOrientation(seg []byte) int {
	if len(seg) < 14 || string(seg[:6]) != "Exif\x00\x00" {
		return 1
	}
	t := seg[6:]
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	if bo.Uint16(t[2:4]) != 0x2A {
		return 1
	}
	off := int(bo.Uint32(t[4:8]))
	if off+2 > len(t) {
		return 1
	}
	n := int(bo.Uint16(t[off : off+2]))
	for j := 0; j < n; j++ {
		e := off + 2 + j*12
		if e+12 > len(t) {
			return 1
		}
		if bo.Uint16(t[e:e+2]) == 0x0112 {
			if v := int(bo.Uint16(t[e+8 : e+10])); v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}
```

- [ ] **Step 5: Write img.go**

```go
// Package img turns an uploaded image into oriented, metadata-free WebP variants.
package img

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gen2brain/webp"
	xdraw "golang.org/x/image/draw"
)

var ErrUnsupported = errors.New("unsupported image type: use JPEG, PNG, WebP or GIF")

var Widths = []int{400, 900, 1600}

const quality = 82

type Result struct {
	Stem   string
	Width  int
	Height int
	Widths []int
}

func Filename(stem string, w int) string { return fmt.Sprintf("%s-%d.webp", stem, w) }

// WidthsFor lists the variants that exist for an image whose largest variant is width.
func WidthsFor(width int) []int {
	var out []int
	for _, w := range Widths {
		if w < width {
			out = append(out, w)
		}
	}
	return append(out, width)
}

func Remove(dir, stem string, width int) {
	for _, w := range WidthsFor(width) {
		os.Remove(filepath.Join(dir, Filename(stem, w)))
	}
}

func decode(data []byte) (image.Image, error) {
	r := bytes.NewReader(data)
	switch http.DetectContentType(data) {
	case "image/jpeg":
		m, err := jpeg.Decode(r)
		if err != nil {
			return nil, err
		}
		return applyOrientation(m, Orientation(data)), nil
	case "image/png":
		return png.Decode(r)
	case "image/gif":
		return gif.Decode(r) // first frame
	case "image/webp":
		return webp.Decode(r, webp.Options{AutoRotate: true})
	}
	return nil, ErrUnsupported
}

// Process decodes data, writes every applicable WebP variant into dir, and
// reports the largest variant's dimensions. On any failure it removes what it wrote.
func Process(data []byte, dir string) (Result, error) {
	src, err := decode(data)
	if err != nil {
		if errors.Is(err, ErrUnsupported) {
			return Result{}, ErrUnsupported
		}
		return Result{}, fmt.Errorf("decode: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Result{}, err
	}
	var stemBytes [8]byte
	rand.Read(stemBytes[:])
	res := Result{Stem: hex.EncodeToString(stemBytes[:])}
	ow := src.Bounds().Dx()
	targets := WidthsFor(min(ow, Widths[len(Widths)-1]))
	for _, w := range targets {
		m := resize(src, w)
		f, err := os.Create(filepath.Join(dir, Filename(res.Stem, w)))
		if err == nil {
			err = webp.Encode(f, m, webp.Options{Quality: quality})
			f.Close()
		}
		if err != nil {
			Remove(dir, res.Stem, targets[len(targets)-1])
			return Result{}, err
		}
		res.Widths = append(res.Widths, w)
		res.Width, res.Height = m.Bounds().Dx(), m.Bounds().Dy()
	}
	return res, nil
}

func resize(src image.Image, w int) *image.NRGBA {
	b := src.Bounds()
	if b.Dx() == w {
		dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
		return dst
	}
	h := int(math.Round(float64(b.Dy()) * float64(w) / float64(b.Dx())))
	if h < 1 {
		h = 1
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Src, nil)
	return dst
}

// applyOrientation maps EXIF orientation values 2–8 onto pixel moves.
func applyOrientation(src image.Image, o int) image.Image {
	if o <= 1 || o > 8 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	in := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(in, in.Bounds(), src, b.Min, draw.Src)
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	out := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch o {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			si, di := in.PixOffset(x, y), out.PixOffset(dx, dy)
			copy(out.Pix[di:di+4], in.Pix[si:si+4])
		}
	}
	return out
}
```

- [ ] **Step 6: Run to verify it passes**

Run: `go test ./internal/img/`
Expected: PASS. Note the first WebP encode warms up the wasm-transpiled encoder; the package tests may take a few seconds.

- [ ] **Step 7: Commit**

```bash
git add avalonshop
git commit -m "feat(shop): image pipeline with EXIF orientation and WebP variants"
```

---

### Task 9: Mail (SMTP to Brevo, text templates, async send)

**Files:**
- Create: `avalonshop/internal/mail/mail.go`, `avalonshop/internal/mail/mail_test.go`, `avalonshop/templates/email/order_confirmation.txt`, `avalonshop/templates/email/order_new_admin.txt`, `avalonshop/templates/email/order_shipped.txt`, `avalonshop/templates/email/order_delivered.txt`, `avalonshop/templates/email/password_reset.txt`

**Interfaces:**
- Produces: `mail.New(host, port, user, pass, from string, fsys fs.FS, log *slog.Logger) (*Mailer, error)` (parses `templates/email/*.txt` from fsys), `(*Mailer) Send(to, template string, data any)` (async, logs failure), `(*Mailer) SendNow(to, template string, data any) error`, `(*Mailer) Render(template string, data any) (subject, body string, err error)`, `(*Mailer) Wait()`.
- Template data contract (maps built by later tasks):
  - `order_confirmation`, `order_new_admin`, `order_shipped`, `order_delivered`: `map[string]any{"Order": store.Order, "Items": []store.OrderItem, "TrackURL": string, "AdminURL": string}`
  - `password_reset`: `map[string]any{"Name": string, "ResetURL": string}`
- Templates get a `taka` function.

- [ ] **Step 1: Write the email templates**

Each template's first line is `Subject: ...`, then a blank line, then the body.

`avalonshop/templates/email/order_confirmation.txt`:

```
Subject: Order {{.Order.Number}} received — Avalon

Hi {{.Order.Name}},

Thanks for your order. We'll call {{.Order.Phone}} to confirm before delivery.

Order {{.Order.Number}}
{{range .Items}}- {{.ProductName}} ({{.VariantName}}) × {{.Qty}} — {{taka .UnitPrice}} each
{{end}}
Subtotal: {{taka .Order.Subtotal}}
Delivery ({{.Order.ZoneName}}): {{taka .Order.DeliveryFee}}
Total (cash on delivery): {{taka .Order.Total}}

Deliver to:
{{.Order.Address}}

Track your order: {{.TrackURL}}

Avalon Corporation · Rajshahi
```

`avalonshop/templates/email/order_new_admin.txt`:

```
Subject: New order {{.Order.Number}} — {{taka .Order.Total}} — {{.Order.Name}}

New order {{.Order.Number}}

Customer: {{.Order.Name}}
Phone: {{.Order.Phone}}
Email: {{.Order.Email}}
Zone: {{.Order.ZoneName}} ({{taka .Order.DeliveryFee}})
Address:
{{.Order.Address}}
{{if .Order.Note}}
Customer note: {{.Order.Note}}
{{end}}
Items:
{{range .Items}}- {{.ProductName}} ({{.VariantName}}) × {{.Qty}} — {{taka .UnitPrice}}
{{end}}
Total: {{taka .Order.Total}}

Manage: {{.AdminURL}}
```

`avalonshop/templates/email/order_shipped.txt`:

```
Subject: Order {{.Order.Number}} is on its way — Avalon

Hi {{.Order.Name}},

Your order {{.Order.Number}} has been handed to delivery. Please keep {{taka .Order.Total}} ready in cash.

Track: {{.TrackURL}}

Avalon Corporation · Rajshahi
```

`avalonshop/templates/email/order_delivered.txt`:

```
Subject: Order {{.Order.Number}} delivered — Avalon

Hi {{.Order.Name}},

Order {{.Order.Number}} is marked delivered. Thank you for buying from Avalon.

If anything is wrong with your order, reply to this email.

Avalon Corporation · Rajshahi
```

`avalonshop/templates/email/password_reset.txt`:

```
Subject: Reset your Avalon password

Hi {{.Name}},

Use this link within one hour to set a new password:

{{.ResetURL}}

If you didn't ask for this, ignore this email.
```

- [ ] **Step 2: Write the failing test**

`avalonshop/internal/mail/mail_test.go`:

```go
package mail

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestRenderAndDevSend(t *testing.T) {
	var logbuf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logbuf, nil))
	m, err := New("", "587", "", "", "shop@example.com", os.DirFS("../.."), log)
	if err != nil {
		t.Fatal(err)
	}
	subj, body, err := m.Render("password_reset", map[string]any{"Name": "Ana", "ResetURL": "https://x/reset/abc"})
	if err != nil {
		t.Fatal(err)
	}
	if subj != "Reset your Avalon password" || !strings.Contains(body, "https://x/reset/abc") || strings.HasPrefix(body, "Subject:") {
		t.Fatalf("subject=%q body=%q", subj, body)
	}
	if err := m.SendNow("ana@example.com", "password_reset", map[string]any{"Name": "Ana", "ResetURL": "u"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logbuf.String(), "ana@example.com") {
		t.Fatalf("dev mode should log the send: %s", logbuf.String())
	}
	if _, _, err := m.Render("nope", nil); err == nil {
		t.Fatal("unknown template should error")
	}
	m.Send("ana@example.com", "password_reset", map[string]any{"Name": "Ana", "ResetURL": "u"})
	m.Wait()
}

func TestBuildMessage(t *testing.T) {
	msg := buildMessage("shop@example.com", "ana@example.com", "Order ৳ 650", "line one\nটাকা\n")
	s := string(msg)
	for _, want := range []string{"From: shop@example.com\r\n", "To: ana@example.com\r\n", "Subject: =?utf-8?q?", "Content-Type: text/plain; charset=utf-8\r\n", "Content-Transfer-Encoding: quoted-printable\r\n", "\r\n\r\nline one\r\n"} {
		if !strings.Contains(s, want) {
			t.Errorf("message missing %q:\n%s", want, s)
		}
	}
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `go test ./internal/mail/`
Expected: FAIL, `undefined: New`

- [ ] **Step 4: Write mail.go**

```go
// Package mail renders plain-text templates and sends them over SMTP.
package mail

import (
	"bytes"
	"fmt"
	"io/fs"
	"log/slog"
	"mime"
	"mime/quotedprintable"
	"net/smtp"
	"strings"
	"sync"
	"text/template"
	"time"

	"avalonshop/internal/money"
)

type Mailer struct {
	host, port, user, pass, from string
	tmpl                         *template.Template
	log                          *slog.Logger
	wg                           sync.WaitGroup
}

// New parses templates/email/*.txt. With an empty host, Send logs instead of sending.
func New(host, port, user, pass, from string, fsys fs.FS, log *slog.Logger) (*Mailer, error) {
	t, err := template.New("email").Funcs(template.FuncMap{"taka": money.Format}).ParseFS(fsys, "templates/email/*.txt")
	if err != nil {
		return nil, err
	}
	return &Mailer{host: host, port: port, user: user, pass: pass, from: from, tmpl: t, log: log}, nil
}

// Render executes the named template. The first line must be "Subject: ...".
func (m *Mailer) Render(name string, data any) (subject, body string, err error) {
	var buf bytes.Buffer
	if err := m.tmpl.ExecuteTemplate(&buf, name+".txt", data); err != nil {
		return "", "", err
	}
	first, rest, _ := strings.Cut(buf.String(), "\n")
	if !strings.HasPrefix(first, "Subject: ") {
		return "", "", fmt.Errorf("mail: template %s must start with a Subject line", name)
	}
	return strings.TrimPrefix(first, "Subject: "), strings.TrimLeft(rest, "\n"), nil
}

// Send delivers in the background. Failures are logged, never returned:
// an order must succeed even if email is down.
func (m *Mailer) Send(to, name string, data any) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		if err := m.SendNow(to, name, data); err != nil {
			m.log.Error("mail failed", "to", to, "template", name, "err", err)
		}
	}()
}

func (m *Mailer) Wait() { m.wg.Wait() }

func (m *Mailer) SendNow(to, name string, data any) error {
	subject, body, err := m.Render(name, data)
	if err != nil {
		return err
	}
	if m.host == "" {
		m.log.Info("mail (dev mode, not sent)", "to", to, "subject", subject, "body", body)
		return nil
	}
	msg := buildMessage(m.from, to, subject, body)
	auth := smtp.PlainAuth("", m.user, m.pass, m.host)
	return smtp.SendMail(m.host+":"+m.port, auth, m.from, []string{to}, msg)
}

func buildMessage(from, to, subject, body string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n",
		from, to, mime.QEncoding.Encode("utf-8", subject), time.Now().Format(time.RFC1123Z))
	qp := quotedprintable.NewWriter(&b)
	qp.Write([]byte(strings.ReplaceAll(body, "\n", "\r\n")))
	qp.Close()
	return b.Bytes()
}
```

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./internal/mail/`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add avalonshop
git commit -m "feat(shop): SMTP mailer with plain-text templates"
```

---

### Task 10: App core: templates, rendering, middleware, static assets, design system

**Files:**
- Create: `avalonshop/internal/app/app.go`, `render.go`, `middleware.go`, `cookies.go`, `limiter.go`, `app_test.go`, `limiter_test.go`, `avalonshop/templates/layout.html`, `avalonshop/templates/admin/layout.html`, `avalonshop/templates/store/404.html`, `avalonshop/templates/partials/cart_badge.html`, `avalonshop/static/app.css`, `avalonshop/static/app.js`, `avalonshop/static/favicon.svg`, `avalonshop/static/htmx.min.js`, `avalonshop/static/fonts/*.woff2`
- Modify: `avalonshop/main.go`

**Interfaces:**
- Produces:
  - `app.New(cfg config.Config, st *store.Store, m *mail.Mailer, templates, static fs.FS, log *slog.Logger) (*App, error)`, `(*App) Handler() http.Handler`
  - Internal helpers used by every later task: `a.render(w, r, name string, p page)`, `a.renderStatus(w, r, status int, name string, p page)`, `a.renderPartial(w, name string, data any)`, `a.notFound(w, r)`, `a.serverError(w, r, err)`, `isHTMX(r) bool`, `a.redirect(w, r, url string)` (303, or `HX-Redirect` for HTMX)
  - `page{Title, Description, Canonical, OGImage string; JSONLD template.JS; NoIndex bool; Data any}` (User, CartCount, Flash, V, BaseURL, Path filled by render)
  - Cookies: `a.cartLines(r) []token.CartLine`, `a.saveCart(w, lines)`, `cartCount(lines) int`, `a.currentUser(r) *store.User`, `a.login(w, userID int64)`, `a.logout(w)`, `a.setFlash(w, msg)`, `a.popFlash(w, r) string`
  - `clientIP(r) string`, `newLimiter(limit int, window time.Duration) *limiter`, `(*limiter) Allow(key string) bool` (records + checks), `(*limiter) Blocked(key) bool`, `(*limiter) Hit(key)`
  - Template funcs: `taka`, `imgURL stem width`, `srcset stem width`, `dhaka time`, `deref *string`, `derefInt *int`, `add`, `mul`, `jsonld any`
  - Routes registered here: `GET /healthz`, `GET /static/`, `GET /media/`, and a `/` fallback that renders the 404 page. Later tasks add lines to `routes()`.

- [ ] **Step 1: Fetch htmx and fonts, write the favicon**

```bash
cd avalonshop && mkdir -p static/fonts
curl -fsSL -o static/htmx.min.js https://cdn.jsdelivr.net/npm/htmx.org@2/dist/htmx.min.js
head -c 200 static/htmx.min.js   # confirm it starts with a version comment for htmx 2.x

UA="Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0 Safari/537.36"
css=$(curl -fsSL -A "$UA" "https://fonts.googleapis.com/css2?family=Manrope:wght@400..700&family=DM+Mono:wght@400;500&display=swap")
# One URL per @font-face block that is preceded by "/* latin */" — order: Manrope, DM Mono 400, DM Mono 500
urls=$(printf '%s\n' "$css" | awk '/\/\* latin \*\//{f=1} f && /url\(/{sub(/.*url\(/,""); sub(/\).*/,""); print; f=0}')
set -- $urls
curl -fsSL -o static/fonts/manrope.woff2 "$1"
curl -fsSL -o static/fonts/dmmono-400.woff2 "$2"
curl -fsSL -o static/fonts/dmmono-500.woff2 "$3"
ls -la static/fonts   # expect three files, roughly 20–60 KB each
```

If the Google CSS endpoint is unreachable, fall back to the TTFs from the google/fonts repo (`ofl/manrope/Manrope[wght].ttf`, `ofl/dmmono/DMMono-Regular.ttf`, `ofl/dmmono/DMMono-Medium.ttf`) and change the `format("woff2")` hints in `app.css` to `format("truetype")`.

`avalonshop/static/favicon.svg`:

```svg
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><rect width="32" height="32" rx="8" fill="#1f6b3a"/><path d="M16 6l8 20h-4.4l-1.7-4.6h-3.8L12.4 26H8z M15.2 18.2h1.6L16 14.6z" fill="#fff"/></svg>
```

- [ ] **Step 2: Write app.css**

`avalonshop/static/app.css` (the whole design system; keep it under 15 KB):

```css
@font-face{font-family:Manrope;font-style:normal;font-weight:400 700;font-display:swap;src:url(/static/fonts/manrope.woff2) format("woff2")}
@font-face{font-family:"DM Mono";font-style:normal;font-weight:400;font-display:swap;src:url(/static/fonts/dmmono-400.woff2) format("woff2")}
@font-face{font-family:"DM Mono";font-style:normal;font-weight:500;font-display:swap;src:url(/static/fonts/dmmono-500.woff2) format("woff2")}

:root{
  --ground:#fff;--tint:#eef4ef;--ink:#14201c;--muted:#6b7f72;--line:#e6ebe8;
  --accent:#1f6b3a;--accent-ink:#fff;--foot:#14201c;--foot-ink:#c8d3cc;
  --danger:#b3261e;--ok:#1f6b3a;--radius:10px;--pill:999px;
  --font:Manrope,system-ui,-apple-system,"Segoe UI",sans-serif;
  --mono:"DM Mono",ui-monospace,SFMono-Regular,Menlo,monospace;--wrap:1200px
}
*,*::before,*::after{box-sizing:border-box}
html{-webkit-text-size-adjust:100%}
body{margin:0;background:var(--ground);color:var(--ink);font:400 1rem/1.55 var(--font)}
body.no-scroll{overflow:hidden}
img{max-width:100%;height:auto;display:block}
a{color:inherit}
button,input,select,textarea{font:inherit;color:inherit}
:focus-visible{outline:2px solid var(--accent);outline-offset:2px}
h1,h2,h3{font-weight:600;letter-spacing:-.03em;line-height:1.1;margin:0 0 .5em}
h1{font-size:clamp(1.8rem,4vw,2.6rem);letter-spacing:-.04em}
h2{font-size:1.35rem}
h3{font-size:1.05rem}
p{margin:0 0 1em}
.mono{font-family:var(--mono)}
.eyebrow{font-family:var(--mono);font-size:.7rem;font-weight:500;letter-spacing:.12em;text-transform:uppercase;color:var(--accent);margin:0 0 .5rem}
.muted{color:var(--muted)}
.price{font-family:var(--mono);font-weight:500;color:var(--accent)}
.wrap{max-width:var(--wrap);margin:0 auto;padding:0 1rem}
@media(min-width:640px){.wrap{padding:0 2rem}}
main.wrap{min-height:60vh;padding-top:1.5rem;padding-bottom:3rem}

/* nav */
.nav{position:sticky;top:0;z-index:20;background:var(--ground);border-bottom:1px solid var(--line)}
.nav .in{max-width:var(--wrap);margin:0 auto;padding:.75rem 1rem;display:flex;align-items:center;justify-content:space-between;gap:1rem}
@media(min-width:640px){.nav .in{padding:.9rem 2rem}}
.brand{font-weight:700;letter-spacing:-.03em;font-size:1.25rem;text-decoration:none}
.nav nav{display:flex;align-items:center;gap:1.25rem}
.nav nav a{text-decoration:none;font-weight:500;font-size:.95rem}
.cart-btn{background:none;border:1px solid var(--line);border-radius:var(--pill);padding:.4rem .9rem;cursor:pointer;display:inline-flex;gap:.4rem;align-items:center;font-weight:500}
.badge{font-family:var(--mono);font-size:.75rem;background:var(--accent);color:var(--accent-ink);border-radius:var(--pill);padding:.05rem .45rem;min-width:1.4rem;text-align:center}
.badge:empty,.badge[data-zero]{display:none}
.flash{background:var(--tint);color:var(--ink);padding:.7rem 1rem;text-align:center;font-size:.95rem}
.flash.err{background:#fbeaea;color:var(--danger)}

/* hero */
.hero{background:var(--tint);border-radius:var(--radius);padding:clamp(2rem,6vw,4rem) clamp(1.25rem,4vw,3rem);margin-bottom:2.5rem}
.hero p{max-width:34rem;color:var(--muted);font-size:1.05rem}

/* buttons */
.btn{display:inline-flex;align-items:center;justify-content:center;gap:.5rem;padding:.7rem 1.4rem;border-radius:var(--pill);border:1px solid var(--accent);background:var(--accent);color:var(--accent-ink);font-weight:600;text-decoration:none;cursor:pointer;line-height:1.2}
.btn:hover{filter:brightness(1.08)}
.btn:disabled{opacity:.5;cursor:not-allowed}
.btn.ghost{background:none;color:var(--ink);border-color:var(--line)}
.btn.sm{padding:.4rem .9rem;font-size:.9rem}
.btn.danger{background:var(--danger);border-color:var(--danger)}

/* product grid */
.grid{display:grid;gap:1rem;grid-template-columns:repeat(2,1fr)}
@media(min-width:640px){.grid{grid-template-columns:repeat(3,1fr);gap:1.25rem}}
@media(min-width:1024px){.grid{grid-template-columns:repeat(4,1fr);gap:1.5rem}}
.card{border:1px solid var(--line);border-radius:var(--radius);overflow:hidden;text-decoration:none;display:flex;flex-direction:column;background:var(--ground);transition:transform .15s}
.card:hover{transform:translateY(-2px)}
.card .ph,.card img{aspect-ratio:1;object-fit:cover;width:100%;background:var(--tint)}
.card .body{padding:.75rem .9rem 1rem}
.card .name{font-weight:600;margin:0 0 .25rem}
.card .line{display:flex;justify-content:space-between;align-items:baseline;gap:.5rem;font-size:.95rem}
.card .hint{font-family:var(--mono);font-size:.72rem;color:var(--muted)}
.section-head{display:flex;justify-content:space-between;align-items:baseline;margin:0 0 1rem}
.chips{display:flex;flex-wrap:wrap;gap:.5rem;margin:0 0 1.5rem}
.chip{border:1px solid var(--line);border-radius:var(--pill);padding:.35rem .9rem;text-decoration:none;font-size:.9rem}
.chip.on{background:var(--ink);color:#fff;border-color:var(--ink)}
.search{display:flex;gap:.5rem;margin:0 0 1.5rem}
.search input{flex:1}

/* product page */
.crumb{font-size:.85rem;color:var(--muted);margin:0 0 1rem}
.crumb a{text-decoration:none}
.pp{display:grid;gap:2rem}
@media(min-width:900px){.pp{grid-template-columns:1.1fr 1fr;gap:3rem;align-items:start}.pp .buy{position:sticky;top:5rem}}
.gallery img.main{aspect-ratio:1;object-fit:cover;width:100%;border-radius:var(--radius);background:var(--tint)}
.thumbs{display:flex;gap:.5rem;margin-top:.6rem}
.thumbs img{width:64px;height:64px;object-fit:cover;border-radius:8px;border:2px solid transparent;cursor:pointer}
.thumbs img.on{border-color:var(--accent)}
.buy .price{font-size:1.5rem;margin:.5rem 0 1rem;display:block}
.buy .price small{font-size:.8rem;color:var(--muted);font-weight:400;margin-left:.5rem}
.pills{display:flex;flex-wrap:wrap;gap:.5rem;margin:.25rem 0 1.25rem}
.pill{position:relative}
.pill input{position:absolute;opacity:0;inset:0;margin:0;cursor:pointer}
.pill span{display:inline-block;padding:.45rem .95rem;border:1px solid #cfd9d2;border-radius:var(--pill);font-size:.95rem}
.pill input:checked+span{background:var(--ink);color:#fff;border-color:var(--ink)}
.pill input:disabled+span{color:#b5bfb8;text-decoration:line-through;cursor:not-allowed}
.pill input:focus-visible+span{outline:2px solid var(--accent);outline-offset:2px}
.qty{display:inline-flex;border:1px solid #cfd9d2;border-radius:var(--pill);overflow:hidden;vertical-align:middle}
.qty button{background:none;border:0;padding:.5rem .8rem;cursor:pointer}
.qty input{width:3rem;text-align:center;border:0;border-left:1px solid #cfd9d2;border-right:1px solid #cfd9d2;-moz-appearance:textfield}
.qty input::-webkit-outer-spin-button,.qty input::-webkit-inner-spin-button{-webkit-appearance:none;margin:0}
.buy-row{display:flex;gap:.75rem;align-items:center;flex-wrap:wrap;margin-bottom:1.25rem}
.desc{white-space:pre-line;color:#3d4a43}
.meta{display:grid;grid-template-columns:1fr 1fr;gap:.5rem;padding-top:1rem;margin-top:1rem;border-top:1px solid var(--line);font-family:var(--mono);font-size:.78rem;color:var(--muted)}
.related{margin-top:3rem;padding-top:1.5rem;border-top:1px solid var(--line)}

/* cart drawer + cart page */
.scrim{position:fixed;inset:0;background:rgba(20,32,28,.4);opacity:0;pointer-events:none;transition:opacity .2s;z-index:30}
.scrim.open{opacity:1;pointer-events:auto}
.drawer{position:fixed;top:0;right:0;bottom:0;width:min(380px,100%);background:var(--ground);z-index:40;transform:translateX(100%);transition:transform .25s;display:flex;flex-direction:column;box-shadow:-8px 0 30px rgba(0,0,0,.08)}
.drawer.open{transform:none}
.drawer .head{display:flex;justify-content:space-between;align-items:center;padding:1rem 1.25rem;border-bottom:1px solid var(--line)}
.drawer .lines{flex:1;overflow-y:auto;padding:.5rem 1.25rem}
.drawer .foot{padding:1rem 1.25rem;border-top:1px solid var(--line)}
.cl{display:grid;grid-template-columns:64px 1fr auto;gap:.75rem;align-items:center;padding:.75rem 0;border-bottom:1px solid var(--line)}
.cl img,.cl .ph{width:64px;height:64px;border-radius:8px;object-fit:cover;background:var(--tint)}
.cl .n{font-weight:600;font-size:.95rem;margin:0}
.cl .v{font-family:var(--mono);font-size:.75rem;color:var(--muted)}
.cl .qty{margin-top:.35rem;transform:scale(.9);transform-origin:left}
.cl .rm{background:none;border:0;color:var(--muted);cursor:pointer;font-size:.8rem;padding:0;text-decoration:underline}
.totals{display:grid;grid-template-columns:1fr auto;gap:.35rem .75rem;font-size:.95rem}
.totals .big{font-size:1.15rem;font-weight:600}
.x{background:none;border:0;font-size:1.4rem;cursor:pointer;line-height:1}
.empty{text-align:center;color:var(--muted);padding:2rem 0}
.cart-page{display:grid;gap:2rem}
@media(min-width:900px){.cart-page{grid-template-columns:1.4fr 1fr;align-items:start}}
.panel{border:1px solid var(--line);border-radius:var(--radius);padding:1.25rem}

/* forms */
.form{max-width:32rem}
.form.wide{max-width:none}
label{display:block;font-weight:500;font-size:.9rem;margin:0 0 .3rem}
input[type=text],input[type=email],input[type=password],input[type=tel],input[type=number],input[type=search],input[type=url],select,textarea{width:100%;padding:.6rem .8rem;border:1px solid #cfd9d2;border-radius:8px;background:#fff}
textarea{min-height:6rem;resize:vertical}
.field{margin-bottom:1rem}
.field .err{color:var(--danger);font-size:.85rem;margin:.3rem 0 0}
.row{display:grid;gap:1rem}
@media(min-width:640px){.row{grid-template-columns:1fr 1fr}}
.check{display:flex;gap:.5rem;align-items:center;font-weight:400}
.check input{width:auto}
.radio-list label{display:flex;justify-content:space-between;gap:1rem;padding:.7rem .9rem;border:1px solid var(--line);border-radius:8px;margin-bottom:.5rem;cursor:pointer;font-weight:400}
.radio-list input{margin-right:.6rem}
.help{font-size:.85rem;color:var(--muted)}
.errbox{background:#fbeaea;color:var(--danger);padding:.7rem 1rem;border-radius:8px;margin-bottom:1rem}

/* order / account */
.status{display:inline-block;font-family:var(--mono);font-size:.72rem;letter-spacing:.06em;text-transform:uppercase;padding:.2rem .6rem;border-radius:var(--pill);background:var(--tint);color:var(--accent)}
.status.new{background:#fff4e0;color:#8a5a00}
.status.shipped{background:#e6f0ff;color:#1d4f9c}
.status.delivered{background:var(--tint);color:var(--accent)}
.status.cancelled{background:#f1f1f1;color:#666}
table{width:100%;border-collapse:collapse;font-size:.95rem}
th,td{text-align:left;padding:.6rem .5rem;border-bottom:1px solid var(--line);vertical-align:top}
th{font-family:var(--mono);font-size:.72rem;letter-spacing:.08em;text-transform:uppercase;color:var(--muted);font-weight:500}
td.num,th.num{text-align:right;font-family:var(--mono)}
.table-wrap{overflow-x:auto}

/* footer */
.foot{background:var(--foot);color:var(--foot-ink);margin-top:2rem}
.foot .in{max-width:var(--wrap);margin:0 auto;padding:1.5rem 1rem;display:flex;flex-wrap:wrap;gap:.75rem 1.5rem;align-items:center;font-family:var(--mono);font-size:.72rem;letter-spacing:.08em;text-transform:uppercase}
@media(min-width:640px){.foot .in{padding:1.5rem 2rem}}
.foot a{text-decoration:none}

/* admin */
.admin{display:grid;min-height:100vh}
@media(min-width:900px){.admin{grid-template-columns:220px 1fr}}
.side{background:var(--foot);color:var(--foot-ink);padding:1rem}
.side .brand{color:#fff;display:block;margin-bottom:1rem}
.side nav{display:flex;flex-wrap:wrap;gap:.25rem}
@media(min-width:900px){.side nav{flex-direction:column}}
.side nav a{color:var(--foot-ink);text-decoration:none;padding:.45rem .7rem;border-radius:8px;font-size:.95rem}
.side nav a.on,.side nav a:hover{background:rgba(255,255,255,.08);color:#fff}
.side form{margin-top:1rem}
.side .btn{width:100%}
.content{padding:1.5rem 1rem;max-width:1100px}
@media(min-width:900px){.content{padding:2rem}}
.stats{display:grid;grid-template-columns:repeat(auto-fit,minmax(160px,1fr));gap:1rem;margin-bottom:2rem}
.stat{border:1px solid var(--line);border-radius:var(--radius);padding:1rem}
.stat .n{font-size:2rem;font-weight:600;letter-spacing:-.04em}
.toolbar{display:flex;flex-wrap:wrap;gap:.75rem;align-items:center;justify-content:space-between;margin-bottom:1rem}
.inline{display:inline}
.variants td input{min-width:5rem}
.thumb{width:48px;height:48px;object-fit:cover;border-radius:6px;background:var(--tint)}
.imgs{display:grid;grid-template-columns:repeat(auto-fill,minmax(160px,1fr));gap:1rem}
.imgs figure{margin:0;border:1px solid var(--line);border-radius:var(--radius);padding:.5rem}
.imgs img{aspect-ratio:1;object-fit:cover;border-radius:6px;width:100%}
.imgs .acts{display:flex;gap:.25rem;margin-top:.4rem}
.imgs .acts button{flex:1}

@media(prefers-reduced-motion:reduce){.drawer,.scrim,.card{transition:none}.card:hover{transform:none}}
```

- [ ] **Step 3: Write app.js**

`avalonshop/static/app.js` (no inline handlers anywhere, CSP forbids them):

```js
(function () {
  var drawer = document.getElementById('drawer');
  var scrim = document.querySelector('.scrim');

  function openDrawer() {
    if (!drawer) return;
    drawer.classList.add('open');
    drawer.setAttribute('aria-hidden', 'false');
    scrim.classList.add('open');
    document.body.classList.add('no-scroll');
    var f = drawer.querySelector('button, a, input');
    if (f) f.focus();
  }
  function closeDrawer() {
    if (!drawer) return;
    drawer.classList.remove('open');
    drawer.setAttribute('aria-hidden', 'true');
    scrim.classList.remove('open');
    document.body.classList.remove('no-scroll');
  }

  document.addEventListener('click', function (e) {
    if (e.target.closest('[data-close-cart]')) { closeDrawer(); return; }

    var thumb = e.target.closest('[data-thumb]');
    if (thumb) {
      var main = document.getElementById('gallery-main');
      main.src = thumb.dataset.src;
      main.srcset = thumb.dataset.srcset;
      main.alt = thumb.dataset.alt || '';
      document.querySelectorAll('[data-thumb]').forEach(function (t) { t.classList.toggle('on', t === thumb); });
      return;
    }

    var step = e.target.closest('[data-step]');
    if (step) {
      var inp = step.parentElement.querySelector('input[type=number]');
      var v = (parseInt(inp.value, 10) || 1) + parseInt(step.dataset.step, 10);
      var lo = parseInt(inp.min, 10) || 0, hi = parseInt(inp.max, 10) || 99;
      inp.value = Math.min(Math.max(v, lo), hi);
      inp.dispatchEvent(new Event('change', { bubbles: true }));
    }
  });

  document.addEventListener('change', function (e) {
    var pill = e.target.closest('input[name=variant_id]');
    if (!pill) return;
    var price = document.getElementById('price');
    var stock = document.getElementById('stock');
    var qty = document.querySelector('input[name=qty]');
    if (price) price.textContent = pill.dataset.priceText;
    if (stock) stock.textContent = pill.dataset.stockText;
    if (qty) { qty.max = pill.dataset.stock; if (+qty.value > +pill.dataset.stock) qty.value = pill.dataset.stock; }
  });

  document.addEventListener('keydown', function (e) { if (e.key === 'Escape') closeDrawer(); });

  document.body.addEventListener('htmx:afterSwap', function (e) {
    if (e.detail.target && e.detail.target.id === 'drawer') openDrawer();
  });
})();
```

- [ ] **Step 4: Write the layouts and partials**

`avalonshop/templates/layout.html`:

```html
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
{{if .Description}}<meta name="description" content="{{.Description}}">{{end}}
{{if .NoIndex}}<meta name="robots" content="noindex">{{else}}<link rel="canonical" href="{{.Canonical}}">{{end}}
<meta property="og:site_name" content="Avalon">
<meta property="og:title" content="{{.Title}}">
{{if .Description}}<meta property="og:description" content="{{.Description}}">{{end}}
<meta property="og:url" content="{{.Canonical}}">
<meta property="og:type" content="{{if .JSONLD}}product{{else}}website{{end}}">
{{if .OGImage}}<meta property="og:image" content="{{.OGImage}}">{{end}}
<meta name="theme-color" content="#1f6b3a">
<link rel="icon" href="/static/favicon.svg" type="image/svg+xml">
<link rel="preload" href="/static/fonts/manrope.woff2" as="font" type="font/woff2" crossorigin>
<link rel="stylesheet" href="/static/app.css?v={{.V}}">
<meta name="htmx-config" content='{"includeIndicatorStyles":false,"allowEval":false}'>
<script src="/static/htmx.min.js?v={{.V}}" defer></script>
<script src="/static/app.js?v={{.V}}" defer></script>
{{if .JSONLD}}<script type="application/ld+json">{{.JSONLD}}</script>{{end}}
</head>
<body>
<header class="nav"><div class="in">
  <a class="brand" href="/">Avalon</a>
  <nav>
    <a href="/products">Shop</a>
    {{if .User}}<a href="/account">Account</a>{{else}}<a href="/login">Login</a>{{end}}
    <a class="cart-btn" href="/cart" hx-get="/cart/drawer" hx-target="#drawer" hx-swap="innerHTML">Cart {{template "cart_badge.html" .CartCount}}</a>
  </nav>
</div></header>
{{if .Flash}}<div class="flash" role="status">{{.Flash}}</div>{{end}}
<main class="wrap">{{template "content" .}}</main>
<footer class="foot"><div class="in">
  <span>Avalon Corporation</span><span>·</span><span>Rajshahi, Bangladesh</span><span>·</span><a href="/products">Shop</a>
</div></footer>
<div class="scrim" data-close-cart></div>
<aside id="drawer" class="drawer" aria-hidden="true" aria-label="Shopping cart"></aside>
</body>
</html>
```

`avalonshop/templates/partials/cart_badge.html`:

```html
<span id="cart-count" class="badge" hx-swap-oob="true"{{if eq . 0}} data-zero{{end}}>{{.}}</span>
```

`avalonshop/templates/store/404.html`:

```html
{{define "content"}}
<div class="empty"><h1>Page not found</h1><p>The page you asked for doesn't exist.</p><a class="btn" href="/products">Browse products</a></div>
{{end}}
```

`avalonshop/templates/admin/layout.html`:

```html
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<meta name="robots" content="noindex">
<link rel="icon" href="/static/favicon.svg" type="image/svg+xml">
<link rel="stylesheet" href="/static/app.css?v={{.V}}">
<meta name="htmx-config" content='{"includeIndicatorStyles":false,"allowEval":false}'>
<script src="/static/htmx.min.js?v={{.V}}" defer></script>
<script src="/static/app.js?v={{.V}}" defer></script>
</head>
<body class="admin">
<aside class="side">
  <a class="brand" href="/admin">Avalon Admin</a>
  <nav>
    <a href="/admin"{{if eq .Path "/admin"}} class="on"{{end}}>Dashboard</a>
    <a href="/admin/orders"{{if hasPrefix .Path "/admin/orders"}} class="on"{{end}}>Orders</a>
    <a href="/admin/products"{{if hasPrefix .Path "/admin/products"}} class="on"{{end}}>Products</a>
    <a href="/admin/categories"{{if hasPrefix .Path "/admin/categories"}} class="on"{{end}}>Categories</a>
    <a href="/admin/zones"{{if hasPrefix .Path "/admin/zones"}} class="on"{{end}}>Delivery zones</a>
    <a href="/">View shop</a>
  </nav>
  <form method="post" action="/logout"><button class="btn ghost sm" type="submit">Log out</button></form>
</aside>
<div class="content">
{{if .Flash}}<div class="flash" role="status">{{.Flash}}</div>{{end}}
{{template "content" .}}
</div>
</body>
</html>
```

- [ ] **Step 5: Write the failing tests**

`avalonshop/internal/app/limiter_test.go`:

```go
package app

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	l := newLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !l.Allow("a") {
			t.Fatalf("call %d should be allowed", i)
		}
	}
	if l.Allow("a") {
		t.Fatal("4th call should be blocked")
	}
	if !l.Allow("b") {
		t.Fatal("other key unaffected")
	}
	l.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	if !l.Allow("a") {
		t.Fatal("window should have expired")
	}
	f := newLimiter(2, time.Minute)
	if f.Blocked("x") {
		t.Fatal("fresh key must not be blocked")
	}
	f.Hit("x")
	f.Hit("x")
	if !f.Blocked("x") {
		t.Fatal("two hits should block at limit 2")
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	if clientIP(r) != "10.0.0.1" {
		t.Fatal(clientIP(r))
	}
	r.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.1")
	if clientIP(r) != "203.0.113.5" {
		t.Fatal(clientIP(r))
	}
}
```

`avalonshop/internal/app/app_test.go`:

```go
package app

import (
	"bytes"
	"compress/gzip"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"avalonshop/internal/config"
	"avalonshop/internal/mail"
	"avalonshop/internal/store"
	"avalonshop/internal/store/storetest"

	"github.com/jackc/pgx/v5/pgxpool"
)

// newApp builds an App against the real templates and static files. pool may be nil
// for tests that never touch the database.
func newApp(t *testing.T, pool *pgxpool.Pool) *App {
	t.Helper()
	cfg := config.Config{
		Addr: ":0", BaseURL: "http://localhost:8080", SessionSecret: bytes.Repeat([]byte("k"), 32),
		UploadDir: t.TempDir(), MailFrom: "shop@test.local", OrderNotifyEmail: "owner@test.local",
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	m, err := mail.New("", "587", "", "", cfg.MailFrom, os.DirFS("../.."), log)
	if err != nil {
		t.Fatal(err)
	}
	var st *store.Store
	if pool != nil {
		st = store.New(pool)
	}
	a, err := New(cfg, st, m, os.DirFS("../.."), os.DirFS("../.."), log)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func newDBApp(t *testing.T) (*App, *store.Store) {
	t.Helper()
	pool := storetest.Pool(t)
	a := newApp(t, pool)
	return a, a.st
}

func do(t *testing.T, a *App, method, path string, body io.Reader, hdr ...string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, body)
	if body != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}

func TestSecurityHeadersAndGzip404(t *testing.T) {
	a := newApp(t, nil)
	w := do(t, a, "GET", "/no-such-page", nil, "Accept-Encoding", "gzip")
	if w.Code != 404 {
		t.Fatalf("status %d", w.Code)
	}
	if w.Header().Get("Content-Encoding") != "gzip" || w.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatalf("html not gzipped: %v", w.Header())
	}
	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self'") || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("security headers missing: %v", w.Header())
	}
	zr, _ := gzip.NewReader(w.Body)
	html, _ := io.ReadAll(zr)
	if !strings.Contains(string(html), "Page not found") || !strings.Contains(string(html), `<meta name="robots" content="noindex">`) {
		t.Fatalf("404 page wrong: %s", html)
	}
	if strings.Count(string(html), "<h1") != 1 {
		t.Fatal("exactly one h1 expected")
	}
}

func TestStaticAndMediaCaching(t *testing.T) {
	a := newApp(t, nil)
	w := do(t, a, "GET", "/static/app.css?v=abc", nil, "Accept-Encoding", "gzip")
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("css: %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	if w.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("css headers: %v", w.Header())
	}
	os.WriteFile(a.cfg.UploadDir+"/x-400.webp", []byte("RIFF....WEBP"), 0o644)
	w = do(t, a, "GET", "/media/x-400.webp", nil, "Accept-Encoding", "gzip")
	if w.Code != 200 || w.Header().Get("Content-Encoding") != "" || w.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("media: %d %v", w.Code, w.Header())
	}
	if w := do(t, a, "GET", "/media/", nil); w.Code != 404 {
		t.Fatalf("directory listing should 404, got %d", w.Code)
	}
	if w := do(t, a, "GET", "/healthz", nil); w.Code != 200 || w.Body.String() != "ok" {
		t.Fatalf("healthz: %d %q", w.Code, w.Body.String())
	}
}

func TestPlainResponseWithoutGzip(t *testing.T) {
	a := newApp(t, nil)
	w := do(t, a, "GET", "/no-such-page", nil) // no Accept-Encoding
	if w.Header().Get("Content-Encoding") != "" || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("plain response wrong: %v", w.Header())
	}
}
```

- [ ] **Step 6: Run to verify it fails**

Run: `go test ./internal/app/`
Expected: FAIL, `undefined: New` / `undefined: newLimiter`

- [ ] **Step 7: Write limiter.go**

```go
package app

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// limiter is a fixed-window-per-key counter kept in memory.
// ponytail: resets on restart and is per-process; fine for one container.
type limiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	limit  int
	window time.Duration
	now    func() time.Time
}

func newLimiter(limit int, window time.Duration) *limiter {
	return &limiter{hits: map[string][]time.Time{}, limit: limit, window: window, now: time.Now}
}

// prune drops expired hits for key and returns the live ones. Caller holds mu.
func (l *limiter) prune(key string, now time.Time) []time.Time {
	cutoff := now.Add(-l.window)
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	l.hits[key] = kept
	if len(l.hits) > 10000 { // ponytail: crude sweep; a real LRU if this ever matters
		for k, v := range l.hits {
			if len(v) == 0 || !v[len(v)-1].After(cutoff) {
				delete(l.hits, k)
			}
		}
	}
	return kept
}

// Blocked reports whether key has reached the limit without recording anything.
func (l *limiter) Blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.prune(key, l.now())) >= l.limit
}

// Hit records one event for key (used to count only failed logins).
func (l *limiter) Hit(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.hits[key] = append(l.prune(key, now), now)
}

// Allow records a hit if under the limit and reports whether it was allowed.
func (l *limiter) Allow(key string) bool {
	if l.Blocked(key) {
		return false
	}
	l.Hit(key)
	return true
}

// clientIP trusts the first X-Forwarded-For entry because the app always sits
// behind Coolify's Traefik. ponytail: spoofable if ever exposed directly.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
```

- [ ] **Step 8: Write middleware.go**

```go
package app

import (
	"compress/gzip"
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"avalonshop/internal/store"
)

func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; frame-ancestors 'none'; form-action 'self'; base-uri 'self'")
		next.ServeHTTP(w, r)
	})
}

var gzPool = sync.Pool{New: func() any { return gzip.NewWriter(nil) }}

type gzipWriter struct {
	http.ResponseWriter
	gz          *gzip.Writer
	wroteHeader bool
	compress    bool
}

func compressible(ct string) bool {
	for _, p := range []string{"text/html", "text/css", "text/plain", "application/javascript", "text/javascript", "application/json", "application/xml", "text/xml", "image/svg+xml"} {
		if strings.HasPrefix(ct, p) {
			return true
		}
	}
	return false
}

func (g *gzipWriter) WriteHeader(code int) {
	if g.wroteHeader {
		return
	}
	g.wroteHeader = true
	h := g.Header()
	if code != http.StatusNoContent && code != http.StatusNotModified && compressible(h.Get("Content-Type")) {
		g.compress = true
		h.Set("Content-Encoding", "gzip")
		h.Add("Vary", "Accept-Encoding")
		h.Del("Content-Length")
		g.gz.Reset(g.ResponseWriter)
	}
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipWriter) Write(b []byte) (int, error) {
	if !g.wroteHeader {
		if g.Header().Get("Content-Type") == "" {
			g.Header().Set("Content-Type", http.DetectContentType(b))
		}
		g.WriteHeader(http.StatusOK)
	}
	if g.compress {
		return g.gz.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

// gzipMiddleware compresses text responses. Images are already compressed and pass through.
func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		gz := gzPool.Get().(*gzip.Writer)
		defer gzPool.Put(gz)
		gw := &gzipWriter{ResponseWriter: w, gz: gz}
		defer func() {
			if gw.compress {
				gw.gz.Close()
			}
		}()
		next.ServeHTTP(gw, r)
	})
}

// limitBody caps request bodies; the image upload route gets its own larger cap.
func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && !(strings.HasPrefix(r.URL.Path, "/admin/products/") && strings.HasSuffix(r.URL.Path, "/images")) {
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		}
		next.ServeHTTP(w, r)
	})
}

type ctxKey struct{}

// withUser loads the session user once per request. Static and media paths skip it.
func (a *App) withUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/static/") && !strings.HasPrefix(r.URL.Path, "/media/") {
			if c, err := r.Cookie("sess"); err == nil {
				if id, ok := a.tok.DecodeSession(c.Value, time.Now()); ok {
					if u, err := a.st.GetUser(r.Context(), id); err == nil {
						r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, &u))
					}
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) currentUser(r *http.Request) *store.User {
	u, _ := r.Context().Value(ctxKey{}).(*store.User)
	return u
}

func immutable(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		next.ServeHTTP(w, r)
	})
}
```

- [ ] **Step 9: Write cookies.go**

```go
package app

import (
	"net/http"
	"net/url"
	"time"

	"avalonshop/internal/token"
)

const sessionTTL = 30 * 24 * time.Hour

func (a *App) setCookie(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: a.cfg.Secure(), SameSite: http.SameSiteLaxMode,
	})
}

func (a *App) cartLines(r *http.Request) []token.CartLine {
	c, err := r.Cookie("cart")
	if err != nil {
		return nil
	}
	return a.tok.DecodeCart(c.Value)
}

func (a *App) saveCart(w http.ResponseWriter, lines []token.CartLine) {
	if len(lines) == 0 {
		a.setCookie(w, "cart", "", -1)
		return
	}
	a.setCookie(w, "cart", a.tok.EncodeCart(lines), int(sessionTTL.Seconds()))
}

func cartCount(lines []token.CartLine) int {
	n := 0
	for _, l := range lines {
		n += l.Qty
	}
	return n
}

func (a *App) login(w http.ResponseWriter, userID int64) {
	a.setCookie(w, "sess", a.tok.EncodeSession(userID, time.Now().Add(sessionTTL)), int(sessionTTL.Seconds()))
}

func (a *App) logout(w http.ResponseWriter) { a.setCookie(w, "sess", "", -1) }

// Flash is a one-shot message. It is URL-escaped because cookie values cannot hold spaces.
func (a *App) setFlash(w http.ResponseWriter, msg string) {
	a.setCookie(w, "flash", url.QueryEscape(msg), 60)
}

func (a *App) popFlash(w http.ResponseWriter, r *http.Request) string {
	c, err := r.Cookie("flash")
	if err != nil {
		return ""
	}
	a.setCookie(w, "flash", "", -1)
	msg, _ := url.QueryUnescape(c.Value)
	return msg
}
```

- [ ] **Step 10: Write render.go**

```go
package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"avalonshop/internal/img"
	"avalonshop/internal/money"
	"avalonshop/internal/store"
)

// page is what every layout receives. Handlers fill the SEO fields and Data;
// render fills the rest.
type page struct {
	Title       string
	Description string
	Canonical   string
	OGImage     string
	JSONLD      template.JS
	NoIndex     bool
	User        *store.User
	CartCount   int
	Flash       string
	V           string
	BaseURL     string
	Path        string
	Data        any
}

func (a *App) funcs() template.FuncMap {
	return template.FuncMap{
		"taka":   money.Format,
		"imgURL": func(stem string, w int) string { return "/media/" + img.Filename(stem, w) },
		"srcset": func(stem string, width int) string {
			var parts []string
			for _, w := range img.WidthsFor(width) {
				parts = append(parts, fmt.Sprintf("/media/%s %dw", img.Filename(stem, w), w))
			}
			return strings.Join(parts, ", ")
		},
		"dhaka":     func(t time.Time) string { return t.In(a.dhaka).Format("2 Jan 2006, 3:04 PM") },
		"deref":     func(p *string) string { if p == nil { return "" }; return *p },
		"derefInt":  func(p *int) int { if p == nil { return 0 }; return *p },
		"add":       func(x, y int) int { return x + y },
		"mul":       func(x, y int) int { return x * y },
		"hasPrefix": strings.HasPrefix,
		"jsonld": func(v any) template.JS {
			b, _ := json.Marshal(v)
			return template.JS(b)
		},
	}
}

func parseTemplates(fsys fs.FS, funcs template.FuncMap) (map[string]*template.Template, error) {
	out := map[string]*template.Template{}
	for _, set := range []struct{ dir, layout string }{{"store", "templates/layout.html"}, {"admin", "templates/admin/layout.html"}} {
		pages, err := fs.Glob(fsys, "templates/"+set.dir+"/*.html")
		if err != nil {
			return nil, err
		}
		for _, p := range pages {
			if p == set.layout {
				continue
			}
			t, err := template.New("layout.html").Funcs(funcs).ParseFS(fsys, set.layout, "templates/partials/*.html", p)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", p, err)
			}
			out[set.dir+"/"+path.Base(p)] = t
		}
	}
	partials, err := fs.Glob(fsys, "templates/partials/*.html")
	if err != nil {
		return nil, err
	}
	for _, p := range partials {
		t, err := template.New(path.Base(p)).Funcs(funcs).ParseFS(fsys, "templates/partials/*.html")
		if err != nil {
			return nil, err
		}
		out["partials/"+path.Base(p)] = t
	}
	return out, nil
}

func (a *App) render(w http.ResponseWriter, r *http.Request, name string, p page) {
	a.renderStatus(w, r, http.StatusOK, name, p)
}

// renderStatus executes into a buffer first so a template error yields a clean
// 500 instead of half a page.
func (a *App) renderStatus(w http.ResponseWriter, r *http.Request, status int, name string, p page) {
	t, ok := a.tmpl[name]
	if !ok {
		a.serverError(w, r, fmt.Errorf("template %q not found", name))
		return
	}
	p.User = a.currentUser(r)
	p.CartCount = cartCount(a.cartLines(r))
	p.Flash = a.popFlash(w, r)
	p.V = a.assetV
	p.BaseURL = a.cfg.BaseURL
	p.Path = r.URL.Path
	if p.Canonical == "" {
		p.Canonical = a.cfg.BaseURL + r.URL.Path
	}
	switch {
	case p.Title == "":
		p.Title = "Avalon"
	case !strings.Contains(p.Title, "Avalon"):
		p.Title += " · Avalon"
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout.html", p); err != nil {
		a.serverError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-cache")
	w.WriteHeader(status)
	buf.WriteTo(w)
}

func (a *App) renderPartial(w http.ResponseWriter, name string, data any) {
	t, ok := a.tmpl["partials/"+name]
	if !ok {
		http.Error(w, "partial not found", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		a.log.Error("partial", "name", name, "err", err)
		http.Error(w, "render error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-cache")
	buf.WriteTo(w)
}

func (a *App) notFound(w http.ResponseWriter, r *http.Request) {
	a.renderStatus(w, r, http.StatusNotFound, "store/404.html", page{Title: "Page not found", NoIndex: true})
}

func (a *App) serverError(w http.ResponseWriter, r *http.Request, err error) {
	a.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	http.Error(w, "Something went wrong. Please try again.", http.StatusInternalServerError)
}

func isHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

// redirect sends 303 for browsers and HX-Redirect for HTMX so both land on url.
func (a *App) redirect(w http.ResponseWriter, r *http.Request, url string) {
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", url)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, url, http.StatusSeeOther)
}
```

- [ ] **Step 11: Write app.go**

```go
// Package app is the HTTP layer: storefront and admin handlers, rendering, middleware.
package app

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"avalonshop/internal/config"
	"avalonshop/internal/mail"
	"avalonshop/internal/store"
	"avalonshop/internal/token"
)

type App struct {
	cfg           config.Config
	st            *store.Store
	mail          *mail.Mailer
	log           *slog.Logger
	tok           token.Signer
	tmpl          map[string]*template.Template
	static        fs.FS
	assetV        string
	dhaka         *time.Location
	loginLimit    *limiter
	forgotLimit   *limiter
	checkoutLimit *limiter
	mux           *http.ServeMux
}

func New(cfg config.Config, st *store.Store, m *mail.Mailer, templates, static fs.FS, log *slog.Logger) (*App, error) {
	sub, err := fs.Sub(static, "static")
	if err != nil {
		return nil, err
	}
	a := &App{
		cfg: cfg, st: st, mail: m, log: log, tok: token.New(cfg.SessionSecret), static: sub,
		loginLimit:    newLimiter(5, 15*time.Minute),
		forgotLimit:   newLimiter(5, 15*time.Minute),
		checkoutLimit: newLimiter(10, time.Hour),
		mux:           http.NewServeMux(),
	}
	if a.dhaka, err = time.LoadLocation("Asia/Dhaka"); err != nil {
		return nil, err
	}
	a.assetV = assetVersion(sub)
	if a.tmpl, err = parseTemplates(templates, a.funcs()); err != nil {
		return nil, fmt.Errorf("templates: %w", err)
	}
	a.routes()
	return a, nil
}

// assetVersion is a short hash of the CSS and JS so their URLs change on deploy.
func assetVersion(static fs.FS) string {
	h := sha256.New()
	for _, name := range []string{"app.css", "app.js"} {
		b, _ := fs.ReadFile(static, name)
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))[:8]
}

func (a *App) routes() {
	m := a.mux
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("ok"))
	})
	m.Handle("GET /static/", immutable(http.StripPrefix("/static/", http.FileServerFS(a.static))))
	m.Handle("GET /media/", immutable(http.StripPrefix("/media/", http.FileServer(http.Dir(a.cfg.UploadDir)))))
	m.HandleFunc("/", a.notFound)
	// Later tasks append their routes below this line.
}

func (a *App) Handler() http.Handler {
	csrf := http.NewCrossOriginProtection()
	return secureHeaders(gzipMiddleware(limitBody(a.withUser(csrf.Handler(a.mux)))))
}
```

- [ ] **Step 12: Run to verify it passes**

Run: `go vet ./... && go test ./internal/app/`
Expected: PASS

- [ ] **Step 13: Wire the app into main.go**

Replace the whole `main.go` with:

```go
package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	"avalonshop/internal/app"
	"avalonshop/internal/config"
	"avalonshop/internal/mail"
	"avalonshop/internal/store"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

//go:embed templates
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

func main() {
	healthcheck := flag.Bool("healthcheck", false, "probe the running server and exit 0 if healthy")
	flag.Parse()
	if *healthcheck {
		os.Exit(probe())
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// probe is used as the Docker HEALTHCHECK; the distroless image has no curl.
func probe() int {
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://127.0.0.1:8080/healthz")
	if err != nil || resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func run(log *slog.Logger) error {
	if err := config.LoadDotEnv(".env"); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := store.Migrate(ctx, pool, migrationsFS); err != nil {
		return err
	}
	st := store.New(pool)
	if err := st.SeedAdmin(ctx, cfg.AdminEmail, cfg.AdminPassword); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.UploadDir, 0o755); err != nil {
		return err
	}
	mailer, err := mail.New(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUser, cfg.SMTPPass, cfg.MailFrom, templatesFS, log)
	if err != nil {
		return err
	}
	a, err := app.New(cfg, st, mailer, templatesFS, staticFS, log)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           a.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(sctx)
		mailer.Wait()
	}()
	log.Info("listening", "addr", cfg.Addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
```

- [ ] **Step 14: Smoke run and commit**

Run: `go build ./... && go run . &` with a `.env` from `.env.example` and local Postgres up, then `curl -sI http://localhost:8080/static/app.css | grep -i cache-control` → expect `immutable`; `curl -s -o /dev/null -w '%{http_code}\n' http://localhost:8080/nope` → `404`. Stop with `kill %1`.

```bash
git add avalonshop
git commit -m "feat(shop): app core with layouts, design system, middleware, static serving"
```

---

### Task 11: Storefront pages: home, listing, product page with SEO

**Files:**
- Create: `avalonshop/internal/app/store_pages.go`, `avalonshop/internal/app/store_pages_test.go`, `avalonshop/templates/store/home.html`, `avalonshop/templates/store/products.html`, `avalonshop/templates/store/product.html`, `avalonshop/templates/partials/product_card.html`
- Modify: `avalonshop/internal/app/app.go` (routes), `avalonshop/internal/app/render.go` (add `cardData` func)

**Interfaces:**
- Consumes: `store.ListProductCards`, `store.GetProductBySlug`, `store.ListCategories`, `store.GetCategoryBySlug`, `img.Filename`, render helpers from Task 10.
- Produces: routes `GET /{$}`, `GET /products`, `GET /products/{slug}`; helpers `truncate(s string, n int) string`, `selectedVariant([]store.Variant) store.Variant`, `(*App) productJSONLD(store.ProductFull) template.JS`; template func `cardData card index` → `{C store.ProductCard; Eager bool}`.
- **Speed and SEO rules baked in here:** first-row cards load eagerly, the rest lazy; product hero image `fetchpriority="high"`; every image has `width`, `height`, `srcset`, `sizes`; JSON-LD Product with per-variant offers; search results are `noindex`; category pages get a canonical with the category query.

- [ ] **Step 1: Write the failing tests**

`avalonshop/internal/app/store_pages_test.go`:

```go
package app

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"avalonshop/internal/store"
)

// seedCatalog creates one category, one featured product (2 variants, one sold out, one image)
// and one inactive product. Returns the featured product's slug.
func seedCatalog(t *testing.T, st *store.Store) string {
	t.Helper()
	ctx := context.Background()
	cat, err := st.CreateCategory(ctx, store.Category{Slug: "honey", Name: "Honey"})
	if err != nil {
		t.Fatal(err)
	}
	sku := "HNY-500"
	id, err := st.CreateProduct(ctx, store.Product{Slug: "wild-forest-honey", Name: "Wild Forest Honey", Description: "Raw honey <b>from</b> the Sundarbans.\nDark and smoky.", CategoryID: &cat, Active: true, Featured: true},
		[]store.Variant{{Name: "500g", SKU: &sku, Price: 650, Stock: 5}, {Name: "1kg", Price: 1200, Stock: 0}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddImage(ctx, store.Image{ProductID: id, File: "abcdef0123456789", Alt: "Jar of honey", Width: 1600, Height: 1200}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateProduct(ctx, store.Product{Slug: "hidden", Name: "Hidden", Active: false}, []store.Variant{{Name: "x", Price: 1, Stock: 1}}); err != nil {
		t.Fatal(err)
	}
	return "wild-forest-honey"
}

var jsonldRe = regexp.MustCompile(`(?s)<script type="application/ld\+json">(.*?)</script>`)

func TestHomeAndListing(t *testing.T) {
	a, st := newDBApp(t)
	seedCatalog(t, st)
	w := do(t, a, "GET", "/", nil)
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, "Wild Forest Honey") || strings.Count(body, "<h1") != 1 {
		t.Fatalf("home: %d\n%s", w.Code, body)
	}
	if !strings.Contains(body, `<link rel="canonical" href="http://localhost:8080/">`) || !strings.Contains(body, `<meta name="description"`) {
		t.Fatal("home missing canonical or description")
	}
	if strings.Contains(body, `loading="lazy"`) {
		t.Fatal("first-row cards must load eagerly")
	}
	w = do(t, a, "GET", "/products?category=honey", nil)
	body = w.Body.String()
	if w.Code != 200 || !strings.Contains(body, `class="chip on" href="/products?category=honey"`) || !strings.Contains(body, `href="http://localhost:8080/products?category=honey"`) {
		t.Fatalf("category listing: %d\n%s", w.Code, body)
	}
	if strings.Contains(body, "Hidden") {
		t.Fatal("inactive product listed")
	}
	if w := do(t, a, "GET", "/products?category=nope", nil); w.Code != 404 {
		t.Fatalf("unknown category should 404, got %d", w.Code)
	}
	w = do(t, a, "GET", "/products?q=forest", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `content="noindex"`) || !strings.Contains(w.Body.String(), "Wild Forest Honey") {
		t.Fatalf("search: %d", w.Code)
	}
}

func TestProductPageSEOAndSpeed(t *testing.T) {
	a, st := newDBApp(t)
	slug := seedCatalog(t, st)
	w := do(t, a, "GET", "/products/"+slug, nil)
	body := w.Body.String()
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	for _, want := range []string{
		`<title>Wild Forest Honey · Avalon</title>`,
		`<link rel="canonical" href="http://localhost:8080/products/wild-forest-honey">`,
		`<meta property="og:image" content="http://localhost:8080/media/abcdef0123456789-1600.webp">`,
		`<meta property="og:type" content="product">`,
		`fetchpriority="high"`,
		`srcset="/media/abcdef0123456789-400.webp 400w, /media/abcdef0123456789-900.webp 900w, /media/abcdef0123456789-1600.webp 1600w"`,
		`width="1600" height="1200"`,
		`sizes="(min-width: 900px) 50vw, 100vw"`,
		`value="`, // variant radios
		`disabled><span>1kg</span>`,
		`SKU HNY-500`,
		"Raw honey &lt;b&gt;from&lt;/b&gt;", // description escaped
	} {
		if !strings.Contains(body, want) {
			t.Errorf("product page missing %q", want)
		}
	}
	if strings.Count(body, "<h1") != 1 {
		t.Error("exactly one h1 expected")
	}
	m := jsonldRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no JSON-LD block")
	}
	var ld map[string]any
	if err := json.Unmarshal([]byte(m[1]), &ld); err != nil {
		t.Fatalf("JSON-LD not valid JSON: %v\n%s", err, m[1])
	}
	offers := ld["offers"].([]any)
	first := offers[0].(map[string]any)
	if ld["@type"] != "Product" || len(offers) != 2 || first["price"].(float64) != 650 || first["priceCurrency"] != "BDT" || first["sku"] != "HNY-500" {
		t.Fatalf("JSON-LD wrong: %v", ld)
	}
	if offers[1].(map[string]any)["availability"] != "https://schema.org/OutOfStock" {
		t.Fatal("sold-out variant should be OutOfStock")
	}
	if w := do(t, a, "GET", "/products/hidden", nil); w.Code != 404 {
		t.Fatalf("inactive product should 404, got %d", w.Code)
	}
	if w := do(t, a, "GET", "/products/nope", nil); w.Code != 404 {
		t.Fatalf("unknown slug should 404, got %d", w.Code)
	}
}

func TestTruncateAndSelectedVariant(t *testing.T) {
	if got := truncate("one two three four", 10); got != "one two…" {
		t.Fatalf("truncate = %q", got)
	}
	if got := truncate("short", 10); got != "short" {
		t.Fatalf("truncate short = %q", got)
	}
	vs := []store.Variant{{ID: 1, Stock: 0}, {ID: 2, Stock: 3}}
	if selectedVariant(vs).ID != 2 {
		t.Fatal("should pick first in-stock variant")
	}
	if selectedVariant(vs[:1]).ID != 1 {
		t.Fatal("should fall back to first variant")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `TEST_DATABASE_URL=... go test ./internal/app/ -run 'Home|ProductPage|Truncate'`
Expected: FAIL, `undefined: truncate`

- [ ] **Step 3: Add the `cardData` template func**

In `render.go`, add to the `template.FuncMap` returned by `funcs()`:

```go
		"cardData": func(c store.ProductCard, i int) map[string]any { return map[string]any{"C": c, "Eager": i < 4} },
```

- [ ] **Step 4: Write store_pages.go**

```go
package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"

	"avalonshop/internal/img"
	"avalonshop/internal/store"
)

const siteDescription = "Natural products from Rajshahi, Bangladesh. Small batches, honest sourcing, cash on delivery nationwide."

func (a *App) home(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	featured, err := a.st.ListProductCards(ctx, store.ListOpts{FeaturedOnly: true, Limit: 8})
	if err == nil && len(featured) == 0 {
		featured, err = a.st.ListProductCards(ctx, store.ListOpts{Limit: 8})
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	cats, err := a.st.ListCategories(ctx)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, "store/home.html", page{
		Title:       "Avalon Shop · Fresh from Rajshahi",
		Description: siteDescription,
		Data:        map[string]any{"Featured": featured, "Categories": cats},
	})
}

func (a *App) products(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	catSlug := r.URL.Query().Get("category")
	cats, err := a.st.ListCategories(ctx)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	var current *store.Category
	if catSlug != "" {
		c, err := a.st.GetCategoryBySlug(ctx, catSlug)
		if errors.Is(err, store.ErrNotFound) {
			a.notFound(w, r)
			return
		}
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		current = &c
	}
	cards, err := a.st.ListProductCards(ctx, store.ListOpts{CategorySlug: catSlug, Query: q})
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	p := page{Title: "All products", Description: "Browse every product from Avalon. " + siteDescription,
		Data: map[string]any{"Cards": cards, "Categories": cats, "Current": current, "Query": q}}
	switch {
	case q != "":
		p.Title = "Search: " + q
		p.NoIndex = true
	case current != nil:
		p.Title = current.Name
		p.Description = current.Name + " from Avalon. " + siteDescription
		p.Canonical = a.cfg.BaseURL + "/products?category=" + current.Slug
	}
	a.render(w, r, "store/products.html", p)
}

func (a *App) product(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, err := a.st.GetProductBySlug(ctx, r.PathValue("slug"), true)
	if errors.Is(err, store.ErrNotFound) {
		a.notFound(w, r)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	var related []store.ProductCard
	if p.CategoryID != nil {
		related, _ = a.st.ListProductCards(ctx, store.ListOpts{CategoryID: *p.CategoryID, ExcludeID: p.ID, Limit: 4})
	}
	desc := p.MetaDescription
	if desc == "" {
		desc = truncate(strings.ReplaceAll(p.Description, "\n", " "), 155)
	}
	pg := page{Title: p.Name, Description: desc, JSONLD: a.productJSONLD(p, desc),
		Data: map[string]any{"Product": p, "Related": related, "Selected": selectedVariant(p.Variants)}}
	if len(p.Images) > 0 {
		pg.OGImage = a.cfg.BaseURL + "/media/" + img.Filename(p.Images[0].File, p.Images[0].Width)
	}
	a.render(w, r, "store/product.html", pg)
}

// selectedVariant is the variant shown first: the first one in stock, else the first.
func selectedVariant(vs []store.Variant) store.Variant {
	for _, v := range vs {
		if v.Stock > 0 {
			return v
		}
	}
	if len(vs) > 0 {
		return vs[0]
	}
	return store.Variant{}
}

// truncate cuts s to at most n runes at a word boundary and appends an ellipsis.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)[:n]
	cut := string(runes)
	if i := strings.LastIndex(cut, " "); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,.;:") + "…"
}

func (a *App) productJSONLD(p store.ProductFull, desc string) template.JS {
	url := a.cfg.BaseURL + "/products/" + p.Slug
	images := []string{}
	for _, im := range p.Images {
		images = append(images, a.cfg.BaseURL+"/media/"+img.Filename(im.File, im.Width))
	}
	offers := []map[string]any{}
	for _, v := range p.Variants {
		avail := "https://schema.org/InStock"
		if v.Stock == 0 {
			avail = "https://schema.org/OutOfStock"
		}
		o := map[string]any{"@type": "Offer", "name": v.Name, "price": v.Price, "priceCurrency": "BDT", "availability": avail, "url": url}
		if v.SKU != nil {
			o["sku"] = *v.SKU
		}
		offers = append(offers, o)
	}
	sort.SliceStable(offers, func(i, j int) bool { return offers[i]["price"].(int) < offers[j]["price"].(int) })
	data := map[string]any{
		"@context": "https://schema.org", "@type": "Product",
		"name": p.Name, "description": desc, "image": images, "url": url,
		"brand":  map[string]any{"@type": "Brand", "name": "Avalon"},
		"offers": offers,
	}
	b, err := json.Marshal(data)
	if err != nil {
		return template.JS(fmt.Sprintf(`{"error":%q}`, err.Error()))
	}
	return template.JS(b)
}
```

- [ ] **Step 5: Register the routes**

In `app.go` `routes()`, above the `m.HandleFunc("/", a.notFound)` line, add:

```go
	m.HandleFunc("GET /{$}", a.home)
	m.HandleFunc("GET /products", a.products)
	m.HandleFunc("GET /products/{slug}", a.product)
```

- [ ] **Step 6: Write the templates**

`avalonshop/templates/partials/product_card.html`:

```html
<a class="card" href="/products/{{.C.Slug}}">
  {{if .C.ImageFile}}<img src="{{imgURL (deref .C.ImageFile) 400}}" srcset="{{srcset (deref .C.ImageFile) (derefInt .C.ImageWidth)}}" sizes="(min-width: 1024px) 25vw, (min-width: 640px) 33vw, 50vw" width="{{derefInt .C.ImageWidth}}" height="{{derefInt .C.ImageHeight}}" alt="{{deref .C.ImageAlt}}"{{if not .Eager}} loading="lazy"{{end}} decoding="async">{{else}}<div class="ph" role="img" aria-label="No image yet"></div>{{end}}
  <div class="body">
    <p class="name">{{.C.Name}}</p>
    <div class="line">
      <span class="price">{{if gt .C.VariantCount 1}}from {{end}}{{taka .C.MinPrice}}</span>
      <span class="hint">{{if not .C.InStock}}Sold out{{else if gt .C.VariantCount 1}}{{.C.VariantCount}} options{{end}}</span>
    </div>
  </div>
</a>
```

`avalonshop/templates/store/home.html`:

```html
{{define "content"}}
<section class="hero">
  <p class="eyebrow">Fresh from Rajshahi</p>
  <h1>Good things,<br>grown well.</h1>
  <p>Small batches. Honest sourcing. Cash on delivery anywhere in Bangladesh.</p>
  <a class="btn" href="/products">Shop now</a>
</section>
{{with .Data.Categories}}<div class="chips">{{range .}}<a class="chip" href="/products?category={{.Slug}}">{{.Name}}</a>{{end}}</div>{{end}}
<div class="section-head"><h2>Featured</h2><a href="/products">All products →</a></div>
{{if .Data.Featured}}<div class="grid">{{range $i, $c := .Data.Featured}}{{template "product_card.html" (cardData $c $i)}}{{end}}</div>
{{else}}<p class="empty">Products are on their way.</p>{{end}}
{{end}}
```

`avalonshop/templates/store/products.html`:

```html
{{define "content"}}
<p class="eyebrow">Shop</p>
<h1>{{if .Data.Current}}{{.Data.Current.Name}}{{else if .Data.Query}}Results for “{{.Data.Query}}”{{else}}All products{{end}}</h1>
<form class="search" method="get" action="/products" role="search">
  <input type="search" name="q" value="{{.Data.Query}}" placeholder="Search products" aria-label="Search products">
  <button class="btn ghost" type="submit">Search</button>
</form>
<div class="chips">
  <a class="chip{{if not .Data.Current}} on{{end}}" href="/products">All</a>
  {{$cur := .Data.Current}}{{range .Data.Categories}}<a class="chip{{if and $cur (eq $cur.ID .ID)}} on{{end}}" href="/products?category={{.Slug}}">{{.Name}}</a>{{end}}
</div>
{{if .Data.Cards}}<div class="grid">{{range $i, $c := .Data.Cards}}{{template "product_card.html" (cardData $c $i)}}{{end}}</div>
{{else}}<p class="empty">Nothing here yet.</p>{{end}}
{{end}}
```

`avalonshop/templates/store/product.html`:

```html
{{define "content"}}
{{$p := .Data.Product}}{{$sel := .Data.Selected}}
<p class="crumb"><a href="/products">Shop</a>{{if $p.Category}} / <a href="/products?category={{$p.Category.Slug}}">{{$p.Category.Name}}</a>{{end}} / {{$p.Name}}</p>
<div class="pp">
  <div class="gallery">
    {{if $p.Images}}{{$first := index $p.Images 0}}
    <img id="gallery-main" class="main" src="{{imgURL $first.File 900}}" srcset="{{srcset $first.File $first.Width}}" sizes="(min-width: 900px) 50vw, 100vw" width="{{$first.Width}}" height="{{$first.Height}}" alt="{{$first.Alt}}" fetchpriority="high" decoding="async">
    {{if gt (len $p.Images) 1}}<div class="thumbs">{{range $i, $im := $p.Images}}<img{{if eq $i 0}} class="on"{{end}} src="{{imgURL $im.File 400}}" width="64" height="64" alt="{{$im.Alt}}" loading="lazy" decoding="async" tabindex="0" data-thumb data-src="{{imgURL $im.File 900}}" data-srcset="{{srcset $im.File $im.Width}}" data-alt="{{$im.Alt}}">{{end}}</div>{{end}}
    {{else}}<div class="main ph" role="img" aria-label="No image yet"></div>{{end}}
  </div>
  <div class="buy">
    {{if $p.Category}}<p class="eyebrow">{{$p.Category.Name}}</p>{{end}}
    <h1>{{$p.Name}}</h1>
    {{if $p.Variants}}
    <form method="post" action="/cart/items" hx-post="/cart/items" hx-target="#drawer" hx-swap="innerHTML">
      <span class="price"><span id="price">{{taka $sel.Price}}</span><small id="stock">{{if gt $sel.Stock 0}}In stock{{else}}Sold out{{end}}</small></span>
      {{if gt (len $p.Variants) 1}}<p class="eyebrow muted">Option</p>{{end}}
      <div class="pills">{{range $p.Variants}}<label class="pill"><input type="radio" name="variant_id" value="{{.ID}}" data-price-text="{{taka .Price}}" data-stock="{{.Stock}}" data-stock-text="{{if gt .Stock 0}}In stock{{else}}Sold out{{end}}"{{if eq .ID $sel.ID}} checked{{end}}{{if eq .Stock 0}} disabled{{end}}><span>{{.Name}}</span></label>{{end}}</div>
      <div class="buy-row">
        <span class="qty"><button type="button" data-step="-1" aria-label="Decrease quantity">−</button><input type="number" name="qty" value="1" min="1" max="{{if gt $sel.Stock 0}}{{$sel.Stock}}{{else}}1{{end}}" aria-label="Quantity"><button type="button" data-step="1" aria-label="Increase quantity">+</button></span>
        <button class="btn" type="submit"{{if eq $sel.Stock 0}} disabled{{end}}>Add to cart</button>
      </div>
    </form>
    {{else}}<p class="muted">Not available right now.</p>{{end}}
    {{if $p.Description}}<p class="desc">{{$p.Description}}</p>{{end}}
    <div class="meta"><span>Cash on delivery</span><span>Delivery across Bangladesh</span>{{if $sel.SKU}}<span>SKU {{deref $sel.SKU}}</span>{{end}}<span>Ships in 1–2 days</span></div>
  </div>
</div>
{{if .Data.Related}}<section class="related"><div class="section-head"><h2>You might also like</h2></div><div class="grid">{{range $i, $c := .Data.Related}}{{template "product_card.html" (cardData $c $i)}}{{end}}</div></section>{{end}}
{{end}}
```

- [ ] **Step 7: Run to verify it passes**

Run: `TEST_DATABASE_URL=... go test ./internal/app/`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add avalonshop
git commit -m "feat(shop): home, listing, and product pages with SEO metadata and JSON-LD"
```

---

### Task 12: Cart (cookie cart, HTMX drawer, cart page)

**Files:**
- Create: `avalonshop/internal/app/cart.go`, `avalonshop/internal/app/cart_test.go`, `avalonshop/templates/partials/cart_drawer.html`, `avalonshop/templates/store/cart.html`
- Modify: `avalonshop/internal/app/app.go` (routes)

**Interfaces:**
- Consumes: `token.CartLine`, `a.cartLines`, `a.saveCart`, `store.VariantsForCart`.
- Produces: routes `GET /cart`, `GET /cart/drawer`, `POST /cart/items` (form `variant_id`, `qty`), `POST /cart/items/{id}` (form `qty`, 0 removes); types `cartLineView{store.CartVariant; Qty, LineTotal int; Short bool}`, `cartView{Lines []cartLineView; Subtotal, Count int}`; `(*App) buildCart(ctx, lines) (cartView, []token.CartLine, error)` returning the lines that survived (vanished/inactive variants dropped). Checkout (Task 13) reuses `buildCart`.

- [ ] **Step 1: Write the failing tests**

`avalonshop/internal/app/cart_test.go`:

```go
package app

import (
	"context"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func variantIDs(t *testing.T, a *App, slug string) (int64, int64) {
	t.Helper()
	p, err := a.st.GetProductBySlug(context.Background(), slug, false)
	if err != nil {
		t.Fatal(err)
	}
	return p.Variants[0].ID, p.Variants[1].ID
}

func cookieHeader(w *httptest.ResponseRecorder, name string) string {
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

func TestCartAddUpdateRemove(t *testing.T) {
	a, st := newDBApp(t)
	slug := seedCatalog(t, st)
	v1, _ := variantIDs(t, a, slug)
	form := strings.NewReader("variant_id=" + itoa(v1) + "&qty=2")
	w := do(t, a, "POST", "/cart/items", form)
	if w.Code != 303 || w.Header().Get("Location") != "/cart" {
		t.Fatalf("plain add: %d %s", w.Code, w.Header().Get("Location"))
	}
	cart := cookieHeader(w, "cart")
	if cart == "" {
		t.Fatal("no cart cookie set")
	}
	// second add of the same variant increments
	w = do(t, a, "POST", "/cart/items", strings.NewReader("variant_id="+itoa(v1)+"&qty=1"), "Cookie", "cart="+cart)
	cart = cookieHeader(w, "cart")
	w = do(t, a, "GET", "/cart", nil, "Cookie", "cart="+cart)
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, "Wild Forest Honey") || !strings.Contains(body, `value="3"`) || !strings.Contains(body, "৳ 1,950") {
		t.Fatalf("cart page: %d\n%s", w.Code, body)
	}
	if !strings.Contains(body, `content="noindex"`) {
		t.Fatal("cart must be noindex")
	}
	// HTMX add returns the drawer plus an out-of-band badge
	w = do(t, a, "POST", "/cart/items", strings.NewReader("variant_id="+itoa(v1)+"&qty=1"), "Cookie", "cart="+cart, "HX-Request", "true")
	body = w.Body.String()
	if w.Code != 200 || !strings.Contains(body, `id="cart-count" class="badge" hx-swap-oob="true">4<`) || !strings.Contains(body, `href="/checkout"`) {
		t.Fatalf("htmx add: %d\n%s", w.Code, body)
	}
	cart = cookieHeader(w, "cart")
	// set qty to 0 removes the line
	w = do(t, a, "POST", "/cart/items/"+itoa(v1), strings.NewReader("qty=0"), "Cookie", "cart="+cart, "HX-Request", "true")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Your cart is empty") {
		t.Fatalf("remove: %d\n%s", w.Code, w.Body.String())
	}
	// unknown variant → flash + redirect
	w = do(t, a, "POST", "/cart/items", strings.NewReader("variant_id=999999&qty=1"))
	if w.Code != 303 || w.Header().Get("Location") != "/products" || cookieHeader(w, "flash") == "" {
		t.Fatalf("unknown variant: %d %s", w.Code, w.Header().Get("Location"))
	}
	// drawer GET
	w = do(t, a, "GET", "/cart/drawer", nil, "HX-Request", "true")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Your cart is empty") {
		t.Fatalf("drawer: %d", w.Code)
	}
}

func TestCartDropsInactiveAndFlagsShort(t *testing.T) {
	a, st := newDBApp(t)
	slug := seedCatalog(t, st)
	v1, v2 := variantIDs(t, a, slug)
	// v2 has stock 0: adding it is refused
	w := do(t, a, "POST", "/cart/items", strings.NewReader("variant_id="+itoa(v2)+"&qty=1"))
	if w.Code != 303 || cookieHeader(w, "cart") != "" {
		t.Fatalf("sold-out variant should not be added: %d", w.Code)
	}
	w = do(t, a, "POST", "/cart/items", strings.NewReader("variant_id="+itoa(v1)+"&qty=9"))
	cart := cookieHeader(w, "cart")
	w = do(t, a, "GET", "/cart", nil, "Cookie", "cart="+cart)
	if !strings.Contains(w.Body.String(), "Only 5 left") {
		t.Fatalf("short line not flagged:\n%s", w.Body.String())
	}
	// deactivate the product: the line disappears and the cookie is rewritten
	p, _ := st.GetProductBySlug(context.Background(), slug, false)
	p.Active = false
	st.UpdateProduct(context.Background(), p.Product, p.Variants)
	w = do(t, a, "GET", "/cart", nil, "Cookie", "cart="+cart)
	if !strings.Contains(w.Body.String(), "Your cart is empty") {
		t.Fatal("inactive product should vanish from cart")
	}
	if c := w.Result().Cookies(); len(c) == 0 || c[0].Name != "cart" || c[0].MaxAge != -1 {
		t.Fatalf("cart cookie should be cleared: %v", c)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
```

- [ ] **Step 2: Run to verify it fails**

Run: `TEST_DATABASE_URL=... go test ./internal/app/ -run Cart`
Expected: FAIL with 404s (routes missing)

- [ ] **Step 3: Write cart.go**

```go
package app

import (
	"context"
	"net/http"
	"strconv"

	"avalonshop/internal/store"
	"avalonshop/internal/token"
)

type cartLineView struct {
	store.CartVariant
	Qty       int
	LineTotal int
	Short     bool // requested qty exceeds stock
}

type cartView struct {
	Lines    []cartLineView
	Subtotal int
	Count    int
}

// buildCart resolves cookie lines against the database. Lines whose variant is
// gone or whose product is inactive are dropped; the surviving lines are returned
// so the caller can rewrite the cookie.
func (a *App) buildCart(ctx context.Context, lines []token.CartLine) (cartView, []token.CartLine, error) {
	var v cartView
	if len(lines) == 0 {
		return v, nil, nil
	}
	ids := make([]int64, 0, len(lines))
	for _, l := range lines {
		ids = append(ids, l.VariantID)
	}
	vars, err := a.st.VariantsForCart(ctx, ids)
	if err != nil {
		return v, nil, err
	}
	byID := map[int64]store.CartVariant{}
	for _, cv := range vars {
		if cv.Active {
			byID[cv.VariantID] = cv
		}
	}
	var kept []token.CartLine
	for _, l := range lines {
		cv, ok := byID[l.VariantID]
		if !ok {
			continue
		}
		kept = append(kept, l)
		v.Lines = append(v.Lines, cartLineView{CartVariant: cv, Qty: l.Qty, LineTotal: cv.Price * l.Qty, Short: cv.Stock < l.Qty})
		v.Subtotal += cv.Price * l.Qty
		v.Count += l.Qty
	}
	return v, kept, nil
}

// loadCart reads the cookie, builds the view, and rewrites the cookie if lines were dropped.
func (a *App) loadCart(w http.ResponseWriter, r *http.Request) (cartView, []token.CartLine, error) {
	lines := a.cartLines(r)
	v, kept, err := a.buildCart(r.Context(), lines)
	if err == nil && len(kept) != len(lines) {
		a.saveCart(w, kept)
	}
	return v, kept, err
}

func clampQty(s string, lo int) int {
	q, _ := strconv.Atoi(s)
	if q < lo {
		q = lo
	}
	if q > 99 {
		q = 99
	}
	return q
}

func (a *App) cartAdd(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.FormValue("variant_id"), 10, 64)
	qty := clampQty(r.FormValue("qty"), 1)
	vars, err := a.st.VariantsForCart(r.Context(), []int64{id})
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if len(vars) == 0 || !vars[0].Active || vars[0].Stock == 0 {
		a.setFlash(w, "That item is not available right now.")
		a.redirect(w, r, "/products")
		return
	}
	lines := a.cartLines(r)
	found := false
	for i := range lines {
		if lines[i].VariantID == id {
			lines[i].Qty = min(lines[i].Qty+qty, 99)
			found = true
		}
	}
	if !found {
		if len(lines) >= token.MaxCartLines {
			a.setFlash(w, "Your cart is full.")
			a.redirect(w, r, "/cart")
			return
		}
		lines = append(lines, token.CartLine{VariantID: id, Qty: qty})
	}
	a.saveCart(w, lines)
	a.respondCart(w, r, lines, "Added to cart.")
}

func (a *App) cartUpdate(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	qty := clampQty(r.FormValue("qty"), 0)
	lines := a.cartLines(r)
	out := lines[:0]
	for _, l := range lines {
		if l.VariantID == id {
			if qty == 0 {
				continue
			}
			l.Qty = qty
		}
		out = append(out, l)
	}
	a.saveCart(w, out)
	a.respondCart(w, r, out, "")
}

// respondCart renders the drawer for HTMX, or flashes and redirects to /cart.
func (a *App) respondCart(w http.ResponseWriter, r *http.Request, lines []token.CartLine, flash string) {
	if isHTMX(r) {
		v, kept, err := a.buildCart(r.Context(), lines)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		if len(kept) != len(lines) {
			a.saveCart(w, kept)
		}
		a.renderPartial(w, "cart_drawer.html", v)
		return
	}
	if flash != "" {
		a.setFlash(w, flash)
	}
	http.Redirect(w, r, "/cart", http.StatusSeeOther)
}

func (a *App) cartDrawer(w http.ResponseWriter, r *http.Request) {
	v, _, err := a.loadCart(w, r)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.renderPartial(w, "cart_drawer.html", v)
}

func (a *App) cartPage(w http.ResponseWriter, r *http.Request) {
	v, _, err := a.loadCart(w, r)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, "store/cart.html", page{Title: "Your cart", NoIndex: true, Data: v})
}
```

- [ ] **Step 4: Register the routes**

In `routes()`, above the `/` fallback:

```go
	m.HandleFunc("GET /cart", a.cartPage)
	m.HandleFunc("GET /cart/drawer", a.cartDrawer)
	m.HandleFunc("POST /cart/items", a.cartAdd)
	m.HandleFunc("POST /cart/items/{id}", a.cartUpdate)
```

- [ ] **Step 5: Write the templates**

`avalonshop/templates/partials/cart_drawer.html`:

```html
<div class="head"><h2>Your cart</h2><button class="x" type="button" data-close-cart aria-label="Close cart">×</button></div>
<div class="lines">
{{if .Lines}}{{range .Lines}}
<div class="cl">
  {{if .ImageFile}}<img src="{{imgURL (deref .ImageFile) 400}}" width="64" height="64" alt="{{deref .ImageAlt}}" loading="lazy" decoding="async">{{else}}<div class="ph"></div>{{end}}
  <div>
    <p class="n"><a href="/products/{{.ProductSlug}}">{{.ProductName}}</a></p>
    <div class="v">{{.VariantName}} · {{taka .Price}}</div>
    {{if .Short}}<div class="v err" role="alert">Only {{.Stock}} left</div>{{end}}
    <form method="post" action="/cart/items/{{.VariantID}}" hx-post="/cart/items/{{.VariantID}}" hx-target="#drawer" hx-swap="innerHTML" hx-trigger="change">
      <span class="qty"><button type="button" data-step="-1" aria-label="Decrease quantity">−</button><input type="number" name="qty" value="{{.Qty}}" min="0" max="99" aria-label="Quantity"><button type="button" data-step="1" aria-label="Increase quantity">+</button></span>
    </form>
  </div>
  <div class="price">{{taka .LineTotal}}</div>
</div>
{{end}}{{else}}<p class="empty">Your cart is empty.</p>{{end}}
</div>
<div class="foot">
  <div class="totals"><span>Subtotal</span><span class="price">{{taka .Subtotal}}</span></div>
  <p class="help">Delivery is added at checkout.</p>
  {{if .Lines}}<a class="btn" href="/checkout">Checkout</a>{{else}}<a class="btn ghost" href="/products">Browse products</a>{{end}}
</div>
{{template "cart_badge.html" .Count}}
```

Add to `app.css` under the cart section: `.cl .err{color:var(--danger)}`.

`avalonshop/templates/store/cart.html`:

```html
{{define "content"}}
<p class="eyebrow">Cart</p>
<h1>Your cart</h1>
{{if .Data.Lines}}
<div class="cart-page">
  <div>
  {{range .Data.Lines}}
  <div class="cl">
    {{if .ImageFile}}<img src="{{imgURL (deref .ImageFile) 400}}" width="64" height="64" alt="{{deref .ImageAlt}}" loading="lazy" decoding="async">{{else}}<div class="ph"></div>{{end}}
    <div>
      <p class="n"><a href="/products/{{.ProductSlug}}">{{.ProductName}}</a></p>
      <div class="v">{{.VariantName}} · {{taka .Price}}</div>
      {{if .Short}}<div class="v err" role="alert">Only {{.Stock}} left</div>{{end}}
      <form method="post" action="/cart/items/{{.VariantID}}">
        <span class="qty"><button type="button" data-step="-1" aria-label="Decrease quantity">−</button><input type="number" name="qty" value="{{.Qty}}" min="0" max="99" aria-label="Quantity"><button type="button" data-step="1" aria-label="Increase quantity">+</button></span>
        <button class="btn ghost sm" type="submit">Update</button>
      </form>
    </div>
    <div class="price">{{taka .LineTotal}}</div>
  </div>
  {{end}}
  </div>
  <aside class="panel">
    <div class="totals"><span>Subtotal</span><span class="price">{{taka .Data.Subtotal}}</span></div>
    <p class="help">Delivery is added at checkout. Cash on delivery.</p>
    <a class="btn" href="/checkout">Checkout</a>
  </aside>
</div>
{{else}}
<p class="empty">Your cart is empty.</p>
<p><a class="btn" href="/products">Browse products</a></p>
{{end}}
{{end}}
```

- [ ] **Step 6: Run to verify it passes**

Run: `TEST_DATABASE_URL=... go test ./internal/app/`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add avalonshop
git commit -m "feat(shop): cookie cart with HTMX drawer and cart page"
```

---

### Task 13: Checkout, order page, order emails

**Files:**
- Create: `avalonshop/internal/app/checkout.go`, `avalonshop/internal/app/checkout_test.go`, `avalonshop/templates/store/checkout.html`, `avalonshop/templates/store/order.html`, `avalonshop/templates/partials/checkout_totals.html`
- Modify: `avalonshop/internal/app/app.go` (routes)

**Interfaces:**
- Consumes: `a.loadCart`/`a.buildCart` (Task 12), `store.PlaceOrder`, `store.ListZones`, `store.GetOrderByNumber`, `a.tok.OrderToken`, `mail.Send`, `a.checkoutLimit`.
- Produces: routes `GET /checkout`, `POST /checkout`, `GET /checkout/totals?zone_id=`, `GET /orders/{number}?t=`; `normalizePhone(s) (string, bool)`, `validEmail(s) bool`, `(*App) orderURL(number) string`, `(*App) orderMailData(store.OrderFull) map[string]any`, `(*App) sendOrderMails(o store.OrderFull, customerTmpl, adminTmpl string)` (Task 18 reuses this for shipped/delivered), `totalsView{Subtotal, Fee, Total int; HasZone bool}`.

- [ ] **Step 1: Write the failing tests**

`avalonshop/internal/app/checkout_test.go`:

```go
package app

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"avalonshop/internal/store"
)

func checkoutFixture(t *testing.T) (*App, *store.Store, string, int64) {
	t.Helper()
	a, st := newDBApp(t)
	slug := seedCatalog(t, st)
	v1, _ := variantIDs(t, a, slug)
	zone, err := st.CreateZone(context.Background(), store.Zone{Name: "Rajshahi", Fee: 60, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	w := do(t, a, "POST", "/cart/items", strings.NewReader("variant_id="+itoa(v1)+"&qty=2"))
	return a, st, cookieHeader(w, "cart"), zone
}

func checkoutBody(zone int64, phone string) string {
	v := url.Values{"name": {"Ana"}, "phone": {phone}, "email": {"ana@example.com"}, "address": {"House 1, Road 2, Rajshahi"}, "zone_id": {itoa(zone)}, "note": {"ring the bell"}}
	return v.Encode()
}

func TestCheckoutHappyPath(t *testing.T) {
	a, st, cart, zone := checkoutFixture(t)
	w := do(t, a, "GET", "/checkout", nil, "Cookie", "cart="+cart)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Rajshahi") || !strings.Contains(w.Body.String(), "৳ 1,300") {
		t.Fatalf("checkout page: %d\n%s", w.Code, w.Body.String())
	}
	w = do(t, a, "GET", "/checkout/totals?zone_id="+itoa(zone), nil, "Cookie", "cart="+cart, "HX-Request", "true")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "৳ 1,360") {
		t.Fatalf("totals partial: %d\n%s", w.Code, w.Body.String())
	}
	w = do(t, a, "POST", "/checkout", strings.NewReader(checkoutBody(zone, "+88 01712-345678")), "Cookie", "cart="+cart)
	loc := w.Header().Get("Location")
	if w.Code != 303 || !strings.HasPrefix(loc, "/orders/AV-001001?t=") {
		t.Fatalf("place order: %d %s\n%s", w.Code, loc, w.Body.String())
	}
	cleared := false
	for _, c := range w.Result().Cookies() {
		if c.Name == "cart" && c.MaxAge == -1 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("cart cookie not cleared after order")
	}
	w = do(t, a, "GET", loc, nil)
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, "Thank you, Ana") || !strings.Contains(body, "৳ 1,360") || !strings.Contains(body, `content="noindex"`) {
		t.Fatalf("order page: %d\n%s", w.Code, body)
	}
	if w := do(t, a, "GET", "/orders/AV-001001", nil); w.Code != 404 {
		t.Fatalf("order page without token should 404, got %d", w.Code)
	}
	if w := do(t, a, "GET", "/orders/AV-001001?t=0000000000000000", nil); w.Code != 404 {
		t.Fatalf("bad token should 404, got %d", w.Code)
	}
	o, err := st.GetOrderByNumber(context.Background(), "AV-001001")
	if err != nil || o.Phone != "01712345678" || o.Note != "ring the bell" || o.Total != 1360 {
		t.Fatalf("stored order: %+v %v", o.Order, err)
	}
	a.mail.Wait()
}

func TestCheckoutValidationAndOversell(t *testing.T) {
	a, _, cart, zone := checkoutFixture(t)
	w := do(t, a, "POST", "/checkout", strings.NewReader(checkoutBody(zone, "12345")), "Cookie", "cart="+cart)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "valid Bangladeshi mobile") {
		t.Fatalf("bad phone: %d", w.Code)
	}
	w = do(t, a, "POST", "/checkout", strings.NewReader(checkoutBody(0, "01712345678")), "Cookie", "cart="+cart)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "Choose a delivery area") {
		t.Fatalf("missing zone: %d", w.Code)
	}
	// bump qty to 9 (stock is 5) then try to order
	slug := "wild-forest-honey"
	v1, _ := variantIDs(t, a, slug)
	w = do(t, a, "POST", "/cart/items/"+itoa(v1), strings.NewReader("qty=9"), "Cookie", "cart="+cart)
	cart = cookieHeader(w, "cart")
	w = do(t, a, "POST", "/checkout", strings.NewReader(checkoutBody(zone, "01712345678")), "Cookie", "cart="+cart)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "Only 5 of") {
		t.Fatalf("oversell: %d\n%s", w.Code, w.Body.String())
	}
	if w := do(t, a, "GET", "/checkout", nil); w.Code != 303 || w.Header().Get("Location") != "/cart" {
		t.Fatalf("empty cart should redirect: %d", w.Code)
	}
}

func TestCheckoutRateLimit(t *testing.T) {
	a, _, cart, zone := checkoutFixture(t)
	a.checkoutLimit = newLimiter(1, time.Hour)
	do(t, a, "POST", "/checkout", strings.NewReader(checkoutBody(zone, "bad")), "Cookie", "cart="+cart)
	w := do(t, a, "POST", "/checkout", strings.NewReader(checkoutBody(zone, "01712345678")), "Cookie", "cart="+cart)
	if w.Code != 429 {
		t.Fatalf("second attempt should be rate limited, got %d", w.Code)
	}
}

func TestPhoneAndEmail(t *testing.T) {
	ok := map[string]string{"01712345678": "01712345678", "+8801712345678": "01712345678", "01712 345 678": "01712345678", "8801912345678": "01912345678"}
	for in, want := range ok {
		if got, valid := normalizePhone(in); !valid || got != want {
			t.Errorf("normalizePhone(%q) = %q,%v", in, got, valid)
		}
	}
	for _, bad := range []string{"", "0171234567", "02712345678", "017123456789", "abc"} {
		if _, valid := normalizePhone(bad); valid {
			t.Errorf("normalizePhone(%q) should be invalid", bad)
		}
	}
	if !validEmail("a@b.co") || validEmail("nope") || validEmail("a b@c.d") || validEmail("a@b") {
		t.Error("validEmail wrong")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `TEST_DATABASE_URL=... go test ./internal/app/ -run 'Checkout|Phone'`
Expected: FAIL, `undefined: normalizePhone`

- [ ] **Step 3: Write checkout.go**

```go
package app

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"avalonshop/internal/store"
)

type checkoutForm struct {
	Name, Phone, Email, Address, Note string
	ZoneID                            int64
	Errors                            map[string]string
}

type totalsView struct {
	Subtotal, Fee, Total int
	HasZone              bool
}

type checkoutData struct {
	Totals totalsView
	Cart   cartView
	Zones  []store.Zone
	Form   checkoutForm
	Error  string
}

// normalizePhone accepts Bangladeshi mobile numbers in any common spelling and
// returns the 11-digit local form (01XXXXXXXXX).
func normalizePhone(s string) (string, bool) {
	var d strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			d.WriteRune(r)
		}
	}
	p := d.String()
	if len(p) == 13 && strings.HasPrefix(p, "88") {
		p = p[2:]
	}
	if len(p) == 11 && p[0] == '0' && p[1] == '1' && p[2] >= '3' && p[2] <= '9' {
		return p, true
	}
	return "", false
}

func validEmail(s string) bool {
	at := strings.LastIndex(s, "@")
	return len(s) >= 6 && len(s) <= 254 && at > 0 && strings.Contains(s[at:], ".") && !strings.ContainsAny(s, " \t\n\r")
}

func parseCheckout(r *http.Request) checkoutForm {
	f := checkoutForm{
		Name: strings.TrimSpace(r.FormValue("name")), Phone: strings.TrimSpace(r.FormValue("phone")),
		Email: strings.TrimSpace(strings.ToLower(r.FormValue("email"))), Address: strings.TrimSpace(r.FormValue("address")),
		Note: strings.TrimSpace(r.FormValue("note")), Errors: map[string]string{},
	}
	f.ZoneID, _ = strconv.ParseInt(r.FormValue("zone_id"), 10, 64)
	if l := utf8.RuneCountInString(f.Name); l < 1 || l > 100 {
		f.Errors["name"] = "Please enter your name."
	}
	if p, ok := normalizePhone(f.Phone); ok {
		f.Phone = p
	} else {
		f.Errors["phone"] = "Enter a valid Bangladeshi mobile number, e.g. 01712 345678."
	}
	if !validEmail(f.Email) {
		f.Errors["email"] = "Enter a valid email address."
	}
	if l := utf8.RuneCountInString(f.Address); l < 10 || l > 500 {
		f.Errors["address"] = "Please enter your full delivery address (at least 10 characters)."
	}
	if f.ZoneID == 0 {
		f.Errors["zone_id"] = "Choose a delivery area."
	}
	if utf8.RuneCountInString(f.Note) > 500 {
		f.Errors["note"] = "Note is too long (500 characters max)."
	}
	return f
}

func feeFor(zones []store.Zone, id int64) (int, bool) {
	for _, z := range zones {
		if z.ID == id {
			return z.Fee, true
		}
	}
	return 0, false
}

// checkoutView renders the checkout page; redirects to /cart when the cart is empty.
func (a *App) checkoutView(w http.ResponseWriter, r *http.Request, f checkoutForm, status int, errMsg string) {
	v, kept, err := a.loadCart(w, r)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if len(kept) == 0 {
		a.setFlash(w, "Your cart is empty.")
		http.Redirect(w, r, "/cart", http.StatusSeeOther)
		return
	}
	zones, err := a.st.ListZones(r.Context(), true)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	fee, has := feeFor(zones, f.ZoneID)
	a.renderStatus(w, r, status, "store/checkout.html", page{Title: "Checkout", NoIndex: true, Data: checkoutData{
		Totals: totalsView{Subtotal: v.Subtotal, Fee: fee, Total: v.Subtotal + fee, HasZone: has},
		Cart:   v, Zones: zones, Form: f, Error: errMsg,
	}})
}

func (a *App) checkoutGet(w http.ResponseWriter, r *http.Request) {
	f := checkoutForm{Errors: map[string]string{}}
	if u := a.currentUser(r); u != nil {
		f.Name, f.Phone, f.Email, f.Address = u.Name, u.Phone, u.Email, u.Address
	}
	if zones, err := a.st.ListZones(r.Context(), true); err == nil && len(zones) == 1 {
		f.ZoneID = zones[0].ID
	}
	a.checkoutView(w, r, f, http.StatusOK, "")
}

func (a *App) checkoutTotals(w http.ResponseWriter, r *http.Request) {
	zoneID, _ := strconv.ParseInt(r.URL.Query().Get("zone_id"), 10, 64)
	v, _, err := a.buildCart(r.Context(), a.cartLines(r))
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	zones, err := a.st.ListZones(r.Context(), true)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	fee, has := feeFor(zones, zoneID)
	a.renderPartial(w, "checkout_totals.html", totalsView{Subtotal: v.Subtotal, Fee: fee, Total: v.Subtotal + fee, HasZone: has})
}

func (a *App) checkoutPost(w http.ResponseWriter, r *http.Request) {
	f := parseCheckout(r)
	if !a.checkoutLimit.Allow(clientIP(r)) {
		a.checkoutView(w, r, f, http.StatusTooManyRequests, "Too many attempts. Please try again in an hour.")
		return
	}
	if len(f.Errors) > 0 {
		a.checkoutView(w, r, f, http.StatusUnprocessableEntity, "Please fix the highlighted fields.")
		return
	}
	_, kept, err := a.loadCart(w, r)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if len(kept) == 0 {
		a.setFlash(w, "Your cart is empty.")
		http.Redirect(w, r, "/cart", http.StatusSeeOther)
		return
	}
	in := store.NewOrder{Name: f.Name, Phone: f.Phone, Email: f.Email, Address: f.Address, Note: f.Note, ZoneID: f.ZoneID}
	for _, l := range kept {
		in.Lines = append(in.Lines, store.OrderLine{VariantID: l.VariantID, Qty: l.Qty})
	}
	u := a.currentUser(r)
	if u != nil {
		in.UserID = &u.ID
	}
	o, err := a.st.PlaceOrder(r.Context(), in)
	var oos store.ErrOutOfStock
	switch {
	case errors.As(err, &oos):
		a.checkoutView(w, r, f, http.StatusUnprocessableEntity, "Only "+strconv.Itoa(oos.Available)+" of "+oos.Name+" available. Please adjust your cart.")
		return
	case errors.Is(err, store.ErrNotFound):
		a.setFlash(w, "Something in your cart is no longer available.")
		http.Redirect(w, r, "/cart", http.StatusSeeOther)
		return
	case err != nil:
		a.serverError(w, r, err)
		return
	}
	a.saveCart(w, nil)
	if u != nil {
		a.st.UpdateProfile(r.Context(), u.ID, f.Name, f.Phone, f.Address) // remember for next time; best effort
	}
	a.sendOrderMails(o, "order_confirmation", "order_new_admin")
	a.setFlash(w, "Order placed! We'll call you to confirm.")
	http.Redirect(w, r, a.orderURL(o.Number), http.StatusSeeOther)
}

func (a *App) orderURL(number string) string {
	return "/orders/" + number + "?t=" + a.tok.OrderToken(number)
}

func (a *App) orderMailData(o store.OrderFull) map[string]any {
	return map[string]any{
		"Order": o.Order, "Items": o.Items,
		"TrackURL": a.cfg.BaseURL + a.orderURL(o.Number),
		"AdminURL": a.cfg.BaseURL + "/admin/orders/" + strconv.FormatInt(o.ID, 10),
	}
}

// sendOrderMails queues customer and/or admin emails; either template may be "".
func (a *App) sendOrderMails(o store.OrderFull, customerTmpl, adminTmpl string) {
	data := a.orderMailData(o)
	if customerTmpl != "" {
		a.mail.Send(o.Email, customerTmpl, data)
	}
	if adminTmpl != "" {
		a.mail.Send(a.cfg.OrderNotifyEmail, adminTmpl, data)
	}
}

// orderPage is visible with the tracking token, to the owning user, or to an admin.
func (a *App) orderPage(w http.ResponseWriter, r *http.Request) {
	o, err := a.st.GetOrderByNumber(r.Context(), r.PathValue("number"))
	if errors.Is(err, store.ErrNotFound) {
		a.notFound(w, r)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	u := a.currentUser(r)
	allowed := a.tok.VerifyOrderToken(o.Number, r.URL.Query().Get("t")) ||
		(u != nil && (u.Role == "admin" || (o.UserID != nil && *o.UserID == u.ID)))
	if !allowed {
		a.notFound(w, r)
		return
	}
	a.render(w, r, "store/order.html", page{Title: "Order " + o.Number, NoIndex: true, Data: o})
}
```

- [ ] **Step 4: Register the routes**

In `routes()`, above the `/` fallback:

```go
	m.HandleFunc("GET /checkout", a.checkoutGet)
	m.HandleFunc("POST /checkout", a.checkoutPost)
	m.HandleFunc("GET /checkout/totals", a.checkoutTotals)
	m.HandleFunc("GET /orders/{number}", a.orderPage)
```

- [ ] **Step 5: Write the templates**

`avalonshop/templates/partials/checkout_totals.html`:

```html
<div class="totals">
  <span>Subtotal</span><span class="mono">{{taka .Subtotal}}</span>
  <span>Delivery</span><span class="mono">{{if .HasZone}}{{taka .Fee}}{{else}}—{{end}}</span>
  <span class="big">Total</span><span class="big price">{{taka .Total}}</span>
</div>
```

`avalonshop/templates/store/checkout.html`:

```html
{{define "content"}}
{{$d := .Data}}{{$f := $d.Form}}
<p class="eyebrow">Checkout</p>
<h1>Delivery details</h1>
{{if $d.Error}}<div class="errbox" role="alert">{{$d.Error}}</div>{{end}}
<div class="cart-page">
<form class="form wide" method="post" action="/checkout" novalidate>
  <div class="row">
    <div class="field"><label for="name">Full name</label><input id="name" type="text" name="name" value="{{$f.Name}}" required autocomplete="name">{{with $f.Errors.name}}<p class="err">{{.}}</p>{{end}}</div>
    <div class="field"><label for="phone">Mobile number</label><input id="phone" type="tel" name="phone" value="{{$f.Phone}}" required autocomplete="tel" placeholder="01712 345678">{{with $f.Errors.phone}}<p class="err">{{.}}</p>{{end}}</div>
  </div>
  <div class="field"><label for="email">Email</label><input id="email" type="email" name="email" value="{{$f.Email}}" required autocomplete="email"><p class="help">We'll send your order confirmation here.</p>{{with $f.Errors.email}}<p class="err">{{.}}</p>{{end}}</div>
  <div class="field"><label for="address">Delivery address</label><textarea id="address" name="address" required autocomplete="street-address">{{$f.Address}}</textarea>{{with $f.Errors.address}}<p class="err">{{.}}</p>{{end}}</div>
  <fieldset class="field radio-list"><legend class="eyebrow">Delivery area</legend>
    {{range $d.Zones}}<label><span><input type="radio" name="zone_id" value="{{.ID}}"{{if eq .ID $f.ZoneID}} checked{{end}} hx-get="/checkout/totals" hx-trigger="change" hx-target="#totals" hx-swap="innerHTML">{{.Name}}</span><span class="price">{{taka .Fee}}</span></label>{{end}}
    {{with $f.Errors.zone_id}}<p class="err">{{.}}</p>{{end}}
  </fieldset>
  <div class="field"><label for="note">Note for us (optional)</label><textarea id="note" name="note">{{$f.Note}}</textarea>{{with $f.Errors.note}}<p class="err">{{.}}</p>{{end}}</div>
  <button class="btn" type="submit">Place order · pay cash on delivery</button>
</form>
<aside class="panel">
  <h2>Your order</h2>
  <div class="totals">{{range $d.Cart.Lines}}<span>{{.ProductName}} <span class="muted">{{.VariantName}} × {{.Qty}}</span></span><span class="mono">{{taka .LineTotal}}</span>{{end}}</div>
  <hr>
  <div id="totals">{{template "checkout_totals.html" $d.Totals}}</div>
  <p class="help"><a href="/cart">Edit cart</a></p>
</aside>
</div>
{{end}}
```

`avalonshop/templates/store/order.html`:

```html
{{define "content"}}{{$o := .Data}}
<p class="eyebrow">Order {{$o.Number}}</p>
<h1>{{if eq $o.Status "new"}}Thank you, {{$o.Name}}!{{else}}Order {{$o.Number}}{{end}}</h1>
<p>Status: <span class="status {{$o.Status}}">{{$o.Status}}</span> <span class="help">· placed {{dhaka $o.CreatedAt}}</span></p>
{{if eq $o.Status "new"}}<p>We'll call {{$o.Phone}} to confirm before delivery. Keep this page's link to check status.</p>{{end}}
<div class="cart-page">
  <div class="table-wrap"><table>
    <thead><tr><th>Item</th><th class="num">Qty</th><th class="num">Amount</th></tr></thead>
    <tbody>{{range $o.Items}}<tr><td>{{.ProductName}} <span class="muted">{{.VariantName}}</span></td><td class="num">{{.Qty}}</td><td class="num">{{taka (mul .UnitPrice .Qty)}}</td></tr>{{end}}</tbody>
  </table></div>
  <aside class="panel">
    <div class="totals"><span>Subtotal</span><span class="mono">{{taka $o.Subtotal}}</span><span>Delivery ({{$o.ZoneName}})</span><span class="mono">{{taka $o.DeliveryFee}}</span><span class="big">Total</span><span class="big price">{{taka $o.Total}}</span></div>
    <p class="help">Pay {{taka $o.Total}} in cash on delivery.</p>
    <h3>Deliver to</h3>
    <p class="desc">{{$o.Name}}
{{$o.Phone}}
{{$o.Address}}</p>
  </aside>
</div>
{{end}}
```

- [ ] **Step 6: Run to verify it passes**

Run: `TEST_DATABASE_URL=... go test ./internal/app/`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add avalonshop
git commit -m "feat(shop): checkout with zone fees, order page, confirmation emails"
```

---

### Task 14: Customer accounts (register, login, logout, reset, account page)

**Files:**
- Create: `avalonshop/internal/app/auth.go`, `avalonshop/internal/app/auth_test.go`, `avalonshop/templates/store/login.html`, `register.html`, `forgot.html`, `reset.html`, `account.html`
- Modify: `avalonshop/internal/app/app.go` (routes)

**Interfaces:**
- Consumes: `store.CreateUser`, `GetUserByEmail`, `GetUser`, `UpdateProfile`, `UpdatePassword`, `ListOrdersByUser`; `a.tok.ResetToken/ParseReset`; `a.login/logout`; `a.loginLimit`, `a.forgotLimit`; `mail.Send`.
- Produces: routes `GET|POST /login`, `GET|POST /register`, `POST /logout`, `GET|POST /forgot`, `GET|POST /reset/{token}`, `GET|POST /account`; `(*App) requireUser(h http.HandlerFunc) http.HandlerFunc` (redirects to `/login?next=`), `safeNext(s) string`. Admin login goes through the same `/login`; admins land on `/admin`.

- [ ] **Step 1: Write the failing tests**

`avalonshop/internal/app/auth_test.go`:

```go
package app

import (
	"bytes"
	"context"
	"log/slog"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"avalonshop/internal/mail"
	"avalonshop/internal/store"
)

func registerForm(email, name, pw string) string {
	return url.Values{"email": {email}, "name": {name}, "password": {pw}}.Encode()
}

func loginForm(email, pw string) string { return url.Values{"email": {email}, "password": {pw}}.Encode() }

func sessionCookie(t *testing.T, a *App, userID int64) string {
	t.Helper()
	return "sess=" + a.tok.EncodeSession(userID, time.Now().Add(sessionTTL))
}

func TestRegisterLoginLogout(t *testing.T) {
	a, _ := newDBApp(t)
	w := do(t, a, "POST", "/register", strings.NewReader(registerForm("Ana@Example.com", "Ana", "short")))
	if w.Code != 422 || !strings.Contains(w.Body.String(), "at least 8") {
		t.Fatalf("weak password: %d", w.Code)
	}
	w = do(t, a, "POST", "/register", strings.NewReader(registerForm("Ana@Example.com", "Ana", "correct horse")))
	if w.Code != 303 || w.Header().Get("Location") != "/account" || cookieHeader(w, "sess") == "" {
		t.Fatalf("register: %d %s", w.Code, w.Header().Get("Location"))
	}
	sess := cookieHeader(w, "sess")
	w = do(t, a, "GET", "/account", nil, "Cookie", "sess="+sess)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Ana") {
		t.Fatalf("account page: %d", w.Code)
	}
	w = do(t, a, "POST", "/register", strings.NewReader(registerForm("ana@example.com", "Dup", "correct horse")))
	if w.Code != 422 || !strings.Contains(w.Body.String(), "already registered") {
		t.Fatalf("duplicate: %d", w.Code)
	}
	w = do(t, a, "POST", "/login", strings.NewReader(loginForm("ana@example.com", "wrong password")))
	if w.Code != 401 || !strings.Contains(w.Body.String(), "Wrong email or password") {
		t.Fatalf("bad login: %d", w.Code)
	}
	w = do(t, a, "POST", "/login", strings.NewReader(loginForm("ANA@example.com", "correct horse")+"&next=/checkout"))
	if w.Code != 303 || w.Header().Get("Location") != "/checkout" || cookieHeader(w, "sess") == "" {
		t.Fatalf("good login: %d %s", w.Code, w.Header().Get("Location"))
	}
	w = do(t, a, "POST", "/login", strings.NewReader(loginForm("ana@example.com", "correct horse")+"&next=//evil.com"))
	if w.Header().Get("Location") != "/account" {
		t.Fatalf("open redirect: %s", w.Header().Get("Location"))
	}
	w = do(t, a, "POST", "/logout", nil, "Cookie", "sess="+sess)
	if w.Code != 303 {
		t.Fatal("logout")
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "sess" && c.MaxAge != -1 {
			t.Fatal("sess cookie not cleared")
		}
	}
	if w := do(t, a, "GET", "/account", nil); w.Code != 303 || w.Header().Get("Location") != "/login?next=%2Faccount" {
		t.Fatalf("logged-out account: %d %s", w.Code, w.Header().Get("Location"))
	}
}

func TestLoginRateLimitAndAdminRedirect(t *testing.T) {
	a, st := newDBApp(t)
	st.SeedAdmin(context.Background(), "admin@example.com", "admin-pass-123")
	for i := 0; i < 5; i++ {
		do(t, a, "POST", "/login", strings.NewReader(loginForm("admin@example.com", "nope")))
	}
	w := do(t, a, "POST", "/login", strings.NewReader(loginForm("admin@example.com", "admin-pass-123")))
	if w.Code != 429 {
		t.Fatalf("6th attempt should be blocked even with the right password, got %d", w.Code)
	}
	a.loginLimit = newLimiter(5, 15*time.Minute)
	w = do(t, a, "POST", "/login", strings.NewReader(loginForm("admin@example.com", "admin-pass-123")))
	if w.Code != 303 || w.Header().Get("Location") != "/admin" {
		t.Fatalf("admin should land on /admin: %d %s", w.Code, w.Header().Get("Location"))
	}
}

var resetRe = regexp.MustCompile(`/reset/([0-9]+\.[0-9]+\.[0-9a-f]+)`)

func TestForgotAndReset(t *testing.T) {
	a, _ := newDBApp(t)
	var logbuf bytes.Buffer
	m, _ := mail.New("", "587", "", "", "shop@test.local", os.DirFS("../.."), slog.New(slog.NewTextHandler(&logbuf, nil)))
	a.mail = m
	do(t, a, "POST", "/register", strings.NewReader(registerForm("ana@example.com", "Ana", "correct horse")))
	w := do(t, a, "POST", "/forgot", strings.NewReader("email=nobody@example.com"))
	if w.Code != 303 || cookieHeader(w, "flash") == "" {
		t.Fatalf("unknown email must look identical: %d", w.Code)
	}
	w = do(t, a, "POST", "/forgot", strings.NewReader("email=ANA@example.com"))
	if w.Code != 303 {
		t.Fatal("forgot")
	}
	a.mail.Wait()
	m2 := resetRe.FindStringSubmatch(logbuf.String())
	if m2 == nil {
		t.Fatalf("no reset link in dev mail log:\n%s", logbuf.String())
	}
	tok := m2[1]
	w = do(t, a, "GET", "/reset/"+tok, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "New password") {
		t.Fatalf("reset form: %d", w.Code)
	}
	w = do(t, a, "POST", "/reset/"+tok, strings.NewReader("password=new password 9"))
	if w.Code != 303 || cookieHeader(w, "sess") == "" {
		t.Fatalf("reset post: %d", w.Code)
	}
	if w := do(t, a, "GET", "/reset/"+tok, nil); w.Code != 200 || !strings.Contains(w.Body.String(), "expired") {
		t.Fatal("token should be dead after use")
	}
	w = do(t, a, "POST", "/login", strings.NewReader(loginForm("ana@example.com", "new password 9")))
	if w.Code != 303 {
		t.Fatal("login with new password failed")
	}
	if w := do(t, a, "GET", "/reset/garbage", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "expired") {
		t.Fatal("garbage token")
	}
}

func TestAccountProfileAndOrders(t *testing.T) {
	a, st := newDBApp(t)
	u, _ := st.CreateUser(context.Background(), "ana@example.com", "x", "Ana", "customer")
	sess := sessionCookie(t, a, u.ID)
	w := do(t, a, "POST", "/account", strings.NewReader(url.Values{"name": {"Ana B"}, "phone": {"+88 01712345678"}, "address": {"Road 1, Rajshahi"}}.Encode()), "Cookie", sess)
	if w.Code != 303 {
		t.Fatalf("profile save: %d", w.Code)
	}
	got, _ := st.GetUser(context.Background(), u.ID)
	if got.Name != "Ana B" || got.Phone != "01712345678" {
		t.Fatalf("profile not saved: %+v", got)
	}
	w = do(t, a, "POST", "/account", strings.NewReader(url.Values{"name": {"Ana"}, "phone": {"123"}, "address": {""}}.Encode()), "Cookie", sess)
	if w.Code != 422 {
		t.Fatalf("bad phone should 422, got %d", w.Code)
	}
	slug := seedCatalog(t, st)
	v1, _ := variantIDs(t, a, slug)
	zone, _ := st.CreateZone(context.Background(), store.Zone{Name: "Rajshahi", Fee: 60, Active: true})
	o, err := st.PlaceOrder(context.Background(), store.NewOrder{UserID: &u.ID, Name: "Ana", Phone: "01712345678", Email: "ana@example.com", Address: "Road 1", ZoneID: zone, Lines: []store.OrderLine{{VariantID: v1, Qty: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	w = do(t, a, "GET", "/account", nil, "Cookie", sess)
	if !strings.Contains(w.Body.String(), o.Number) {
		t.Fatal("order history missing")
	}
	if w := do(t, a, "GET", "/orders/"+o.Number, nil, "Cookie", sess); w.Code != 200 {
		t.Fatalf("owner should see order without token, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `TEST_DATABASE_URL=... go test ./internal/app/ -run 'Register|LoginRate|Forgot|AccountProfile'`
Expected: FAIL (404s and undefined helpers)

- [ ] **Step 3: Write auth.go**

```go
package app

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"avalonshop/internal/store"

	"golang.org/x/crypto/bcrypt"
)

const bcryptCost = 12

// dummyHash keeps login timing uniform when the email is unknown.
var dummyHash = sync.OnceValue(func() string {
	h, _ := bcrypt.GenerateFromPassword([]byte("avalon-dummy-password"), bcryptCost)
	return string(h)
})

type authForm struct {
	Email, Name, Next string
	Errors            map[string]string
	Invalid           bool // reset: token bad or expired
}

// safeNext only allows same-site relative paths, blocking //evil.com redirects.
func safeNext(s string) string {
	if strings.HasPrefix(s, "/") && !strings.HasPrefix(s, "//") && !strings.HasPrefix(s, "/\\") {
		return s
	}
	return ""
}

func (a *App) requireUser(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.currentUser(r) == nil {
			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
			return
		}
		h(w, r)
	}
}

func (a *App) authPage(w http.ResponseWriter, r *http.Request, status int, name, title string, f authForm) {
	a.renderStatus(w, r, status, "store/"+name+".html", page{Title: title, NoIndex: true, Data: f})
}

func (a *App) loginGet(w http.ResponseWriter, r *http.Request) {
	if a.currentUser(r) != nil {
		http.Redirect(w, r, "/account", http.StatusSeeOther)
		return
	}
	a.authPage(w, r, http.StatusOK, "login", "Log in", authForm{Next: safeNext(r.URL.Query().Get("next")), Errors: map[string]string{}})
}

func (a *App) loginPost(w http.ResponseWriter, r *http.Request) {
	f := authForm{Email: strings.TrimSpace(strings.ToLower(r.FormValue("email"))), Next: safeNext(r.FormValue("next")), Errors: map[string]string{}}
	key := clientIP(r) + "|" + f.Email
	if a.loginLimit.Blocked(key) {
		f.Errors["form"] = "Too many attempts. Please try again in 15 minutes."
		a.authPage(w, r, http.StatusTooManyRequests, "login", "Log in", f)
		return
	}
	u, err := a.st.GetUserByEmail(r.Context(), f.Email)
	hash := dummyHash()
	if err == nil {
		hash = u.PasswordHash
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(r.FormValue("password"))) != nil || err != nil {
		a.loginLimit.Hit(key)
		f.Errors["form"] = "Wrong email or password."
		a.authPage(w, r, http.StatusUnauthorized, "login", "Log in", f)
		return
	}
	a.login(w, u.ID)
	dest := f.Next
	if dest == "" {
		dest = "/account"
		if u.Role == "admin" {
			dest = "/admin"
		}
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

func (a *App) logoutPost(w http.ResponseWriter, r *http.Request) {
	a.logout(w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *App) registerGet(w http.ResponseWriter, r *http.Request) {
	a.authPage(w, r, http.StatusOK, "register", "Create account", authForm{Errors: map[string]string{}})
}

func validPassword(pw string) bool { return utf8.RuneCountInString(pw) >= 8 && len(pw) <= 200 }

func (a *App) registerPost(w http.ResponseWriter, r *http.Request) {
	f := authForm{Email: strings.TrimSpace(strings.ToLower(r.FormValue("email"))), Name: strings.TrimSpace(r.FormValue("name")), Errors: map[string]string{}}
	pw := r.FormValue("password")
	if l := utf8.RuneCountInString(f.Name); l < 1 || l > 100 {
		f.Errors["name"] = "Please enter your name."
	}
	if !validEmail(f.Email) {
		f.Errors["email"] = "Enter a valid email address."
	}
	if !validPassword(pw) {
		f.Errors["password"] = "Use at least 8 characters."
	}
	if len(f.Errors) > 0 {
		a.authPage(w, r, http.StatusUnprocessableEntity, "register", "Create account", f)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcryptCost)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	u, err := a.st.CreateUser(r.Context(), f.Email, string(hash), f.Name, "customer")
	if errors.Is(err, store.ErrDuplicate) {
		f.Errors["email"] = "That email is already registered. Try logging in."
		a.authPage(w, r, http.StatusUnprocessableEntity, "register", "Create account", f)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.login(w, u.ID)
	a.setFlash(w, "Welcome to Avalon!")
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}

func (a *App) forgotGet(w http.ResponseWriter, r *http.Request) {
	a.authPage(w, r, http.StatusOK, "forgot", "Reset password", authForm{Errors: map[string]string{}})
}

func (a *App) forgotPost(w http.ResponseWriter, r *http.Request) {
	if !a.forgotLimit.Allow(clientIP(r)) {
		f := authForm{Errors: map[string]string{"form": "Too many attempts. Please try again in 15 minutes."}}
		a.authPage(w, r, http.StatusTooManyRequests, "forgot", "Reset password", f)
		return
	}
	email := strings.TrimSpace(strings.ToLower(r.FormValue("email")))
	if u, err := a.st.GetUserByEmail(r.Context(), email); err == nil {
		tok := a.tok.ResetToken(u.ID, time.Now().Add(time.Hour), u.PasswordHash)
		a.mail.Send(u.Email, "password_reset", map[string]any{"Name": u.Name, "ResetURL": a.cfg.BaseURL + "/reset/" + tok})
	}
	a.setFlash(w, "If that email is registered, we've sent a reset link. Check your inbox.")
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *App) lookupHash(ctx context.Context) func(int64) (string, bool) {
	return func(id int64) (string, bool) {
		u, err := a.st.GetUser(ctx, id)
		if err != nil {
			return "", false
		}
		return u.PasswordHash, true
	}
}

func (a *App) resetGet(w http.ResponseWriter, r *http.Request) {
	_, ok := a.tok.ParseReset(r.PathValue("token"), time.Now(), a.lookupHash(r.Context()))
	a.authPage(w, r, http.StatusOK, "reset", "Choose a new password", authForm{Invalid: !ok, Errors: map[string]string{}})
}

func (a *App) resetPost(w http.ResponseWriter, r *http.Request) {
	id, ok := a.tok.ParseReset(r.PathValue("token"), time.Now(), a.lookupHash(r.Context()))
	if !ok {
		a.authPage(w, r, http.StatusOK, "reset", "Choose a new password", authForm{Invalid: true, Errors: map[string]string{}})
		return
	}
	pw := r.FormValue("password")
	if !validPassword(pw) {
		a.authPage(w, r, http.StatusUnprocessableEntity, "reset", "Choose a new password", authForm{Errors: map[string]string{"password": "Use at least 8 characters."}})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcryptCost)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := a.st.UpdatePassword(r.Context(), id, string(hash)); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.login(w, id)
	a.setFlash(w, "Password updated.")
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}

type accountData struct {
	User   *store.User
	Orders []store.Order
	Form   map[string]string
	Errors map[string]string
}

func (a *App) accountGet(w http.ResponseWriter, r *http.Request) {
	u := a.currentUser(r)
	orders, err := a.st.ListOrdersByUser(r.Context(), u.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, "store/account.html", page{Title: "Your account", NoIndex: true, Data: accountData{
		User: u, Orders: orders, Form: map[string]string{"name": u.Name, "phone": u.Phone, "address": u.Address}, Errors: map[string]string{},
	}})
}

func (a *App) accountPost(w http.ResponseWriter, r *http.Request) {
	u := a.currentUser(r)
	form := map[string]string{"name": strings.TrimSpace(r.FormValue("name")), "phone": strings.TrimSpace(r.FormValue("phone")), "address": strings.TrimSpace(r.FormValue("address"))}
	errs := map[string]string{}
	if l := utf8.RuneCountInString(form["name"]); l < 1 || l > 100 {
		errs["name"] = "Please enter your name."
	}
	if form["phone"] != "" {
		if p, ok := normalizePhone(form["phone"]); ok {
			form["phone"] = p
		} else {
			errs["phone"] = "Enter a valid Bangladeshi mobile number."
		}
	}
	if utf8.RuneCountInString(form["address"]) > 500 {
		errs["address"] = "Address is too long."
	}
	if len(errs) > 0 {
		orders, _ := a.st.ListOrdersByUser(r.Context(), u.ID)
		a.renderStatus(w, r, http.StatusUnprocessableEntity, "store/account.html", page{Title: "Your account", NoIndex: true, Data: accountData{User: u, Orders: orders, Form: form, Errors: errs}})
		return
	}
	if err := a.st.UpdateProfile(r.Context(), u.ID, form["name"], form["phone"], form["address"]); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.setFlash(w, "Saved.")
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}
```

- [ ] **Step 4: Register the routes**

In `routes()`, above the `/` fallback:

```go
	m.HandleFunc("GET /login", a.loginGet)
	m.HandleFunc("POST /login", a.loginPost)
	m.HandleFunc("POST /logout", a.logoutPost)
	m.HandleFunc("GET /register", a.registerGet)
	m.HandleFunc("POST /register", a.registerPost)
	m.HandleFunc("GET /forgot", a.forgotGet)
	m.HandleFunc("POST /forgot", a.forgotPost)
	m.HandleFunc("GET /reset/{token}", a.resetGet)
	m.HandleFunc("POST /reset/{token}", a.resetPost)
	m.HandleFunc("GET /account", a.requireUser(a.accountGet))
	m.HandleFunc("POST /account", a.requireUser(a.accountPost))
```

- [ ] **Step 5: Write the templates**

`avalonshop/templates/store/login.html`:

```html
{{define "content"}}{{$f := .Data}}
<p class="eyebrow">Account</p>
<h1>Log in</h1>
{{with $f.Errors.form}}<div class="errbox" role="alert">{{.}}</div>{{end}}
<form class="form" method="post" action="/login">
  <input type="hidden" name="next" value="{{$f.Next}}">
  <div class="field"><label for="email">Email</label><input id="email" type="email" name="email" value="{{$f.Email}}" required autocomplete="email"></div>
  <div class="field"><label for="password">Password</label><input id="password" type="password" name="password" required autocomplete="current-password"></div>
  <button class="btn" type="submit">Log in</button>
</form>
<p class="help"><a href="/forgot">Forgot your password?</a> · New here? <a href="/register">Create an account</a></p>
{{end}}
```

`avalonshop/templates/store/register.html`:

```html
{{define "content"}}{{$f := .Data}}
<p class="eyebrow">Account</p>
<h1>Create an account</h1>
<p class="help">Optional. It saves your address and shows your order history. You can always check out as a guest.</p>
<form class="form" method="post" action="/register">
  <div class="field"><label for="name">Name</label><input id="name" type="text" name="name" value="{{$f.Name}}" required autocomplete="name">{{with $f.Errors.name}}<p class="err">{{.}}</p>{{end}}</div>
  <div class="field"><label for="email">Email</label><input id="email" type="email" name="email" value="{{$f.Email}}" required autocomplete="email">{{with $f.Errors.email}}<p class="err">{{.}}</p>{{end}}</div>
  <div class="field"><label for="password">Password</label><input id="password" type="password" name="password" required minlength="8" autocomplete="new-password">{{with $f.Errors.password}}<p class="err">{{.}}</p>{{end}}</div>
  <button class="btn" type="submit">Create account</button>
</form>
<p class="help">Already have one? <a href="/login">Log in</a></p>
{{end}}
```

`avalonshop/templates/store/forgot.html`:

```html
{{define "content"}}{{$f := .Data}}
<p class="eyebrow">Account</p>
<h1>Reset your password</h1>
{{with $f.Errors.form}}<div class="errbox" role="alert">{{.}}</div>{{end}}
<form class="form" method="post" action="/forgot">
  <div class="field"><label for="email">Email</label><input id="email" type="email" name="email" required autocomplete="email"></div>
  <button class="btn" type="submit">Send reset link</button>
</form>
{{end}}
```

`avalonshop/templates/store/reset.html`:

```html
{{define "content"}}{{$f := .Data}}
<p class="eyebrow">Account</p>
<h1>Choose a new password</h1>
{{if $f.Invalid}}
<div class="errbox" role="alert">This reset link is invalid or has expired.</div>
<p><a class="btn ghost" href="/forgot">Request a new link</a></p>
{{else}}
<form class="form" method="post">
  <div class="field"><label for="password">New password</label><input id="password" type="password" name="password" required minlength="8" autocomplete="new-password">{{with $f.Errors.password}}<p class="err">{{.}}</p>{{end}}</div>
  <button class="btn" type="submit">Save password</button>
</form>
{{end}}
{{end}}
```

`avalonshop/templates/store/account.html`:

```html
{{define "content"}}{{$d := .Data}}
<p class="eyebrow">Account</p>
<h1>Hi, {{$d.User.Name}}</h1>
<div class="cart-page">
  <div>
    <h2>Your orders</h2>
    {{if $d.Orders}}<div class="table-wrap"><table>
      <thead><tr><th>Order</th><th>Date</th><th>Status</th><th class="num">Total</th></tr></thead>
      <tbody>{{range $d.Orders}}<tr><td><a href="/orders/{{.Number}}">{{.Number}}</a></td><td>{{dhaka .CreatedAt}}</td><td><span class="status {{.Status}}">{{.Status}}</span></td><td class="num">{{taka .Total}}</td></tr>{{end}}</tbody>
    </table></div>
    {{else}}<p class="empty">No orders yet. <a href="/products">Start shopping</a></p>{{end}}
  </div>
  <aside class="panel">
    <h2>Delivery details</h2>
    <form method="post" action="/account">
      <div class="field"><label for="name">Name</label><input id="name" type="text" name="name" value="{{index $d.Form "name"}}" required>{{with index $d.Errors "name"}}<p class="err">{{.}}</p>{{end}}</div>
      <div class="field"><label for="phone">Mobile</label><input id="phone" type="tel" name="phone" value="{{index $d.Form "phone"}}">{{with index $d.Errors "phone"}}<p class="err">{{.}}</p>{{end}}</div>
      <div class="field"><label for="address">Address</label><textarea id="address" name="address">{{index $d.Form "address"}}</textarea>{{with index $d.Errors "address"}}<p class="err">{{.}}</p>{{end}}</div>
      <button class="btn" type="submit">Save</button>
    </form>
    <form method="post" action="/logout"><button class="btn ghost sm" type="submit">Log out</button></form>
  </aside>
</div>
{{end}}
```

- [ ] **Step 6: Run to verify it passes**

Run: `TEST_DATABASE_URL=... go test ./internal/app/`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add avalonshop
git commit -m "feat(shop): customer accounts with login, registration, password reset"
```

---

### Task 15: Sitemap and robots

**Files:**
- Create: `avalonshop/internal/app/sitemap.go`, `avalonshop/internal/app/sitemap_test.go`
- Modify: `avalonshop/internal/app/app.go` (routes)

**Interfaces:**
- Consumes: `store.ActiveProductsForSitemap`, `store.ListCategories`.
- Produces: `GET /sitemap.xml` (`application/xml`, `Cache-Control: public, max-age=3600`), `GET /robots.txt`.

- [ ] **Step 1: Write the failing test**

`avalonshop/internal/app/sitemap_test.go`:

```go
package app

import (
	"strings"
	"testing"
)

func TestSitemapAndRobots(t *testing.T) {
	a, st := newDBApp(t)
	seedCatalog(t, st)
	w := do(t, a, "GET", "/sitemap.xml", nil)
	body := w.Body.String()
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/xml") || w.Header().Get("Cache-Control") != "public, max-age=3600" {
		t.Fatalf("sitemap headers: %d %v", w.Code, w.Header())
	}
	for _, want := range []string{
		`<loc>http://localhost:8080/</loc>`,
		`<loc>http://localhost:8080/products</loc>`,
		`<loc>http://localhost:8080/products?category=honey</loc>`,
		`<loc>http://localhost:8080/products/wild-forest-honey</loc>`,
		`<lastmod>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("sitemap missing %q\n%s", want, body)
		}
	}
	if strings.Contains(body, "hidden") {
		t.Error("inactive product in sitemap")
	}
	w = do(t, a, "GET", "/robots.txt", nil)
	body = w.Body.String()
	if w.Code != 200 || !strings.Contains(body, "Disallow: /admin") || !strings.Contains(body, "Sitemap: http://localhost:8080/sitemap.xml") {
		t.Fatalf("robots: %d\n%s", w.Code, body)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `TEST_DATABASE_URL=... go test ./internal/app/ -run Sitemap`
Expected: FAIL (404)

- [ ] **Step 3: Write sitemap.go**

```go
package app

import (
	"encoding/xml"
	"net/http"
	"strings"
	"time"
)

func (a *App) sitemap(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	prods, err := a.st.ActiveProductsForSitemap(ctx)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	cats, err := a.st.ListCategories(ctx)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	add := func(loc string, lastmod time.Time) {
		b.WriteString("<url><loc>")
		xml.EscapeText(&b, []byte(loc))
		b.WriteString("</loc>")
		if !lastmod.IsZero() {
			b.WriteString("<lastmod>" + lastmod.UTC().Format("2006-01-02") + "</lastmod>")
		}
		b.WriteString("</url>\n")
	}
	base := a.cfg.BaseURL
	add(base+"/", time.Time{})
	add(base+"/products", time.Time{})
	for _, c := range cats {
		add(base+"/products?category="+c.Slug, time.Time{})
	}
	for _, p := range prods {
		add(base+"/products/"+p.Slug, p.UpdatedAt)
	}
	b.WriteString("</urlset>\n")
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write([]byte(b.String()))
}

func (a *App) robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write([]byte("User-agent: *\nDisallow: /admin\nDisallow: /cart\nDisallow: /checkout\nDisallow: /account\nDisallow: /orders\nDisallow: /login\nDisallow: /register\nDisallow: /forgot\nDisallow: /reset\nSitemap: " + a.cfg.BaseURL + "/sitemap.xml\n"))
}
```

- [ ] **Step 4: Register the routes**

In `routes()`, above the `/` fallback:

```go
	m.HandleFunc("GET /sitemap.xml", a.sitemap)
	m.HandleFunc("GET /robots.txt", a.robots)
```

- [ ] **Step 5: Run to verify it passes, then commit**

Run: `TEST_DATABASE_URL=... go test ./internal/app/`
Expected: PASS

```bash
git add avalonshop
git commit -m "feat(shop): sitemap.xml and robots.txt"
```

---

### Task 16: Admin core: access control, dashboard, categories, zones

**Files:**
- Create: `avalonshop/internal/app/admin.go`, `avalonshop/internal/app/admin_test.go`, `avalonshop/templates/admin/dashboard.html`, `avalonshop/templates/admin/categories.html`, `avalonshop/templates/admin/zones.html`, `avalonshop/templates/partials/order_rows.html`
- Modify: `avalonshop/internal/app/app.go` (routes)

**Interfaces:**
- Consumes: `store.Dashboard`, category and zone CRUD, `store.Slugify`, `store.UniqueSlug`.
- Produces: `(*App) requireAdmin(h http.HandlerFunc) http.HandlerFunc` (404 for anyone who is not `role = admin`), routes `GET /admin`, `GET /admin/categories`, `POST /admin/categories`, `POST /admin/categories/{id}`, `POST /admin/categories/{id}/delete`, `GET /admin/zones`, `POST /admin/zones`, `POST /admin/zones/{id}`, `POST /admin/zones/{id}/delete`; partial `order_rows.html` (takes `[]store.Order`), reused by Task 18; test helper `adminSession(t, a, st) string` (cookie header for a seeded admin).

- [ ] **Step 1: Write the failing tests**

`avalonshop/internal/app/admin_test.go`:

```go
package app

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"avalonshop/internal/store"
)

func adminSession(t *testing.T, a *App, st *store.Store) string {
	t.Helper()
	ctx := context.Background()
	if err := st.SeedAdmin(ctx, "admin@example.com", "admin-pass-123"); err != nil {
		t.Fatal(err)
	}
	u, err := st.GetUserByEmail(ctx, "admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	return sessionCookie(t, a, u.ID)
}

func TestAdminAccess(t *testing.T) {
	a, st := newDBApp(t)
	if w := do(t, a, "GET", "/admin", nil); w.Code != 404 {
		t.Fatalf("logged out should 404, got %d", w.Code)
	}
	cust, _ := st.CreateUser(context.Background(), "c@example.com", "x", "C", "customer")
	if w := do(t, a, "GET", "/admin", nil, "Cookie", sessionCookie(t, a, cust.ID)); w.Code != 404 {
		t.Fatalf("customer should 404, got %d", w.Code)
	}
	adm := adminSession(t, a, st)
	w := do(t, a, "GET", "/admin", nil, "Cookie", adm)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Dashboard") || !strings.Contains(w.Body.String(), `content="noindex"`) {
		t.Fatalf("admin dashboard: %d", w.Code)
	}
}

func TestAdminCategories(t *testing.T) {
	a, st := newDBApp(t)
	adm := adminSession(t, a, st)
	ctx := context.Background()
	w := do(t, a, "POST", "/admin/categories", strings.NewReader(url.Values{"name": {"Wild Honey"}, "sort": {"2"}}.Encode()), "Cookie", adm)
	if w.Code != 303 {
		t.Fatalf("create: %d", w.Code)
	}
	c, err := st.GetCategoryBySlug(ctx, "wild-honey")
	if err != nil || c.Sort != 2 {
		t.Fatalf("category not created: %v", err)
	}
	do(t, a, "POST", "/admin/categories", strings.NewReader(url.Values{"name": {"Wild Honey"}}.Encode()), "Cookie", adm)
	if _, err := st.GetCategoryBySlug(ctx, "wild-honey-2"); err != nil {
		t.Fatal("duplicate name should get -2 slug")
	}
	w = do(t, a, "POST", "/admin/categories/"+itoa(c.ID), strings.NewReader(url.Values{"name": {"Honey"}, "slug": {"Honey!"}, "sort": {"1"}}.Encode()), "Cookie", adm)
	if w.Code != 303 {
		t.Fatalf("update: %d", w.Code)
	}
	c2, _ := st.GetCategory(ctx, c.ID)
	if c2.Name != "Honey" || c2.Slug != "honey" || c2.Sort != 1 {
		t.Fatalf("update lost: %+v", c2)
	}
	w = do(t, a, "GET", "/admin/categories", nil, "Cookie", adm)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `value="Honey"`) {
		t.Fatalf("list: %d", w.Code)
	}
	st.CreateProduct(ctx, store.Product{Slug: "p", Name: "P", CategoryID: &c.ID, Active: true}, nil)
	w = do(t, a, "POST", "/admin/categories/"+itoa(c.ID)+"/delete", nil, "Cookie", adm)
	if w.Code != 303 || !strings.Contains(cookieHeader(w, "flash"), "products") {
		t.Fatalf("in-use delete should flash: %d %q", w.Code, cookieHeader(w, "flash"))
	}
	c3, _ := st.GetCategoryBySlug(ctx, "wild-honey-2")
	do(t, a, "POST", "/admin/categories/"+itoa(c3.ID)+"/delete", nil, "Cookie", adm)
	if _, err := st.GetCategory(ctx, c3.ID); err == nil {
		t.Fatal("category not deleted")
	}
}

func TestAdminZones(t *testing.T) {
	a, st := newDBApp(t)
	adm := adminSession(t, a, st)
	ctx := context.Background()
	w := do(t, a, "POST", "/admin/zones", strings.NewReader(url.Values{"name": {"Rajshahi"}, "fee": {"60"}, "active": {"on"}, "sort": {"1"}}.Encode()), "Cookie", adm)
	if w.Code != 303 {
		t.Fatalf("create: %d", w.Code)
	}
	zones, _ := st.ListZones(ctx, true)
	if len(zones) != 1 || zones[0].Fee != 60 {
		t.Fatalf("zone: %+v", zones)
	}
	z := zones[0]
	w = do(t, a, "POST", "/admin/zones/"+itoa(z.ID), strings.NewReader(url.Values{"name": {"Rajshahi City"}, "fee": {"70"}, "sort": {"1"}}.Encode()), "Cookie", adm)
	z2, _ := st.GetZone(ctx, z.ID)
	if w.Code != 303 || z2.Fee != 70 || z2.Active {
		t.Fatalf("update (unchecked active must become false): %d %+v", w.Code, z2)
	}
	w = do(t, a, "POST", "/admin/zones", strings.NewReader(url.Values{"name": {"X"}, "fee": {"-5"}}.Encode()), "Cookie", adm)
	if w.Code != 303 || !strings.Contains(cookieHeader(w, "flash"), "fee") {
		t.Fatalf("negative fee should be rejected with a flash: %d", w.Code)
	}
	do(t, a, "POST", "/admin/zones/"+itoa(z.ID)+"/delete", nil, "Cookie", adm)
	if _, err := st.GetZone(ctx, z.ID); err == nil {
		t.Fatal("zone not deleted")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `TEST_DATABASE_URL=... go test ./internal/app/ -run Admin`
Expected: FAIL (404 for admin routes even as admin)

- [ ] **Step 3: Write admin.go**

```go
package app

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"avalonshop/internal/store"
)

// requireAdmin hides admin routes from everyone else with a 404.
func (a *App) requireAdmin(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := a.currentUser(r)
		if u == nil || u.Role != "admin" {
			a.notFound(w, r)
			return
		}
		h(w, r)
	}
}

func (a *App) adminDashboard(w http.ResponseWriter, r *http.Request) {
	d, err := a.st.Dashboard(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, "admin/dashboard.html", page{Title: "Dashboard", NoIndex: true, Data: d})
}

func pathID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id
}

func formInt(r *http.Request, name string) (int, bool) {
	v := strings.TrimSpace(r.FormValue(name))
	if v == "" {
		return 0, true
	}
	n, err := strconv.Atoi(v)
	return n, err == nil
}

// flashBack sets a flash and redirects; the one-liner every admin POST ends with.
func (a *App) flashBack(w http.ResponseWriter, r *http.Request, msg, to string) {
	a.setFlash(w, msg)
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// ---- categories ----

func (a *App) adminCategories(w http.ResponseWriter, r *http.Request) {
	cats, err := a.st.ListCategories(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, "admin/categories.html", page{Title: "Categories", NoIndex: true, Data: cats})
}

func (a *App) categoryFromForm(r *http.Request, excludeID int64) (store.Category, string) {
	c := store.Category{ID: excludeID, Name: strings.TrimSpace(r.FormValue("name"))}
	if c.Name == "" {
		return c, "Name is required."
	}
	slug := strings.TrimSpace(r.FormValue("slug"))
	if slug == "" {
		slug = c.Name
	}
	var err error
	if c.Slug, err = a.st.UniqueSlug(r.Context(), "categories", store.Slugify(slug), excludeID); err != nil {
		return c, "Could not generate a slug."
	}
	sort, ok := formInt(r, "sort")
	if !ok {
		return c, "Sort must be a whole number."
	}
	c.Sort = sort
	return c, ""
}

func (a *App) adminCategoryCreate(w http.ResponseWriter, r *http.Request) {
	c, msg := a.categoryFromForm(r, 0)
	if msg == "" {
		if _, err := a.st.CreateCategory(r.Context(), c); err != nil {
			msg = "Could not save: " + err.Error()
		} else {
			msg = "Category added."
		}
	}
	a.flashBack(w, r, msg, "/admin/categories")
}

func (a *App) adminCategoryUpdate(w http.ResponseWriter, r *http.Request) {
	c, msg := a.categoryFromForm(r, pathID(r))
	if msg == "" {
		if err := a.st.UpdateCategory(r.Context(), c); err != nil {
			msg = "Could not save: " + err.Error()
		} else {
			msg = "Saved."
		}
	}
	a.flashBack(w, r, msg, "/admin/categories")
}

func (a *App) adminCategoryDelete(w http.ResponseWriter, r *http.Request) {
	err := a.st.DeleteCategory(r.Context(), pathID(r))
	switch {
	case errors.Is(err, store.ErrInUse):
		a.flashBack(w, r, "That category still has products. Move them first.", "/admin/categories")
	case err != nil:
		a.flashBack(w, r, "Could not delete: "+err.Error(), "/admin/categories")
	default:
		a.flashBack(w, r, "Category deleted.", "/admin/categories")
	}
}

// ---- zones ----

func (a *App) adminZones(w http.ResponseWriter, r *http.Request) {
	zones, err := a.st.ListZones(r.Context(), false)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, "admin/zones.html", page{Title: "Delivery zones", NoIndex: true, Data: zones})
}

func zoneFromForm(r *http.Request, id int64) (store.Zone, string) {
	z := store.Zone{ID: id, Name: strings.TrimSpace(r.FormValue("name")), Active: r.FormValue("active") == "on"}
	if z.Name == "" {
		return z, "Name is required."
	}
	fee, ok := formInt(r, "fee")
	if !ok || fee < 0 {
		return z, "Delivery fee must be a whole number of taka, 0 or more."
	}
	sort, ok := formInt(r, "sort")
	if !ok {
		return z, "Sort must be a whole number."
	}
	z.Fee, z.Sort = fee, sort
	return z, ""
}

func (a *App) adminZoneCreate(w http.ResponseWriter, r *http.Request) {
	z, msg := zoneFromForm(r, 0)
	if msg == "" {
		if _, err := a.st.CreateZone(r.Context(), z); err != nil {
			msg = "Could not save: " + err.Error()
		} else {
			msg = "Zone added."
		}
	}
	a.flashBack(w, r, msg, "/admin/zones")
}

func (a *App) adminZoneUpdate(w http.ResponseWriter, r *http.Request) {
	z, msg := zoneFromForm(r, pathID(r))
	if msg == "" {
		if err := a.st.UpdateZone(r.Context(), z); err != nil {
			msg = "Could not save: " + err.Error()
		} else {
			msg = "Saved."
		}
	}
	a.flashBack(w, r, msg, "/admin/zones")
}

func (a *App) adminZoneDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.st.DeleteZone(r.Context(), pathID(r)); err != nil {
		a.flashBack(w, r, "Could not delete: "+err.Error(), "/admin/zones")
		return
	}
	a.flashBack(w, r, "Zone deleted.", "/admin/zones")
}
```

- [ ] **Step 4: Register the routes**

In `routes()`, above the `/` fallback:

```go
	adm := a.requireAdmin
	m.HandleFunc("GET /admin", adm(a.adminDashboard))
	m.HandleFunc("GET /admin/categories", adm(a.adminCategories))
	m.HandleFunc("POST /admin/categories", adm(a.adminCategoryCreate))
	m.HandleFunc("POST /admin/categories/{id}", adm(a.adminCategoryUpdate))
	m.HandleFunc("POST /admin/categories/{id}/delete", adm(a.adminCategoryDelete))
	m.HandleFunc("GET /admin/zones", adm(a.adminZones))
	m.HandleFunc("POST /admin/zones", adm(a.adminZoneCreate))
	m.HandleFunc("POST /admin/zones/{id}", adm(a.adminZoneUpdate))
	m.HandleFunc("POST /admin/zones/{id}/delete", adm(a.adminZoneDelete))
```

- [ ] **Step 5: Write the templates**

`avalonshop/templates/partials/order_rows.html`:

```html
<div class="table-wrap"><table>
<thead><tr><th>Order</th><th>Customer</th><th>Zone</th><th class="num">Total</th><th>Status</th><th>Placed</th></tr></thead>
<tbody>{{range .}}<tr>
  <td><a href="/admin/orders/{{.ID}}">{{.Number}}</a></td>
  <td>{{.Name}}<br><span class="muted mono">{{.Phone}}</span></td>
  <td>{{.ZoneName}}</td>
  <td class="num">{{taka .Total}}</td>
  <td><span class="status {{.Status}}">{{.Status}}</span></td>
  <td>{{dhaka .CreatedAt}}</td>
</tr>{{else}}<tr><td colspan="6" class="empty">No orders.</td></tr>{{end}}</tbody>
</table></div>
```

`avalonshop/templates/admin/dashboard.html`:

```html
{{define "content"}}{{$d := .Data}}
<h1>Dashboard</h1>
<div class="stats">
  <div class="stat"><div class="n">{{$d.NewOrders}}</div><div class="muted">New orders</div><a href="/admin/orders?status=new">Review</a></div>
  <div class="stat"><div class="n">{{len $d.LowStock}}</div><div class="muted">Low-stock variants (≤ 5)</div></div>
</div>
{{if $d.LowStock}}
<h2>Low stock</h2>
<div class="table-wrap"><table><thead><tr><th>Product</th><th>Variant</th><th class="num">Stock</th></tr></thead>
<tbody>{{range $d.LowStock}}<tr><td>{{.ProductName}}</td><td>{{.VariantName}}</td><td class="num">{{.Stock}}</td></tr>{{end}}</tbody></table></div>
{{end}}
<h2>Recent orders</h2>
{{template "order_rows.html" $d.Recent}}
{{end}}
```

`avalonshop/templates/admin/categories.html`:

```html
{{define "content"}}
<h1>Categories</h1>
<form class="form" method="post" action="/admin/categories">
  <div class="row">
    <div class="field"><label for="new-name">New category</label><input id="new-name" type="text" name="name" required placeholder="e.g. Honey"></div>
    <div class="field"><label for="new-sort">Sort</label><input id="new-sort" type="number" name="sort" value="0"></div>
  </div>
  <button class="btn sm" type="submit">Add category</button>
</form>
<div class="table-wrap"><table>
<thead><tr><th>Name</th><th>Slug</th><th>Sort</th><th></th></tr></thead>
<tbody>{{range .Data}}<tr>
  <td><input form="c{{.ID}}" type="text" name="name" value="{{.Name}}" aria-label="Name"></td>
  <td><input form="c{{.ID}}" type="text" name="slug" value="{{.Slug}}" aria-label="Slug"></td>
  <td><input form="c{{.ID}}" type="number" name="sort" value="{{.Sort}}" aria-label="Sort"></td>
  <td><button form="c{{.ID}}" class="btn sm" type="submit">Save</button> <button form="d{{.ID}}" class="btn sm ghost" type="submit">Delete</button></td>
</tr>{{else}}<tr><td colspan="4" class="empty">No categories yet.</td></tr>{{end}}</tbody>
</table></div>
{{range .Data}}<form id="c{{.ID}}" method="post" action="/admin/categories/{{.ID}}"></form><form id="d{{.ID}}" method="post" action="/admin/categories/{{.ID}}/delete" hx-post="/admin/categories/{{.ID}}/delete" hx-confirm="Delete category “{{.Name}}”?"></form>{{end}}
{{end}}
```

`avalonshop/templates/admin/zones.html`:

```html
{{define "content"}}
<h1>Delivery zones</h1>
<p class="help">Customers pick one at checkout. Inactive zones are hidden but kept for old orders.</p>
<form class="form" method="post" action="/admin/zones">
  <div class="row">
    <div class="field"><label for="new-name">New zone</label><input id="new-name" type="text" name="name" required placeholder="e.g. Inside Rajshahi"></div>
    <div class="field"><label for="new-fee">Fee (৳)</label><input id="new-fee" type="number" name="fee" min="0" value="0" required></div>
  </div>
  <div class="field"><label class="check"><input type="checkbox" name="active" checked> Active</label></div>
  <button class="btn sm" type="submit">Add zone</button>
</form>
<div class="table-wrap"><table>
<thead><tr><th>Name</th><th>Fee</th><th>Active</th><th>Sort</th><th></th></tr></thead>
<tbody>{{range .Data}}<tr>
  <td><input form="z{{.ID}}" type="text" name="name" value="{{.Name}}" aria-label="Name"></td>
  <td><input form="z{{.ID}}" type="number" name="fee" min="0" value="{{.Fee}}" aria-label="Fee"></td>
  <td><input form="z{{.ID}}" type="checkbox" name="active"{{if .Active}} checked{{end}} aria-label="Active"></td>
  <td><input form="z{{.ID}}" type="number" name="sort" value="{{.Sort}}" aria-label="Sort"></td>
  <td><button form="z{{.ID}}" class="btn sm" type="submit">Save</button> <button form="dz{{.ID}}" class="btn sm ghost" type="submit">Delete</button></td>
</tr>{{else}}<tr><td colspan="5" class="empty">No zones yet. Add one before customers can check out.</td></tr>{{end}}</tbody>
</table></div>
{{range .Data}}<form id="z{{.ID}}" method="post" action="/admin/zones/{{.ID}}"></form><form id="dz{{.ID}}" method="post" action="/admin/zones/{{.ID}}/delete" hx-post="/admin/zones/{{.ID}}/delete" hx-confirm="Delete zone “{{.Name}}”?"></form>{{end}}
{{end}}
```

- [ ] **Step 6: Run to verify it passes, then commit**

Run: `TEST_DATABASE_URL=... go test ./internal/app/`
Expected: PASS

```bash
git add avalonshop
git commit -m "feat(shop): admin access control, dashboard, categories, delivery zones"
```

---

### Task 17: Admin products: form with variants, image upload and ordering

**Files:**
- Create: `avalonshop/internal/app/admin_products.go`, `avalonshop/internal/app/admin_products_test.go`, `avalonshop/templates/admin/products.html`, `avalonshop/templates/admin/product_form.html`, `avalonshop/templates/partials/variant_row.html`, `avalonshop/templates/partials/image_list.html`
- Modify: `avalonshop/internal/app/app.go` (routes), `avalonshop/static/app.js` (remove-row button), `avalonshop/static/app.css` (image action buttons)

**Interfaces:**
- Consumes: `store.ListProductsAdmin`, `GetProduct`, `CreateProduct`, `UpdateProduct`, `DeleteProduct`, `UniqueSlug`, `Slugify`, image methods, `img.Process`, `img.Remove`.
- Produces: routes `GET /admin/products`, `GET /admin/products/new`, `POST /admin/products/new`, `GET /admin/products/variant-row`, `GET /admin/products/{id}`, `POST /admin/products/{id}`, `POST /admin/products/{id}/delete`, `POST /admin/products/{id}/images` (multipart field `images`, up to 10 files, 10 MB each), `POST /admin/images/{id}` (form `alt`, `product_id`), `POST /admin/images/{id}/move` (form `dir` = up|down, `product_id`), `POST /admin/images/{id}/delete`; `parseProductForm(r) (store.Product, []store.Variant, map[string]string)`.

- [ ] **Step 1: Write the failing tests**

`avalonshop/internal/app/admin_products_test.go`:

```go
package app

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"avalonshop/internal/img"
	"avalonshop/internal/store"
)

func productForm(name string, variants ...[]string) url.Values {
	v := url.Values{"name": {name}, "description": {"A fine product"}, "meta_description": {"Buy " + name}, "active": {"on"}}
	for _, row := range variants { // id, name, sku, price, stock
		v.Add("variant_id", row[0])
		v.Add("variant_name", row[1])
		v.Add("variant_sku", row[2])
		v.Add("variant_price", row[3])
		v.Add("variant_stock", row[4])
	}
	return v
}

func pngUpload(t *testing.T, w, h int) (io.Reader, string) {
	t.Helper()
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			m.Set(x, y, color.NRGBA{uint8(x), uint8(y), 100, 255})
		}
	}
	var pngBuf bytes.Buffer
	png.Encode(&pngBuf, m)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("images", "photo.png")
	fw.Write(pngBuf.Bytes())
	mw.Close()
	return &body, mw.FormDataContentType()
}

func TestAdminProductCreateEditDelete(t *testing.T) {
	a, st := newDBApp(t)
	adm := adminSession(t, a, st)
	ctx := context.Background()
	if w := do(t, a, "GET", "/admin/products/new", nil, "Cookie", adm); w.Code != 200 || !strings.Contains(w.Body.String(), `name="variant_name"`) {
		t.Fatalf("new form: %d", w.Code)
	}
	form := productForm("Wild Forest Honey", []string{"", "500g", "HNY-500", "650", "5"}, []string{"", "", "", "", ""}, []string{"", "1kg", "", "1200", "2"})
	w := do(t, a, "POST", "/admin/products/new", strings.NewReader(form.Encode()), "Cookie", adm)
	if w.Code != 303 || !strings.HasPrefix(w.Header().Get("Location"), "/admin/products/") {
		t.Fatalf("create: %d %s\n%s", w.Code, w.Header().Get("Location"), w.Body.String())
	}
	p, err := st.GetProductBySlug(ctx, "wild-forest-honey", false)
	if err != nil || len(p.Variants) != 2 || p.Variants[0].SKU == nil || *p.Variants[0].SKU != "HNY-500" || p.Variants[1].SKU != nil {
		t.Fatalf("created product wrong: %+v %v", p, err)
	}
	// blank variant rows are skipped; a product needs at least one
	w = do(t, a, "POST", "/admin/products/new", strings.NewReader(productForm("Empty").Encode()), "Cookie", adm)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "at least one variant") {
		t.Fatalf("no variants: %d", w.Code)
	}
	// duplicate name gets a -2 slug
	do(t, a, "POST", "/admin/products/new", strings.NewReader(productForm("Wild Forest Honey", []string{"", "x", "", "1", "1"}).Encode()), "Cookie", adm)
	if _, err := st.GetProductBySlug(ctx, "wild-forest-honey-2", false); err != nil {
		t.Fatal("expected -2 slug")
	}
	// edit: reprice first, drop second, add third
	edit := productForm("Wild Honey", []string{itoa(p.Variants[0].ID), "500 g", "HNY-500", "700", "4"}, []string{"", "2kg", "", "2400", "1"})
	edit.Set("slug", "wild honey")
	w = do(t, a, "POST", "/admin/products/"+itoa(p.ID), strings.NewReader(edit.Encode()), "Cookie", adm)
	if w.Code != 303 {
		t.Fatalf("edit: %d\n%s", w.Code, w.Body.String())
	}
	p2, _ := st.GetProduct(ctx, p.ID)
	if p2.Name != "Wild Honey" || p2.Slug != "wild-honey" || len(p2.Variants) != 2 || p2.Variants[0].Price != 700 || p2.Variants[1].Name != "2kg" {
		t.Fatalf("edit lost: %+v", p2)
	}
	if w := do(t, a, "GET", "/admin/products", nil, "Cookie", adm); w.Code != 200 || !strings.Contains(w.Body.String(), "Wild Honey") {
		t.Fatalf("list: %d", w.Code)
	}
	if w := do(t, a, "GET", "/admin/products/variant-row", nil, "Cookie", adm, "HX-Request", "true"); w.Code != 200 || !strings.Contains(w.Body.String(), `name="variant_name"`) {
		t.Fatalf("variant row: %d", w.Code)
	}
	w = do(t, a, "POST", "/admin/products/"+itoa(p.ID)+"/delete", nil, "Cookie", adm)
	if w.Code != 303 || w.Header().Get("Location") != "/admin/products" {
		t.Fatalf("delete: %d", w.Code)
	}
	if _, err := st.GetProduct(ctx, p.ID); err == nil {
		t.Fatal("product still exists")
	}
}

func TestAdminProductImages(t *testing.T) {
	a, st := newDBApp(t)
	adm := adminSession(t, a, st)
	ctx := context.Background()
	id, _ := st.CreateProduct(ctx, store.Product{Slug: "p", Name: "P", Active: true}, []store.Variant{{Name: "x", Price: 1, Stock: 1}})
	body, ct := pngUpload(t, 700, 500)
	w := do(t, a, "POST", "/admin/products/"+itoa(id)+"/images", body, "Cookie", adm, "Content-Type", ct, "HX-Request", "true")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `<img src="/media/`) {
		t.Fatalf("upload: %d\n%s", w.Code, w.Body.String())
	}
	imgs, _ := st.ListImages(ctx, id)
	if len(imgs) != 1 || imgs[0].Width != 700 || imgs[0].Height != 500 {
		t.Fatalf("image row: %+v", imgs)
	}
	for _, wd := range img.WidthsFor(700) {
		if _, err := os.Stat(filepath.Join(a.cfg.UploadDir, img.Filename(imgs[0].File, wd))); err != nil {
			t.Fatalf("variant %d missing: %v", wd, err)
		}
	}
	// a non-image is reported, not fatal
	var bad bytes.Buffer
	mw := multipart.NewWriter(&bad)
	fw, _ := mw.CreateFormFile("images", "notes.txt")
	fw.Write([]byte("hello, not an image"))
	mw.Close()
	w = do(t, a, "POST", "/admin/products/"+itoa(id)+"/images", &bad, "Cookie", adm, "Content-Type", mw.FormDataContentType(), "HX-Request", "true")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "unsupported image type") {
		t.Fatalf("bad upload: %d\n%s", w.Code, w.Body.String())
	}
	// second image, move it up, set alt, delete first
	body2, ct2 := pngUpload(t, 500, 500)
	do(t, a, "POST", "/admin/products/"+itoa(id)+"/images", body2, "Cookie", adm, "Content-Type", ct2)
	imgs, _ = st.ListImages(ctx, id)
	second := imgs[1]
	w = do(t, a, "POST", "/admin/images/"+itoa(second.ID)+"/move", strings.NewReader("dir=up&product_id="+itoa(id)), "Cookie", adm)
	if w.Code != 303 {
		t.Fatalf("move: %d", w.Code)
	}
	imgs, _ = st.ListImages(ctx, id)
	if imgs[0].ID != second.ID {
		t.Fatal("move up failed")
	}
	do(t, a, "POST", "/admin/images/"+itoa(second.ID), strings.NewReader("alt=Jar+of+honey&product_id="+itoa(id)), "Cookie", adm)
	imgs, _ = st.ListImages(ctx, id)
	if imgs[0].Alt != "Jar of honey" {
		t.Fatal("alt not saved")
	}
	first := imgs[1]
	do(t, a, "POST", "/admin/images/"+itoa(first.ID)+"/delete", nil, "Cookie", adm)
	if _, err := os.Stat(filepath.Join(a.cfg.UploadDir, img.Filename(first.File, 400))); !os.IsNotExist(err) {
		t.Fatal("deleted image files remain")
	}
	// deleting the product removes remaining files
	do(t, a, "POST", "/admin/products/"+itoa(id)+"/delete", nil, "Cookie", adm)
	if _, err := os.Stat(filepath.Join(a.cfg.UploadDir, img.Filename(second.File, 400))); !os.IsNotExist(err) {
		t.Fatal("product delete left files")
	}
}

func TestAdminProductDeleteRefusedWhenOrdered(t *testing.T) {
	a, st := newDBApp(t)
	adm := adminSession(t, a, st)
	ctx := context.Background()
	id, _ := st.CreateProduct(ctx, store.Product{Slug: "p", Name: "P", Active: true}, []store.Variant{{Name: "x", Price: 1, Stock: 5}})
	p, _ := st.GetProduct(ctx, id)
	zone, _ := st.CreateZone(ctx, store.Zone{Name: "Z", Fee: 0, Active: true})
	if _, err := st.PlaceOrder(ctx, store.NewOrder{Name: "A", Phone: "01712345678", Email: "a@b.co", Address: "somewhere far", ZoneID: zone, Lines: []store.OrderLine{{VariantID: p.Variants[0].ID, Qty: 1}}}); err != nil {
		t.Fatal(err)
	}
	w := do(t, a, "POST", "/admin/products/"+itoa(id)+"/delete", nil, "Cookie", adm)
	if w.Code != 303 || !strings.Contains(cookieHeader(w, "flash"), "orders") {
		t.Fatalf("should refuse: %d %q", w.Code, cookieHeader(w, "flash"))
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `TEST_DATABASE_URL=... go test ./internal/app/ -run AdminProduct`
Expected: FAIL (404s)

- [ ] **Step 3: Write admin_products.go**

```go
package app

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"avalonshop/internal/img"
	"avalonshop/internal/store"
)

const maxUploadBytes = 10 << 20
const maxUploadFiles = 10

type productFormData struct {
	Product    store.Product
	Variants   []store.Variant
	Images     []store.Image
	Categories []store.Category
	Errors     map[string]string
	IsNew      bool
}

func (a *App) adminProducts(w http.ResponseWriter, r *http.Request) {
	rows, err := a.st.ListProductsAdmin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, "admin/products.html", page{Title: "Products", NoIndex: true, Data: rows})
}

func (a *App) productFormPage(w http.ResponseWriter, r *http.Request, status int, d productFormData) {
	cats, err := a.st.ListCategories(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	d.Categories = cats
	if d.Errors == nil {
		d.Errors = map[string]string{}
	}
	title := "Edit product"
	if d.IsNew {
		title = "New product"
	}
	a.renderStatus(w, r, status, "admin/product_form.html", page{Title: title, NoIndex: true, Data: d})
}

func (a *App) adminProductNew(w http.ResponseWriter, r *http.Request) {
	a.productFormPage(w, r, http.StatusOK, productFormData{IsNew: true, Product: store.Product{Active: true}, Variants: []store.Variant{{}}})
}

func (a *App) adminProductEdit(w http.ResponseWriter, r *http.Request) {
	p, err := a.st.GetProduct(r.Context(), pathID(r))
	if errors.Is(err, store.ErrNotFound) {
		a.notFound(w, r)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.productFormPage(w, r, http.StatusOK, productFormData{Product: p.Product, Variants: p.Variants, Images: p.Images})
}

// parseProductForm reads the product fields and the repeated variant_* fields.
// Rows with an empty name are ignored so blank template rows are harmless.
func parseProductForm(r *http.Request) (store.Product, []store.Variant, map[string]string) {
	errs := map[string]string{}
	r.ParseForm()
	p := store.Product{
		Name:            strings.TrimSpace(r.FormValue("name")),
		Slug:            strings.TrimSpace(r.FormValue("slug")),
		Description:     strings.TrimSpace(r.FormValue("description")),
		MetaDescription: strings.TrimSpace(r.FormValue("meta_description")),
		Active:          r.FormValue("active") == "on",
		Featured:        r.FormValue("featured") == "on",
	}
	if cid, _ := strconv.ParseInt(r.FormValue("category_id"), 10, 64); cid > 0 {
		p.CategoryID = &cid
	}
	if p.Name == "" {
		errs["name"] = "Name is required."
	}
	if utf8.RuneCountInString(p.MetaDescription) > 160 {
		errs["meta_description"] = "Keep the meta description under 160 characters; search engines cut it there."
	}
	if p.Slug == "" {
		p.Slug = p.Name
	}
	p.Slug = store.Slugify(p.Slug)

	f := r.Form
	var vs []store.Variant
	for i := range f["variant_name"] {
		name := strings.TrimSpace(f["variant_name"][i])
		if name == "" {
			continue
		}
		get := func(key string) string {
			if i < len(f[key]) {
				return strings.TrimSpace(f[key][i])
			}
			return ""
		}
		id, _ := strconv.ParseInt(get("variant_id"), 10, 64)
		price, err1 := strconv.Atoi(get("variant_price"))
		stock, err2 := strconv.Atoi(get("variant_stock"))
		if err1 != nil || err2 != nil || price < 0 || stock < 0 {
			errs["variants"] = "Price and stock must be whole numbers, 0 or more."
		}
		sku := get("variant_sku")
		vs = append(vs, store.Variant{ID: id, Name: name, SKU: &sku, Price: price, Stock: stock, Sort: len(vs)})
	}
	if len(vs) == 0 {
		errs["variants"] = "Add at least one variant (even a single default size)."
	}
	return p, vs, errs
}

func (a *App) adminProductCreate(w http.ResponseWriter, r *http.Request) {
	p, vs, errs := parseProductForm(r)
	if len(errs) > 0 {
		a.productFormPage(w, r, http.StatusUnprocessableEntity, productFormData{IsNew: true, Product: p, Variants: vs, Errors: errs})
		return
	}
	var err error
	if p.Slug, err = a.st.UniqueSlug(r.Context(), "products", p.Slug, 0); err != nil {
		a.serverError(w, r, err)
		return
	}
	id, err := a.st.CreateProduct(r.Context(), p, vs)
	if errors.Is(err, store.ErrDuplicate) {
		errs["variants"] = "One of those SKUs is already used by another product."
		a.productFormPage(w, r, http.StatusUnprocessableEntity, productFormData{IsNew: true, Product: p, Variants: vs, Errors: errs})
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.flashBack(w, r, "Product created. Now add some photos.", "/admin/products/"+strconv.FormatInt(id, 10))
}

func (a *App) adminProductUpdate(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	existing, err := a.st.GetProduct(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		a.notFound(w, r)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	p, vs, errs := parseProductForm(r)
	p.ID = id
	if len(errs) > 0 {
		a.productFormPage(w, r, http.StatusUnprocessableEntity, productFormData{Product: p, Variants: vs, Images: existing.Images, Errors: errs})
		return
	}
	if p.Slug, err = a.st.UniqueSlug(r.Context(), "products", p.Slug, id); err != nil {
		a.serverError(w, r, err)
		return
	}
	err = a.st.UpdateProduct(r.Context(), p, vs)
	if errors.Is(err, store.ErrDuplicate) {
		errs["variants"] = "One of those SKUs is already used by another product."
		a.productFormPage(w, r, http.StatusUnprocessableEntity, productFormData{Product: p, Variants: vs, Images: existing.Images, Errors: errs})
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.flashBack(w, r, "Saved.", "/admin/products/"+strconv.FormatInt(id, 10))
}

func (a *App) adminProductDelete(w http.ResponseWriter, r *http.Request) {
	imgs, err := a.st.DeleteProduct(r.Context(), pathID(r))
	switch {
	case errors.Is(err, store.ErrInUse):
		a.flashBack(w, r, "This product appears in orders, so it can't be deleted. Mark it inactive instead.", "/admin/products")
		return
	case errors.Is(err, store.ErrNotFound):
		a.notFound(w, r)
		return
	case err != nil:
		a.serverError(w, r, err)
		return
	}
	for _, im := range imgs {
		img.Remove(a.cfg.UploadDir, im.File, im.Width)
	}
	a.flashBack(w, r, "Product deleted.", "/admin/products")
}

func (a *App) adminVariantRow(w http.ResponseWriter, r *http.Request) {
	a.renderPartial(w, "variant_row.html", store.Variant{})
}

// ---- images ----

func (a *App) imagesResponse(w http.ResponseWriter, r *http.Request, productID int64, errs []string) {
	back := "/admin/products/" + strconv.FormatInt(productID, 10)
	if !isHTMX(r) {
		if len(errs) > 0 {
			a.setFlash(w, strings.Join(errs, " "))
		}
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	imgs, err := a.st.ListImages(r.Context(), productID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.renderPartial(w, "image_list.html", map[string]any{"ProductID": productID, "Images": imgs, "Errors": errs})
}

func (a *App) adminImagesUpload(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	if _, err := a.st.GetProduct(r.Context(), id); err != nil {
		a.notFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, int64(maxUploadFiles*maxUploadBytes+1<<20))
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		a.imagesResponse(w, r, id, []string{"Upload too large: at most 10 images of 10 MB each per upload."})
		return
	}
	defer r.MultipartForm.RemoveAll()
	var errs []string
	files := r.MultipartForm.File["images"]
	if len(files) > maxUploadFiles {
		files = files[:maxUploadFiles]
		errs = append(errs, "Only the first 10 files were processed.")
	}
	for _, fh := range files {
		if fh.Size > maxUploadBytes {
			errs = append(errs, fh.Filename+": over 10 MB.")
			continue
		}
		f, err := fh.Open()
		if err != nil {
			errs = append(errs, fh.Filename+": could not read.")
			continue
		}
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			errs = append(errs, fh.Filename+": could not read.")
			continue
		}
		res, err := img.Process(data, a.cfg.UploadDir)
		if err != nil {
			errs = append(errs, fh.Filename+": "+err.Error())
			continue
		}
		if _, err := a.st.AddImage(r.Context(), store.Image{ProductID: id, File: res.Stem, Width: res.Width, Height: res.Height}); err != nil {
			img.Remove(a.cfg.UploadDir, res.Stem, res.Width)
			errs = append(errs, fh.Filename+": could not save.")
		}
	}
	a.imagesResponse(w, r, id, errs)
}

func (a *App) adminImageAlt(w http.ResponseWriter, r *http.Request) {
	productID, _ := strconv.ParseInt(r.FormValue("product_id"), 10, 64)
	if err := a.st.UpdateImageAlt(r.Context(), pathID(r), strings.TrimSpace(r.FormValue("alt"))); err != nil {
		a.imagesResponse(w, r, productID, []string{"Could not save alt text."})
		return
	}
	a.imagesResponse(w, r, productID, nil)
}

func (a *App) adminImageMove(w http.ResponseWriter, r *http.Request) {
	productID, _ := strconv.ParseInt(r.FormValue("product_id"), 10, 64)
	if err := a.st.MoveImage(r.Context(), pathID(r), r.FormValue("dir") == "up"); err != nil {
		a.imagesResponse(w, r, productID, []string{"Could not reorder."})
		return
	}
	a.imagesResponse(w, r, productID, nil)
}

func (a *App) adminImageDelete(w http.ResponseWriter, r *http.Request) {
	im, err := a.st.DeleteImage(r.Context(), pathID(r))
	if err != nil {
		a.notFound(w, r)
		return
	}
	img.Remove(a.cfg.UploadDir, im.File, im.Width)
	a.imagesResponse(w, r, im.ProductID, nil)
}
```

- [ ] **Step 4: Register the routes**

In `routes()`, after the zone routes:

```go
	m.HandleFunc("GET /admin/products", adm(a.adminProducts))
	m.HandleFunc("GET /admin/products/new", adm(a.adminProductNew))
	m.HandleFunc("POST /admin/products/new", adm(a.adminProductCreate))
	m.HandleFunc("GET /admin/products/variant-row", adm(a.adminVariantRow))
	m.HandleFunc("GET /admin/products/{id}", adm(a.adminProductEdit))
	m.HandleFunc("POST /admin/products/{id}", adm(a.adminProductUpdate))
	m.HandleFunc("POST /admin/products/{id}/delete", adm(a.adminProductDelete))
	m.HandleFunc("POST /admin/products/{id}/images", adm(a.adminImagesUpload))
	m.HandleFunc("POST /admin/images/{id}", adm(a.adminImageAlt))
	m.HandleFunc("POST /admin/images/{id}/move", adm(a.adminImageMove))
	m.HandleFunc("POST /admin/images/{id}/delete", adm(a.adminImageDelete))
```

- [ ] **Step 5: Small static additions**

Append to `static/app.js` inside the `click` listener, before its closing brace:

```js
    var rm = e.target.closest('[data-remove-row]');
    if (rm) { rm.closest('tr').remove(); }
```

Append to `static/app.css`:

```css
.imgs .acts form{flex:1;margin:0}
.imgs .acts button{width:100%}
.imgs form input[type=text]{margin:.4rem 0 .3rem}
```

- [ ] **Step 6: Write the templates**

`avalonshop/templates/partials/variant_row.html`:

```html
<tr>
  <td><input type="hidden" name="variant_id" value="{{if .ID}}{{.ID}}{{end}}"><input type="text" name="variant_name" value="{{.Name}}" placeholder="e.g. 500g" aria-label="Variant name"></td>
  <td><input type="text" name="variant_sku" value="{{deref .SKU}}" placeholder="optional" aria-label="SKU"></td>
  <td><input type="number" name="variant_price" min="0" value="{{if .ID}}{{.Price}}{{end}}" aria-label="Price"></td>
  <td><input type="number" name="variant_stock" min="0" value="{{if .ID}}{{.Stock}}{{else}}0{{end}}" aria-label="Stock"></td>
  <td><button class="btn sm ghost" type="button" data-remove-row aria-label="Remove variant">×</button></td>
</tr>
```

`avalonshop/templates/partials/image_list.html`:

```html
{{if .Errors}}<div class="errbox" role="alert">{{range .Errors}}<div>{{.}}</div>{{end}}</div>{{end}}
<div class="imgs">
{{range .Images}}<figure>
  <img src="{{imgURL .File 400}}" width="{{.Width}}" height="{{.Height}}" alt="{{.Alt}}" loading="lazy" decoding="async">
  <form method="post" action="/admin/images/{{.ID}}" hx-post="/admin/images/{{.ID}}" hx-target="#images" hx-swap="innerHTML">
    <input type="hidden" name="product_id" value="{{.ProductID}}">
    <input type="text" name="alt" value="{{.Alt}}" placeholder="Alt text: describe the photo" aria-label="Alt text">
    <button class="btn sm ghost" type="submit">Save alt</button>
  </form>
  <div class="acts">
    <form method="post" action="/admin/images/{{.ID}}/move" hx-post="/admin/images/{{.ID}}/move" hx-target="#images" hx-swap="innerHTML"><input type="hidden" name="product_id" value="{{.ProductID}}"><input type="hidden" name="dir" value="up"><button class="btn sm ghost" type="submit" aria-label="Move up">↑</button></form>
    <form method="post" action="/admin/images/{{.ID}}/move" hx-post="/admin/images/{{.ID}}/move" hx-target="#images" hx-swap="innerHTML"><input type="hidden" name="product_id" value="{{.ProductID}}"><input type="hidden" name="dir" value="down"><button class="btn sm ghost" type="submit" aria-label="Move down">↓</button></form>
    <form method="post" action="/admin/images/{{.ID}}/delete" hx-post="/admin/images/{{.ID}}/delete" hx-target="#images" hx-swap="innerHTML" hx-confirm="Delete this image?"><button class="btn sm danger" type="submit">Delete</button></form>
  </div>
</figure>{{else}}<p class="empty">No photos yet. The first photo is the one shown in listings.</p>{{end}}
</div>
```

`avalonshop/templates/admin/products.html`:

```html
{{define "content"}}
<div class="toolbar"><h1>Products</h1><a class="btn sm" href="/admin/products/new">New product</a></div>
<div class="table-wrap"><table>
<thead><tr><th></th><th>Name</th><th>Category</th><th class="num">Variants</th><th>Status</th><th></th></tr></thead>
<tbody>{{range .Data}}<tr>
  <td>{{if .ImageFile}}<img class="thumb" src="{{imgURL (deref .ImageFile) 400}}" width="48" height="48" alt="" loading="lazy">{{else}}<div class="thumb"></div>{{end}}</td>
  <td><a href="/admin/products/{{.ID}}">{{.Name}}</a><br><span class="muted mono">/products/{{.Slug}}</span></td>
  <td>{{.Category}}</td>
  <td class="num">{{.VariantCount}}</td>
  <td>{{if .Active}}<span class="status delivered">active</span>{{else}}<span class="status cancelled">inactive</span>{{end}} {{if .Featured}}<span class="status">featured</span>{{end}}</td>
  <td><a href="/products/{{.Slug}}" class="help">View</a></td>
</tr>{{else}}<tr><td colspan="6" class="empty">No products yet.</td></tr>{{end}}</tbody>
</table></div>
{{end}}
```

`avalonshop/templates/admin/product_form.html`:

```html
{{define "content"}}{{$d := .Data}}{{$p := $d.Product}}
<h1>{{if $d.IsNew}}New product{{else}}{{$p.Name}}{{end}}</h1>
<form class="form wide" method="post" action="{{if $d.IsNew}}/admin/products/new{{else}}/admin/products/{{$p.ID}}{{end}}">
  <div class="row">
    <div class="field"><label for="name">Name</label><input id="name" type="text" name="name" value="{{$p.Name}}" required>{{with $d.Errors.name}}<p class="err">{{.}}</p>{{end}}</div>
    <div class="field"><label for="slug">URL slug</label><input id="slug" type="text" name="slug" value="{{$p.Slug}}" placeholder="auto from name"><p class="help">Lowercase letters, numbers, hyphens. Changing it breaks old links.</p></div>
  </div>
  <div class="row">
    <div class="field"><label for="category_id">Category</label><select id="category_id" name="category_id"><option value="">— none —</option>{{range $d.Categories}}<option value="{{.ID}}"{{if and $p.CategoryID (eq .ID (derefInt64 $p.CategoryID))}} selected{{end}}>{{.Name}}</option>{{end}}</select></div>
    <div class="field"><label class="check"><input type="checkbox" name="active"{{if $p.Active}} checked{{end}}> Active (visible in the shop)</label><label class="check"><input type="checkbox" name="featured"{{if $p.Featured}} checked{{end}}> Featured on the home page</label></div>
  </div>
  <div class="field"><label for="description">Description</label><textarea id="description" name="description" rows="8">{{$p.Description}}</textarea><p class="help">Plain text. Line breaks are kept.</p></div>
  <div class="field"><label for="meta_description">Meta description (search snippet)</label><input id="meta_description" type="text" name="meta_description" value="{{$p.MetaDescription}}" maxlength="160"><p class="help">Up to 160 characters. Leave empty to use the start of the description.</p>{{with $d.Errors.meta_description}}<p class="err">{{.}}</p>{{end}}</div>

  <h2>Variants</h2>
  {{with $d.Errors.variants}}<div class="errbox" role="alert">{{.}}</div>{{end}}
  <div class="table-wrap"><table class="variants" id="variants">
    <thead><tr><th>Name</th><th>SKU</th><th>Price (৳)</th><th>Stock</th><th></th></tr></thead>
    <tbody>{{range $d.Variants}}{{template "variant_row.html" .}}{{end}}</tbody>
  </table></div>
  <p><button class="btn sm ghost" type="button" hx-get="/admin/products/variant-row" hx-target="#variants tbody" hx-swap="beforeend">Add variant</button></p>
  <button class="btn" type="submit">{{if $d.IsNew}}Create product{{else}}Save changes{{end}}</button>
  {{if not $d.IsNew}} <a class="btn ghost" href="/products/{{$p.Slug}}">View in shop</a>{{end}}
</form>

{{if not $d.IsNew}}
<h2>Photos</h2>
<form class="form" method="post" action="/admin/products/{{$p.ID}}/images" enctype="multipart/form-data" hx-post="/admin/products/{{$p.ID}}/images" hx-encoding="multipart/form-data" hx-target="#images" hx-swap="innerHTML">
  <div class="field"><label for="images">Add photos (JPEG, PNG, WebP or GIF, up to 10 MB each)</label><input id="images" type="file" name="images" accept="image/jpeg,image/png,image/webp,image/gif" multiple required></div>
  <button class="btn sm" type="submit">Upload</button>
</form>
<div id="images">{{template "image_list.html" (imageListData $p.ID $d.Images)}}</div>

<h2>Danger zone</h2>
<form method="post" action="/admin/products/{{$p.ID}}/delete" hx-post="/admin/products/{{$p.ID}}/delete" hx-confirm="Delete “{{$p.Name}}” and all its photos?"><button class="btn danger sm" type="submit">Delete product</button></form>
{{end}}
{{end}}
```

Add two funcs to `funcs()` in `render.go`:

```go
		"derefInt64": func(p *int64) int64 { if p == nil { return 0 }; return *p },
		"imageListData": func(productID int64, imgs []store.Image) map[string]any {
			return map[string]any{"ProductID": productID, "Images": imgs, "Errors": []string(nil)}
		},
```

- [ ] **Step 7: Run to verify it passes, then commit**

Run: `TEST_DATABASE_URL=... go test ./internal/app/`
Expected: PASS

```bash
git add avalonshop
git commit -m "feat(shop): admin product editor with variants and image uploads"
```

---

### Task 18: Admin orders

**Files:**
- Create: `avalonshop/internal/app/admin_orders.go`, `avalonshop/internal/app/admin_orders_test.go`, `avalonshop/templates/admin/orders.html`, `avalonshop/templates/admin/order.html`
- Modify: `avalonshop/internal/app/app.go` (routes), `avalonshop/internal/store/orders.go` (add `NextStatuses`)

**Interfaces:**
- Consumes: `store.ListOrders`, `GetOrder`, `UpdateOrderStatus`, `SetAdminNote`, `a.sendOrderMails`, `order_rows.html`.
- Produces: `store.NextStatuses(from string) []string`; routes `GET /admin/orders?status=` (default `new`; `all` shows everything), `GET /admin/orders/{id}`, `POST /admin/orders/{id}/status` (form `status`), `POST /admin/orders/{id}/note` (form `note`).

- [ ] **Step 1: Write the failing tests**

`avalonshop/internal/app/admin_orders_test.go`:

```go
package app

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"

	"avalonshop/internal/mail"
	"avalonshop/internal/store"
)

func TestAdminOrdersFlow(t *testing.T) {
	a, st := newDBApp(t)
	adm := adminSession(t, a, st)
	ctx := context.Background()
	var logbuf bytes.Buffer
	a.mail, _ = mail.New("", "587", "", "", "shop@test.local", os.DirFS("../.."), slog.New(slog.NewTextHandler(&logbuf, nil)))

	slug := seedCatalog(t, st)
	v1, _ := variantIDs(t, a, slug)
	zone, _ := st.CreateZone(ctx, store.Zone{Name: "Rajshahi", Fee: 60, Active: true})
	o, err := st.PlaceOrder(ctx, store.NewOrder{Name: "Ana", Phone: "01712345678", Email: "ana@example.com", Address: "Road 1, Rajshahi", ZoneID: zone, Lines: []store.OrderLine{{VariantID: v1, Qty: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	w := do(t, a, "GET", "/admin/orders", nil, "Cookie", adm)
	if w.Code != 200 || !strings.Contains(w.Body.String(), o.Number) {
		t.Fatalf("list default (new): %d", w.Code)
	}
	if w := do(t, a, "GET", "/admin/orders?status=shipped", nil, "Cookie", adm); strings.Contains(w.Body.String(), o.Number) {
		t.Fatal("status filter not applied")
	}
	w = do(t, a, "GET", "/admin/orders/"+itoa(o.ID), nil, "Cookie", adm)
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, "Ana") || !strings.Contains(body, `value="confirmed"`) || strings.Contains(body, `value="delivered"`) {
		t.Fatalf("detail should offer only allowed transitions: %d\n%s", w.Code, body)
	}
	w = do(t, a, "POST", "/admin/orders/"+itoa(o.ID)+"/status", strings.NewReader("status=delivered"), "Cookie", adm)
	if w.Code != 303 || !strings.Contains(cookieHeader(w, "flash"), "allowed") {
		t.Fatalf("bad transition should flash: %d %q", w.Code, cookieHeader(w, "flash"))
	}
	do(t, a, "POST", "/admin/orders/"+itoa(o.ID)+"/status", strings.NewReader("status=confirmed"), "Cookie", adm)
	do(t, a, "POST", "/admin/orders/"+itoa(o.ID)+"/status", strings.NewReader("status=shipped"), "Cookie", adm)
	a.mail.Wait()
	if !strings.Contains(logbuf.String(), "on its way") {
		t.Fatalf("shipped email not sent:\n%s", logbuf.String())
	}
	do(t, a, "POST", "/admin/orders/"+itoa(o.ID)+"/note", strings.NewReader("note=Left+with+neighbour"), "Cookie", adm)
	got, _ := st.GetOrder(ctx, o.ID)
	if got.Status != "shipped" || got.AdminNote != "Left with neighbour" {
		t.Fatalf("order state: %+v", got.Order)
	}
	if w := do(t, a, "GET", "/admin/orders?status=all", nil, "Cookie", adm); !strings.Contains(w.Body.String(), o.Number) {
		t.Fatal("all filter")
	}
	if w := do(t, a, "GET", "/admin/orders/999", nil, "Cookie", adm); w.Code != 404 {
		t.Fatal("missing order should 404")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `TEST_DATABASE_URL=... go test ./internal/app/ -run AdminOrders`
Expected: FAIL (404)

- [ ] **Step 3: Add NextStatuses to the store**

In `internal/store/orders.go`, below `CanTransition`:

```go
// NextStatuses lists the statuses an order may move to from its current one.
func NextStatuses(from string) []string { return transitions[from] }
```

- [ ] **Step 4: Write admin_orders.go**

```go
package app

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"avalonshop/internal/store"
)

var orderStatuses = []string{"new", "confirmed", "shipped", "delivered", "cancelled"}

func (a *App) adminOrders(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "new"
	}
	filter := status
	if status == "all" {
		filter = ""
	}
	orders, err := a.st.ListOrders(r.Context(), filter, 200)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, "admin/orders.html", page{Title: "Orders", NoIndex: true, Data: map[string]any{"Orders": orders, "Status": status, "Statuses": orderStatuses}})
}

func (a *App) adminOrder(w http.ResponseWriter, r *http.Request) {
	o, err := a.st.GetOrder(r.Context(), pathID(r))
	if errors.Is(err, store.ErrNotFound) {
		a.notFound(w, r)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, "admin/order.html", page{Title: "Order " + o.Number, NoIndex: true, Data: map[string]any{
		"Order": o, "Next": store.NextStatuses(o.Status), "TrackURL": a.orderURL(o.Number),
	}})
}

func (a *App) adminOrderStatus(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	back := "/admin/orders/" + strconv.FormatInt(id, 10)
	to := r.FormValue("status")
	o, err := a.st.UpdateOrderStatus(r.Context(), id, to)
	switch {
	case errors.Is(err, store.ErrTransition):
		a.flashBack(w, r, "That change isn't allowed from the order's current status.", back)
		return
	case errors.Is(err, store.ErrNotFound):
		a.notFound(w, r)
		return
	case err != nil:
		a.serverError(w, r, err)
		return
	}
	switch to {
	case "shipped":
		a.sendOrderMails(o, "order_shipped", "")
	case "delivered":
		a.sendOrderMails(o, "order_delivered", "")
	}
	a.flashBack(w, r, "Order marked "+to+".", back)
}

func (a *App) adminOrderNote(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	if err := a.st.SetAdminNote(r.Context(), id, strings.TrimSpace(r.FormValue("note"))); err != nil {
		a.notFound(w, r)
		return
	}
	a.flashBack(w, r, "Note saved.", "/admin/orders/"+strconv.FormatInt(id, 10))
}
```

- [ ] **Step 5: Register the routes**

In `routes()`, after the product routes:

```go
	m.HandleFunc("GET /admin/orders", adm(a.adminOrders))
	m.HandleFunc("GET /admin/orders/{id}", adm(a.adminOrder))
	m.HandleFunc("POST /admin/orders/{id}/status", adm(a.adminOrderStatus))
	m.HandleFunc("POST /admin/orders/{id}/note", adm(a.adminOrderNote))
```

- [ ] **Step 6: Write the templates**

`avalonshop/templates/admin/orders.html`:

```html
{{define "content"}}{{$d := .Data}}
<h1>Orders</h1>
<div class="chips">
  {{$cur := $d.Status}}{{range $d.Statuses}}<a class="chip{{if eq . $cur}} on{{end}}" href="/admin/orders?status={{.}}">{{.}}</a>{{end}}
  <a class="chip{{if eq $cur "all"}} on{{end}}" href="/admin/orders?status=all">all</a>
</div>
{{template "order_rows.html" $d.Orders}}
{{end}}
```

`avalonshop/templates/admin/order.html`:

```html
{{define "content"}}{{$d := .Data}}{{$o := $d.Order}}
<div class="toolbar"><h1>Order {{$o.Number}}</h1><span class="status {{$o.Status}}">{{$o.Status}}</span></div>
<p class="help">Placed {{dhaka $o.CreatedAt}} · updated {{dhaka $o.UpdatedAt}} · <a href="{{$d.TrackURL}}">customer view</a></p>
<div class="cart-page">
  <div>
    <h2>Items</h2>
    <div class="table-wrap"><table>
      <thead><tr><th>Item</th><th class="num">Qty</th><th class="num">Unit</th><th class="num">Amount</th></tr></thead>
      <tbody>{{range $o.Items}}<tr><td>{{.ProductName}} <span class="muted">{{.VariantName}}</span></td><td class="num">{{.Qty}}</td><td class="num">{{taka .UnitPrice}}</td><td class="num">{{taka (mul .UnitPrice .Qty)}}</td></tr>{{end}}</tbody>
    </table></div>
    <div class="totals"><span>Subtotal</span><span class="mono">{{taka $o.Subtotal}}</span><span>Delivery ({{$o.ZoneName}})</span><span class="mono">{{taka $o.DeliveryFee}}</span><span class="big">Total to collect</span><span class="big price">{{taka $o.Total}}</span></div>
    {{if $o.Note}}<h3>Customer note</h3><p class="desc">{{$o.Note}}</p>{{end}}
    <h3>Internal note</h3>
    <form method="post" action="/admin/orders/{{$o.ID}}/note"><div class="field"><textarea name="note" rows="3">{{$o.AdminNote}}</textarea></div><button class="btn sm ghost" type="submit">Save note</button></form>
  </div>
  <aside class="panel">
    <h2>Customer</h2>
    <p class="desc">{{$o.Name}}
<a href="tel:{{$o.Phone}}">{{$o.Phone}}</a>
<a href="mailto:{{$o.Email}}">{{$o.Email}}</a>
{{$o.Address}}</p>
    <h2>Update status</h2>
    {{if $d.Next}}{{range $d.Next}}<form class="inline" method="post" action="/admin/orders/{{$o.ID}}/status"{{if eq . "cancelled"}} hx-post="/admin/orders/{{$o.ID}}/status" hx-confirm="Cancel this order and restore stock?"{{end}}><input type="hidden" name="status" value="{{.}}"><button class="btn sm{{if eq . "cancelled"}} danger{{end}}" type="submit">Mark {{.}}</button></form> {{end}}
    <p class="help">Shipped and delivered email the customer. Cancelled puts stock back.</p>
    {{else}}<p class="help">This order is closed.</p>{{end}}
  </aside>
</div>
{{end}}
```

- [ ] **Step 7: Run to verify it passes, then commit**

Run: `TEST_DATABASE_URL=... go test ./...`
Expected: PASS across all packages

```bash
git add avalonshop
git commit -m "feat(shop): admin order list, detail, status transitions, notes"
```

---

### Task 19: Performance and SEO verification, Docker smoke test, deploy docs

**Files:**
- Create: `avalonshop/internal/app/seo_test.go`
- Modify: `avalonshop/README.md`

**Interfaces:**
- Consumes: everything. This task adds automated guards for the performance budget and documents deployment.

- [ ] **Step 1: Write the budget guard test**

`avalonshop/internal/app/seo_test.go`:

```go
package app

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"

	"avalonshop/internal/store"
)

var (
	imgTagRe   = regexp.MustCompile(`<img\b[^>]*>`)
	externalRe = regexp.MustCompile(`(?:src|href)="(https?://[^"]+)"`)
)

func gzipSize(b []byte) int {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(b)
	zw.Close()
	return buf.Len()
}

// TestPerformanceBudget enforces the spec's page budget on every public page:
// no third-party requests, complete image attributes, one h1, and size caps.
func TestPerformanceBudget(t *testing.T) {
	a, st := newDBApp(t)
	slug := seedCatalog(t, st)
	v1, _ := variantIDs(t, a, slug)
	st.CreateZone(context.Background(), store.Zone{Name: "Rajshahi", Fee: 60, Active: true})
	cart := cookieHeader(do(t, a, "POST", "/cart/items", strings.NewReader("variant_id="+itoa(v1)+"&qty=1")), "cart")

	pages := []string{"/", "/products", "/products?category=honey", "/products/" + slug, "/cart", "/checkout", "/login", "/register"}
	for _, p := range pages {
		w := do(t, a, "GET", p, nil, "Cookie", "cart="+cart)
		if w.Code != 200 {
			t.Fatalf("%s: status %d", p, w.Code)
		}
		body := w.Body.Bytes()
		html := string(body)
		if n := gzipSize(body); n > 30*1024 {
			t.Errorf("%s: %d bytes gzipped, budget is 30 KB", p, n)
		}
		if strings.Count(html, "<h1") != 1 {
			t.Errorf("%s: expected exactly one <h1>", p)
		}
		for _, m := range externalRe.FindAllStringSubmatch(html, -1) {
			if !strings.HasPrefix(m[1], a.cfg.BaseURL) && !strings.HasPrefix(m[1], "https://schema.org") {
				t.Errorf("%s: external resource %s", p, m[1])
			}
		}
		for _, tag := range imgTagRe.FindAllString(html, -1) {
			for _, attr := range []string{`width="`, `height="`, `alt="`} {
				if !strings.Contains(tag, attr) {
					t.Errorf("%s: <img> missing %s: %s", p, attr, tag)
				}
			}
			if strings.Contains(tag, "/media/") && !strings.Contains(tag, "srcset=") && !strings.Contains(tag, `width="64"`) && !strings.Contains(tag, `width="48"`) {
				t.Errorf("%s: media image without srcset: %s", p, tag)
			}
			if !strings.Contains(tag, `fetchpriority="high"`) && !strings.Contains(tag, `loading="lazy"`) && p == "/products/"+slug {
				t.Errorf("%s: non-hero image must be lazy: %s", p, tag)
			}
		}
		if !strings.Contains(html, `<link rel="preload" href="/static/fonts/manrope.woff2"`) {
			t.Errorf("%s: Manrope not preloaded", p)
		}
		if strings.Contains(html, "<script src=") && !strings.Contains(html, `defer></script>`) {
			t.Errorf("%s: scripts must be deferred", p)
		}
	}
	css, _ := os.ReadFile("../../static/app.css")
	if len(css) > 15*1024 {
		t.Errorf("app.css is %d bytes, budget 15 KB", len(css))
	}
	js, _ := os.ReadFile("../../static/app.js")
	htmx, _ := os.ReadFile("../../static/htmx.min.js")
	if n := gzipSize(append(js, htmx...)); n > 25*1024 {
		t.Errorf("JS is %d bytes gzipped, budget 25 KB", n)
	}
	// static assets carry the immutable cache header and gzip
	w := do(t, a, "GET", "/static/app.js?v=x", nil, "Accept-Encoding", "gzip")
	if w.Header().Get("Content-Encoding") != "gzip" || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("static js headers: %v", w.Header())
	}
	zr, _ := gzip.NewReader(w.Body)
	if b, _ := io.ReadAll(zr); len(b) != len(js) {
		t.Error("gzipped js does not round-trip")
	}
}
```

- [ ] **Step 2: Run it and fix anything it flags**

Run: `TEST_DATABASE_URL=... go test ./internal/app/ -run PerformanceBudget -v`
Expected: PASS. If a page exceeds a budget, trim the template or CSS rather than raising the budget.

- [ ] **Step 3: Docker smoke test**

```bash
cd avalonshop
docker build -t avalonshop:local .
docker run -d --name avalon-smoke --network host \
  -e DATABASE_URL='postgres://avalon:avalon@localhost:5432/avalon?sslmode=disable' \
  -e BASE_URL=http://localhost:8080 -e SESSION_SECRET=$(printf '0%.0s' {1..64}) \
  -e MAIL_FROM=shop@example.com -e ORDER_NOTIFY_EMAIL=owner@example.com \
  -e ADMIN_EMAIL=admin@example.com -e ADMIN_PASSWORD=admin-pass-123 avalonshop:local
sleep 5
docker inspect --format '{{.State.Health.Status}}' avalon-smoke     # expect: healthy
curl -sI -H 'Accept-Encoding: gzip' http://localhost:8080/ | grep -iE 'content-encoding|cache-control'
curl -s http://localhost:8080/robots.txt
docker rm -f avalon-smoke
```

Expected: `healthy`, `content-encoding: gzip`, `cache-control: private, no-cache`, and the robots body.

- [ ] **Step 4: Lighthouse run (manual, needs Node and Chrome)**

With `go run .` serving seeded data (log in at `/login`, add a zone, a category, one product with two photos):

```bash
npx --yes lighthouse http://localhost:8080/products/<slug> --preset=perf --form-factor=mobile --screenEmulation.mobile --output=html --output-path=/tmp/lh-product.html --only-categories=performance,seo,accessibility,best-practices --quiet
npx --yes lighthouse http://localhost:8080/ --form-factor=mobile --output=html --output-path=/tmp/lh-home.html --only-categories=performance,seo --quiet
```

Targets: Performance ≥ 95, SEO = 100, Accessibility ≥ 95. Common fixes if short: an image without `sizes` (check the template), a font not preloaded, a contrast issue on `--muted` text on `--tint` (darken `--muted` to `#5f7266`). After deploy, repeat on the live URL with https://pagespeed.web.dev.

- [ ] **Step 5: Write the deploy section of the README**

Replace the "Deploy on Coolify" section in `avalonshop/README.md` with:

```markdown
## Deploy on Coolify

1. **New resource → Docker Compose**, pick this repository and branch.
   - Base directory: `/avalonshop`
   - Docker Compose file: `/docker-compose.yml`
2. Coolify generates `SERVICE_FQDN_APP`, `SERVICE_PASSWORD_POSTGRES`, and `SERVICE_HEX_64_SESSION`. Leave them.
3. Add these environment variables in the Coolify UI:
   - `SMTP_USER`, `SMTP_PASS` — from Brevo → SMTP & API → SMTP (login is your Brevo account email, password is the SMTP key)
   - `MAIL_FROM` — e.g. `shop@avalonbd.com` (must be a verified sender in Brevo)
   - `ORDER_NOTIFY_EMAIL` — where new-order alerts go
   - `ADMIN_EMAIL`, `ADMIN_PASSWORD` — the first admin; **remove both after the first successful login**
4. On the `app` service, set the domain (e.g. `https://avalonbd.com`). `BASE_URL` follows it automatically.
5. Deploy. Health check is built into the image; the proxy waits for it.
6. DNS at your registrar:
   - `A` / `CNAME` for the shop domain → the Coolify server.
   - SPF, DKIM, DMARC records for `avalonbd.com` exactly as Brevo shows under Senders & Domains → Domains. Deliverability depends on these.
7. Optional but recommended: put the domain behind Cloudflare (proxied). It adds Brotli, edge caching of `/static` and `/media` (they are `immutable`), and DDoS protection for free.

### First-run checklist

- `/healthz` returns `ok`.
- Log in at `/login` with the admin credentials → lands on `/admin`.
- Add at least one **delivery zone** (customers can't check out without one).
- Add categories, then products with photos. The first photo is the listing image; fill in alt text.
- Place a test order as a guest; confirm both emails arrive (customer confirmation, admin alert).
- Mark it confirmed → shipped → delivered from `/admin/orders`; confirm the shipped/delivered emails.
- Check `/sitemap.xml` and `/robots.txt`, then submit the sitemap in Google Search Console.
- Run PageSpeed Insights on the home and a product page: Performance ≥ 95, SEO 100.

### Backups

Postgres data lives in the `pgdata` volume and uploads in `uploads`. Enable Coolify's scheduled database backups for the `db` service, and back up the `uploads` volume with the server's regular volume backups.
```

- [ ] **Step 6: Final full test run and commit**

Run: `go vet ./... && TEST_DATABASE_URL=... go test ./... -count=1`
Expected: PASS

```bash
git add avalonshop
git commit -m "docs(shop): performance budget test, Docker smoke test, Coolify deploy guide"
```

---

## Self-review notes

- **Spec coverage:** stack and dependency list (Tasks 1, 2, 6, 8), repository layout (header, Task 1), Coolify compose and Dockerfile with healthcheck (Task 1, verified Task 19), data model and indexes (Task 2), status transitions and atomic stock (Task 7), storefront routes (Tasks 11–15), cookies and tokens (Tasks 3, 10), checkout flow and emails (Tasks 9, 13), HTMX with no-JS fallbacks (Tasks 10, 12, 13), admin (Tasks 16–18), image pipeline (Task 8, wired in 17), SEO (Tasks 10, 11, 15, guarded in 19), design system tokens and components (Task 10 CSS, page templates), accessibility basics (labels, focus ring, real form controls, Escape closes drawer, reduced motion in CSS), security (CSRF, CSP, headers, upload hardening, rate limits, 404 for admin), testing (unit always; DB-gated via `storetest`), local dev `.env` (Task 1). The spec's `templates/email/*.txt` and `internal/money` are in Task 9 and Task 4.
- **Deviation recorded:** one `internal/app` package instead of `web` + `admin`; `slug.go` lives in `store`. `golang.org/x/text` is imported directly for accent stripping; it is already a transitive dependency of pgx, so the module graph does not grow.
- **Type consistency checked:** `page`, `render`, `renderStatus`, `renderPartial`, `cartView`, `buildCart`/`loadCart`, `checkoutData.Totals`, `sendOrderMails`, `orderURL`, `requireAdmin`/`adm`, `flashBack`, `pathID`, `img.WidthsFor`/`Filename`/`Remove`, `store.NextStatuses`, and every store method name match between the task that defines them and the tasks that use them.
