# Avalon Shop

Go monolith storefront + admin. See `../docs/superpowers/specs/2026-09-12-avalonshop-design.md`.

## Run locally

    docker run --rm -d --name avalon-pg -e POSTGRES_USER=avalon -e POSTGRES_PASSWORD=avalon -e POSTGRES_DB=avalon -p 5432:5432 postgres:17-alpine
    cp .env.example .env   # edit ADMIN_* at least
    go run .

Open http://localhost:8080. With `SMTP_HOST` empty, emails are printed to stdout.

## Test

    go test ./...                                   # unit tests

The integration tests are gated on `TEST_DATABASE_URL` and, when it is set, **drop and recreate
the `public` schema** of whatever database that URL points at before every run. Never point it at
a database with real data — including your local dev database. Use a disposable container:

    docker run --rm -d --name avalon-test-pg -e POSTGRES_PASSWORD=test -e POSTGRES_USER=avalon -e POSTGRES_DB=avalon -p 5433:5432 postgres:17-alpine
    TEST_DATABASE_URL='postgres://avalon:test@127.0.0.1:5433/avalon?sslmode=disable' go test -count=1 -p 1 ./...

`-p 1` is mandatory on every DB-backed test invocation, not optional: test packages share the one
Postgres schema above and drop it on setup, so running packages in parallel makes failures
intermittent.

## Deploy on Coolify

1. **New resource → Docker Compose**, pick this repository and branch.
   - Base directory: `/avalonshop`
   - Docker Compose file: `/docker-compose.yml`
2. Coolify generates `SERVICE_FQDN_APP`, `SERVICE_PASSWORD_POSTGRES`, and `SERVICE_HEX_64_SESSION`. Leave them.
3. Add these environment variables in the Coolify UI:
   - `SMTP_USER`, `SMTP_PASS` — from Brevo → SMTP & API → SMTP (login is your Brevo account email, password is the SMTP key)
   - `MAIL_FROM` — e.g. `shop@avalonbd.com` (must be a verified sender in Brevo, and must be a **bare
     address with no display name** — it doubles as the SMTP envelope sender, so pasting Brevo's own
     `Avalon Corporation <shop@avalonbd.com>` display form will fail at boot instead of sending mail)
   - `ORDER_NOTIFY_EMAIL` — where new-order alerts go
   - `ADMIN_EMAIL`, `ADMIN_PASSWORD` — the first admin; **remove both after the first successful login**
4. On the `app` service, set the domain (e.g. `https://avalonbd.com`). `BASE_URL` follows it automatically.
5. Set the `app` service's memory limit to **at least 1 GB** — see "Resource requirements" below for why.
6. Deploy. Health check is built into the image; the proxy waits for it.
7. DNS at your registrar:
   - `A` / `CNAME` for the shop domain → the Coolify server.
   - SPF, DKIM, DMARC records for `avalonbd.com` exactly as Brevo shows under Senders & Domains → Domains. Deliverability depends on these.
8. Optional but recommended: put the domain behind Cloudflare (proxied). It adds Brotli, edge caching of `/static` and `/media` (they are `immutable`), and DDoS protection for free.

### First-run checklist

- `/healthz` returns `ok`.
- Log in at `/login` with the admin credentials → lands on `/admin`.
- Add at least one **delivery zone** (customers can't check out without one).
- Add categories, then products with photos. The first photo is the listing image; fill in alt text.
- Place a test order as a guest; confirm both emails arrive (customer confirmation, admin alert).
- Mark it confirmed → shipped → delivered from `/admin/orders`; confirm the shipped/delivered emails.
- Check `/sitemap.xml` and `/robots.txt`, then submit the sitemap in Google Search Console.
- Run PageSpeed Insights on the home and a product page: Performance ≥ 95, SEO 100.

### Resource requirements

**Minimum 1 GB of container memory.** This is measured, not estimated: `internal/img` decodes an
uploaded photo and then allocates further full-size buffers while resizing and correcting
orientation, so the peak is several times the decoded image's own size.

- A 29.8 MP image peaked at **547 MB** resident for a JPEG and **899 MB** for a PNG — which is why
  uploads are capped at 16 MP, not higher.
- At the shipped 16 MP cap, a real 4600×3450 upload measured a **413 MB** peak.
- Uploads within one request are processed **sequentially**, one file at a time, so a 10-file
  upload does not multiply this figure — the peak is per-file, not per-request.

A 512 MB container will be killed by the OOM reaper on a legitimate large photo, and the failure
looks like the whole storefront going down rather than one upload failing.

The two upload caps also **interact** and neither is sufficient alone: the 10 MB per-file size cap
and the 16 MP pixel cap are both required, because the reachable worst case is a highly
compressible image that is under both limits at once — a 36-megapixel decompression bomb
compresses to as little as 291 KB.

### Known limitations

- **Concurrent product edits are last-write-wins.** `store.UpdateProduct` has no optimistic
  concurrency control: if two admins save the same product at the same time, the later commit
  silently discards the earlier one's entire edit — name, description, meta description, variants.
  This is deliberate for a small admin panel, not an oversight; a real fix needs a version column
  plus a conflict UI. The one part of a save that *is* safe under this race is slug history: the
  retired slug is captured under the product row's own lock inside the rename transaction, so two
  concurrent renames of the same product both get recorded correctly.
- **Stock is the one field of that race with its own guard, and even it has a residual case.**
  `syncVariants` only writes a variant's stock column when the submitted value differs from the
  value the edit form was rendered with; if it matches, the admin didn't touch that field, and the
  save leaves stock alone so an order placed while the form sat open isn't silently overwritten
  back to its stale value. The residual case: an admin who *deliberately* edits stock on the same
  variant an order lands against, at the same moment, is still last-write-wins between that save and
  the order's decrement, exactly like every other field above.
- **Category slugs have no rename history.** Renaming a category changes its listing URL
  (`/products?category=<slug>`); the old URL simply 404s. Products don't have this problem —
  renaming a product records the old slug, and its old URL 301s permanently to the new one — but
  that redirect history was scoped to products only. A category's listing URL is `rel=canonical`
  and so is indexable, meaning a category rename can cost its existing search ranking. Not
  implemented; this is a scope decision, not an oversight — ask if it's needed.

### Backups

Postgres data lives in the `pgdata` volume and uploads in `uploads`. Enable Coolify's scheduled database backups for the `db` service, and back up the `uploads` volume with the server's regular volume backups.