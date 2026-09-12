# Avalon Shop — Design Spec

Date: 2026-09-12
Status: approved in brainstorming, pending written review

## 1. Goal

A full-featured, self-hosted online store for Avalon Corporation (Rajshahi, Bangladesh). Small catalog (under 50 products), physical goods, cash on delivery, Bangladesh-wide delivery with admin-configured zones. Optional customer accounts. Built-in admin for products, images, orders, and zones. Deployed on a Coolify instance from the `avalonshop/` directory of this repo.

### Non-goals (deliberately skipped)

Coupons, product reviews, wishlist, Bangla locale, online payment gateway, address book, HEIC uploads, per-district delivery fees, analytics, multi-admin roles. None of these change the schema below except coupons, which would add a table.

## 2. Stack

- **Language**: Go 1.26, single binary, single process.
- **HTTP**: stdlib `net/http` with method + path patterns (`GET /products/{slug}`). No router library.
- **Templates**: stdlib `html/template`, embedded with `embed`. HTMX for partial swaps. No JS build step.
- **Database**: PostgreSQL 17 via `github.com/jackc/pgx/v5` (pool). Hand-written SQL. `ponytail: no sqlc, add when queries exceed ~50`.
- **Migrations**: plain `.sql` files embedded, applied in filename order by a ~30-line runner using a `schema_migrations(version text primary key)` table. Each file runs in its own transaction.
- **Images**: `golang.org/x/image` (draw for resize, webp decode), stdlib `image/jpeg`, `image/png`, `image/gif` for decode, `github.com/gen2brain/webp` for WebP encode (libwebp compiled to wasm under wazero, no cgo). EXIF orientation is read by a ~60-line parser in `internal/img` that finds the JPEG APP1 segment and TIFF tag 0x0112. No EXIF library.
- **Time**: stored UTC, displayed in `Asia/Dhaka`. The distroless image ships tzdata.
- **Passwords**: `golang.org/x/crypto/bcrypt`, cost 12.
- **CSRF**: stdlib `http.CrossOriginProtection` (Go 1.25+) wrapping the mux, plus `SameSite=Lax` on all cookies.
- **Email**: stdlib `net/smtp` with STARTTLS to Brevo's relay. Plain-text emails from `text/template`.
- **Logging**: stdlib `log/slog`, JSON to stdout.
- **CSS**: one hand-written `static/app.css` using custom properties. No Tailwind, no preprocessor.
- **Fonts**: Manrope (variable) and DM Mono self-hosted as two woff2 files in `static/fonts`.

Total third-party Go modules: `pgx`, `x/crypto`, `x/image`, `gen2brain/webp`.

## 3. Repository layout

```
avalonshop/
  Dockerfile
  docker-compose.yml
  go.mod, go.sum
  main.go                       config from env, db pool, migrations, mux, server, graceful shutdown
  migrations/0001_init.sql ...
  templates/
    layout.html                 store shell: head (SEO block), nav, cart badge, footer
    admin/layout.html           admin shell: sidebar nav
    store/*.html                home, products, product, cart, checkout, order, login, register, forgot, reset, account
    admin/*.html                dashboard, products, product_form, categories, orders, order, zones
    partials/*.html             cart_badge, cart_drawer, product_card, flash
    email/*.txt                 order_confirmation, order_new_admin, order_shipped, order_delivered, password_reset
  static/
    app.css, app.js, htmx.min.js, fonts/*.woff2, favicon.svg
  internal/
    config/    env parsing, fail fast on missing required vars
    store/     all SQL: products, variants, images, categories, zones, users, orders; slugify lives here
    app/       all HTTP handlers (storefront and admin share rendering, cookies, middleware), files split by area
    img/       decode → orient → strip → resize → webp
    mail/      smtp sender, template rendering, async send with logging
    money/     integer-taka formatting
    token/     HMAC signer for cart, session, reset, and tracking tokens
```

The existing `index.html`, `styles.css`, and `assets/` at the repo root are the retired coming-soon page. They are left untouched by this work.

## 4. Deployment (Coolify)

**docker-compose.yml**

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
      db: { condition: service_healthy }
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
volumes:
  uploads:
  pgdata:
```

**Dockerfile**: multi-stage. Stage 1 `golang:1.26-alpine` runs `go build -trimpath -ldflags="-s -w"` with `CGO_ENABLED=0`. Stage 2 `gcr.io/distroless/static-debian12` copies the binary. Exposes 8080. Templates, migrations, and static files are embedded, so the final image is the binary only.

**Coolify configuration**: Docker Compose build pack, base directory `/avalonshop`, compose path `/docker-compose.yml`. Coolify generates `SERVICE_FQDN_APP`, `SERVICE_PASSWORD_POSTGRES`, and `SERVICE_HEX_64_SESSION`. The SMTP, mail, and admin variables are entered once in the Coolify environment UI. Domain assigned in the Coolify UI. `GET /healthz` returns 200 for the proxy health check.

**First boot**: migrations run, then if no admin user exists one is created from `ADMIN_EMAIL` and `ADMIN_PASSWORD`. Both vars can be removed after that.

**DNS for email**: SPF, DKIM, and DMARC records for avalonbd.com as shown in the Brevo dashboard. Free tier is 300 emails per day.

## 5. Data model

All money is integer BDT (whole taka). All timestamps `timestamptz`. IDs are `bigint generated always as identity`.

```sql
categories      (id, slug unique, name, sort int default 0)
products        (id, slug unique, name, description text, category_id → categories,
                 meta_description text, active bool default true, featured bool default false,
                 created_at, updated_at)
variants        (id, product_id → products on delete cascade, name, sku text unique nullable,
                 price int check (price >= 0), stock int check (stock >= 0), sort int)
product_images  (id, product_id → products on delete cascade, file text, alt text,
                 width int, height int, sort int)
delivery_zones  (id, name, fee int check (fee >= 0), active bool default true, sort int)
users           (id, email text, password_hash, name, phone, address text,
                 -- unique index on lower(email); no citext extension needed
                 role text check (role in ('customer','admin')) default 'customer', created_at)
orders          (id, number text unique, user_id → users nullable, name, phone, email, address,
                 zone_name, delivery_fee int, subtotal int, total int,
                 status text check (status in ('new','confirmed','shipped','delivered','cancelled')) default 'new',
                 note text, admin_note text, created_at, updated_at)
order_items     (id, order_id → orders on delete cascade, variant_id → variants nullable on delete set null,
                 product_name, variant_name, unit_price int, qty int check (qty > 0))
schema_migrations (version text primary key)
```

Notes:

- Order `number` is `AV-` plus a zero-padded value from a Postgres sequence starting at 1001, e.g. `AV-001042`.
- Order items snapshot names and prices. Deleting a variant later does not corrupt history.
- `zone_name` and `delivery_fee` are snapshotted on the order; zones can be renamed or repriced later.
- Product `updated_at` drives `<lastmod>` in the sitemap.
- Index: `products(category_id)`, `variants(product_id)`, `product_images(product_id)`, `orders(status, created_at desc)`, `orders(user_id)`.

### Status transitions

`new → confirmed → shipped → delivered`. `cancelled` reachable from `new`, `confirmed`, or `shipped`. No other transitions; the admin handler rejects anything else.

### Stock

Decremented inside the order-placement transaction with `UPDATE variants SET stock = stock - $qty WHERE id = $id AND stock >= $qty`. If any row count is 0 the transaction rolls back and the customer sees which line is short. Cancelling an order restores stock in the same transaction as the status change.

## 6. Storefront

### Routes

| Method + path | Behavior |
|---|---|
| `GET /` | Hero, featured products (`featured = true`), category tiles |
| `GET /products` | Grid. Query `category=<slug>` filters, `q=` does `name ILIKE '%q%'`. Sorted by name. |
| `GET /products/{slug}` | Product page: gallery, variant pills, qty stepper, add to cart, description, delivery info, 4 related from same category |
| `GET /cart` | Full cart page with zone-independent subtotal |
| `POST /cart/items` | Body `variant_id, qty`. Adds or increments. Returns cart drawer partial for HTMX, redirect for plain form. |
| `POST /cart/items/{variant_id}` | Body `qty`. `qty = 0` removes. Same response rules. |
| `GET /checkout` | Form: name, phone, email, address, zone (radio list with fees), note. Prefilled from account if logged in. Shows line items, subtotal, delivery, total (updates with zone via HTMX). |
| `POST /checkout` | Validates, places order (see below), redirects to order page |
| `GET /orders/{number}` | Order status page. Requires `?t=<token>` for guests, or the owning logged-in user, or an admin. |
| `GET/POST /login` | Email + password. Rate-limited. Redirects to `next` or `/account`; admins to `/admin`. |
| `GET/POST /register` | Name, email, password (min 8). Logs in on success. |
| `POST /logout` | Clears session cookie |
| `GET/POST /forgot` | Sends reset email if the address exists. Always responds "if that address exists, we've sent a link". |
| `GET/POST /reset/{token}` | New password form |
| `GET /account` | Order history, editable saved name/phone/address |
| `POST /account` | Saves name/phone/address |
| `GET /media/{file}` | Serves from `UPLOAD_DIR`, `Cache-Control: public, max-age=31536000, immutable` |
| `GET /static/...` | Embedded assets, same long cache, filenames carry a build hash query string in templates |
| `GET /sitemap.xml` | Home, `/products`, each category, each active product with lastmod |
| `GET /robots.txt` | Allow all; disallow `/admin`, `/cart`, `/checkout`, `/account`, `/orders`; sitemap link |
| `GET /healthz` | `200 ok` |

### Cookies

All cookies are `HttpOnly`, `Secure` (when `BASE_URL` is https), `SameSite=Lax`, `Path=/`.

- **cart**: `base64(json [{v:variantID, q:qty}]) . hmac`. Max 20 lines, qty 1–99. Tampered or malformed cookie is treated as empty. No server-side cart table. `ponytail: cart in cookie, move to DB table if merge-on-login is ever needed`.
- **sess**: `userID . expiresUnix . hmac`. 30-day expiry. Missing, expired, or bad signature means logged out.
- **flash**: one-shot message, cleared on next render.

HMAC key is `SESSION_SECRET` (hex, 64 chars). `ponytail: one key for cart, session, reset, and tracking tokens; rotate all together`.

### Tokens

- **Guest order tracking** `t`: first 16 hex chars of `hmac(secret, "order:" + number)`. Sent in the confirmation email link and shown after checkout.
- **Password reset**: `userID . expiresUnix . hmac(secret, userID + expires + passwordHash[:12])`. Valid 1 hour. Changing the password changes the hash, which invalidates the token, so it is single-use without a table.

### Checkout flow

1. Validate fields: name (1–100), phone (Bangladeshi mobile, `01[3-9]\d{8}` after stripping spaces and `+88`), email (basic format), address (10–500), zone exists and active, cart non-empty.
2. In one transaction: load variants with `FOR UPDATE`, verify each is active with stock, decrement stock, insert order with snapshot fields, insert items, commit.
3. Clear cart cookie.
4. Send two emails asynchronously: confirmation to the customer with tracking link, new-order alert to `ORDER_NOTIFY_EMAIL` with items, totals, phone, and address. Failures are logged with the order number and never affect the response.
5. Redirect to `/orders/{number}?t=<token>` with a success flash.

Rate limit on `POST /checkout`: 10 per hour per IP, in-memory. Resets on restart.

### HTMX behavior

- Add-to-cart and qty changes return `partials/cart_drawer.html` and an out-of-band swap for `partials/cart_badge.html`.
- Zone radio change on checkout returns the totals block.
- Every HTMX endpoint checks the `HX-Request` header. Without it, the same handler redirects to the full page, so everything works with JS disabled.

### `app.js` (under 100 lines)

Gallery thumbnail click swaps the main image. Variant pill (a styled radio input) updates the price and stock text from `data-` attributes. Qty stepper buttons adjust the number input. Cart drawer open/close. Nothing else.

## 7. Admin

Mounted at `/admin`. Middleware: session user must have `role = 'admin'`, otherwise 404 (not 403, to avoid revealing the path). Login uses the same `/login` page.

| Path | Behavior |
|---|---|
| `GET /admin` | Counts: new orders, low-stock variants (`stock <= 5`). Last 10 orders. |
| `GET /admin/products` | Table: image thumb, name, category, variant count, active, featured. |
| `GET/POST /admin/products/new`, `GET/POST /admin/products/{id}` | One form: name, slug (auto from name, editable, uniqueness enforced), category, description, meta description, active, featured. Variants as repeating rows (name, sku, price, stock, sort) with add/remove via HTMX. Images section: multi-file upload input, thumbnails with alt text field, sort via up/down buttons, delete button. Save is one POST; images upload separately via `POST /admin/products/{id}/images` so the product must be saved once before images. |
| `POST /admin/products/{id}/images` | Multipart, up to 10 files, each max 10 MB. Runs the image pipeline. Returns updated images partial. |
| `POST /admin/images/{id}` | Update alt, sort. `POST /admin/images/{id}/delete` removes the row and the three files. |
| `POST /admin/products/{id}/delete` | Refuses if the product has order items; otherwise deletes product, variants, images, and files. |
| `GET/POST /admin/categories` | Inline list with add/edit/delete. Delete refused if products reference it. |
| `GET /admin/orders?status=` | Table filtered by status (default `new`), newest first. Columns: number, name, phone, zone, total, status, age. |
| `GET /admin/orders/{id}` | Full order, items, customer details, timeline, admin note. Buttons for allowed transitions only. |
| `POST /admin/orders/{id}/status` | Body `status`. Validates transition. `shipped` and `delivered` email the customer. `cancelled` restores stock. |
| `POST /admin/orders/{id}/note` | Saves admin note |
| `GET/POST /admin/zones` | Inline list: name, fee, active, sort. |

Login rate limit: 5 failed attempts per email-or-IP per 15 minutes, in-memory map with periodic sweep.

## 8. Image pipeline

On each uploaded file:

1. Read up to 10 MB. Sniff content type with `http.DetectContentType`; accept only `image/jpeg`, `image/png`, `image/gif`, `image/webp`. Anything else, including HEIC, is rejected with "Please upload JPEG, PNG, WebP or GIF".
2. Decode with `image.Decode`. GIF uses the first frame.
3. Apply EXIF orientation for JPEG (tag 0x0112, values 1–8, rotate/flip accordingly). All metadata is dropped because we re-encode from pixels.
4. Generate widths 400, 900, 1600. Never upscale: targets wider than the original are skipped, and if the original is narrower than 1600 one extra variant at the original width is produced. The largest produced is recorded as `width`/`height` on `product_images`, and the set of existing variants is derived from that width (every standard width below it, plus itself). Height follows aspect ratio. Resize with `draw.CatmullRom`.
5. Encode each as WebP, quality 82. Filenames `{random16hex}-{w}.webp` in `UPLOAD_DIR`. `product_images.file` stores the `{random16hex}` stem.
6. Insert the row with `sort = max(sort) + 1`.

Storefront `<img>` uses `srcset` of the produced widths, `sizes` per context, `width`/`height` attributes from the row, `loading="lazy"` for everything except the first product-page image, and `decoding="async"`.

WebP only, no JPEG fallback. All current browsers decode WebP.

## 9. SEO

Every store page sets: `<title>` (`{Product} · Avalon`), `<meta name="description">` (product `meta_description`, falling back to the first 155 characters of `description`), `<link rel="canonical">` built from `BASE_URL`, Open Graph `title`, `description`, `image` (first product image at 1600), `url`, `type`. Product pages add JSON-LD:

```json
{ "@type": "Product", "name": ..., "image": [...], "description": ...,
  "sku": ..., "brand": {"@type": "Brand", "name": "Avalon"},
  "offers": { "@type": "Offer", "priceCurrency": "BDT", "price": ...,
              "availability": "https://schema.org/InStock", "url": ... } }
```

One `Offer` per variant with the lowest-price variant first. Headings are semantic (`h1` once per page). Category and product slugs are lowercase ASCII with hyphens. Sitemap and robots as in the route table. Inactive products return 404 and are excluded from the sitemap.

## 10. Design system ("Greenhouse")

Chosen in the visual companion. Conventional modern shop feel, white ground, green accent, rounded cards, pill buttons. Manrope and DM Mono carry over from the brand page.

### Tokens

```css
:root {
  --ground: #ffffff;   --tint: #eef4ef;     --ink: #14201c;
  --muted: #6b7f72;    --line: #e6ebe8;     --accent: #1f6b3a;
  --accent-ink: #ffffff; --foot: #14201c;  --foot-ink: #c8d3cc;
  --danger: #b3261e;   --ok: #1f6b3a;
  --radius: 10px;      --radius-pill: 999px;
  --font: Manrope, system-ui, sans-serif;
  --mono: "DM Mono", ui-monospace, monospace;
}
```

### Type

- Headings: Manrope 600, tight letter-spacing (-0.03em to -0.04em), `h1` clamp(1.8rem, 4vw, 2.6rem).
- Body: Manrope 400, 1rem, line-height 1.55.
- Eyebrow labels (category, "Size", section headers): DM Mono 500, 0.7rem, uppercase, 0.12em tracking, `--accent`.
- Prices, SKUs, order numbers, quantities: DM Mono 500, no uppercase, `--accent` for prices.
- Currency shown as `৳ 1,200` with a thin space, Bengali taka sign, comma thousands.

### Components

- **Nav**: white, bottom hairline, brand wordmark left, links + cart badge right. Sticky. Collapses to wordmark + cart + menu button under 640px.
- **Hero** (home): `--tint` ground, eyebrow, big heading, one-line subcopy, one pill button.
- **Product card**: hairline border, `--radius`, square image on top, name (600), price line (mono accent) with variant hint muted. Whole card is the link. Hover lifts the image 2px.
- **Product page**: layout A from the companion. Two columns above 900px (gallery 1.1fr, details 1fr), details column sticky. Single column below. Gallery: square main image with thumbnail row. Variant picker: radio inputs styled as pills, selected is `--ink` filled, out of stock is struck through and disabled. Qty stepper: pill-bordered `- n +`. Primary button: `--accent` pill. Delivery info block: 2-column mono meta under a hairline.
- **Cart drawer**: slides from right, 380px, list of lines with thumb, name, variant, qty stepper, line total, subtotal, "Checkout" primary button.
- **Forms**: labels above inputs, 1px `--line` border, `--radius` 8px, focus ring in `--accent`. Errors in `--danger` under the field.
- **Footer**: `--foot` ground, `--foot-ink` text, mono, "Avalon Corporation · Building towards 2050", contact phone, links.
- **Admin**: same tokens, denser spacing, left sidebar (collapses to top bar on mobile), plain tables with hairlines, status badges as small pills colored by status. No separate theme.

### Grid

Products: 2 columns under 640px, 3 to 1024px, 4 above. Max content width 1200px. Side gutters 1rem mobile, 2rem desktop.

### Accessibility basics

Focus visible on all interactive elements. Color contrast at least 4.5:1 for text (`--muted` on white passes). Variant pills and qty stepper are real form controls. Images always have `alt` from admin. Cart drawer traps focus and closes on Escape. `prefers-reduced-motion` disables the drawer slide and card hover.

## 11. Security

- Parameterized SQL everywhere.
- bcrypt cost 12. Login compares even for unknown emails to keep timing uniform.
- CSRF via `http.CrossOriginProtection`; all state changes are POST.
- Cookies as in section 6. `SESSION_SECRET` required, at least 32 bytes.
- Uploads: size cap, content sniffing, re-encode from pixels, random filenames, served from a dedicated route with no directory listing.
- Admin routes 404 for non-admins. Order pages require token, owner, or admin.
- Rate limits on login, forgot-password, and checkout.
- `html/template` escaping throughout. Product descriptions are plain text with line breaks preserved. `ponytail: no rich text, add a sanitizer if markdown is ever wanted`.
- Security headers: `X-Content-Type-Options: nosniff`, `Referrer-Policy: strict-origin-when-cross-origin`, `Content-Security-Policy: default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'`. Inline JSON-LD uses a `<script type="application/ld+json">` which CSP allows since it is not executed.
- Request body limit 1 MB except upload route (110 MB for 10 files).
- Server timeouts: read header 5s, read 30s, write 60s, idle 120s.

## 12. Testing

Stdlib `testing` only. Run with `go test ./...`.

Unit tests, always run:

- Cart cookie encode/decode round-trip, tamper detection, cap enforcement.
- Session cookie round-trip and expiry.
- Reset token: valid, expired, invalidated by password change.
- Totals: subtotal, delivery, total for a sample cart and zone.
- Slugify: unicode, spaces, duplicates get `-2` suffix.
- Phone normalization: `+8801712345678`, `01712 345678`, invalid forms.
- Image pipeline: 2000×1500 PNG fixture produces three WebP files with correct widths; 600×600 produces the 400 variant plus a 600 variant (the original width, never upscaled) and records 600×600.
- Status transition table.

Integration tests, run when `TEST_DATABASE_URL` is set, else skipped:

- Migrations apply cleanly to an empty database and are idempotent on second run.
- Order placement decrements stock, rejects oversell, restores on cancel. Two concurrent placements for the last unit: exactly one succeeds.
- Handler smoke via `httptest`: home 200, product 200, unknown slug 404, admin route 404 when logged out, checkout POST creates an order.

README documents the one-liner: `docker run --rm -e POSTGRES_PASSWORD=test -p 5433:5432 postgres:17-alpine`.

## 13. Local development

`go run .` with a `.env` (loaded by a 10-line parser, not a library) pointing at local Postgres. `UPLOAD_DIR=./data/uploads`. Emails print to stdout when `SMTP_HOST` is empty. The admin seed runs the same way as production.
