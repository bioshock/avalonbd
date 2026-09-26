# Avalon Foods redesign, content pages, and product API — design

Date: 2026-09-26 · Status: approved in brainstorming, awaiting spec review
Target: `avalonshop/` (the Go storefront at avalonbd.com). The static root
`index.html` is out of scope.

## 1. Goal

Turn the plain shop into the "Avalon Foods" storefront shown in the owner's
mockup (`~/Downloads/WhatsApp Image 2026-09-20 at 2,06,43 AM-Picsart-AiImageEnhancer.jpeg`),
launch the first four products, add the Phase 1 content pages the strategy
plan requires (`avalon-docs/00-strategy/avalon-plan.md` §10, §11, Appendix B),
and give the shop a token-protected JSON API so products can be managed from
scripts.

Success means:
- avalonbd.com home page matches the mockup's structure and feel, with all
  text, prices and buttons as real HTML (no text baked into images).
- The four products below are live with photos, bought through the normal
  cart and checkout.
- About, How it's made, Contact, Delivery & Returns, Privacy and Terms pages
  exist with real content.
- `docs/api.md` documents an API that a script used to create those products.

## 2. Constraints

- **Website checkout is the primary order path.** WhatsApp is a secondary
  option: always an outline button next to a solid cart/shop button, never
  instead of it.
- **Honesty rules (plan §11, Appendix B).** No Halal/HACCP/ISO/"100% Natural"
  badges, no ratings or review counts, no "sold" counts, no pillars, Vision
  2050, conglomerate framing or 2016–2050 timeline. "Made in Bangladesh" and
  "Product of Bangladesh" are allowed.
- Existing stack only: Go `html/template`, htmx, one CSS file, Postgres. No CSS
  framework, no build step, no new JS.
- Existing budgets in `internal/app/seo_test.go` (`TestPerformanceBudget`):
  HTML ≤ 30 KB gzipped per page, JS ≤ 25 KB gzipped, `app.css` ≤ 15 KB raw.
  The CSS cap is raised to 25 KB (see §7).

## 3. Products (final)

| Product | Category | Variant | Price | Regular price |
|---|---|---|---|---|
| Avalon Onion Powder | Spices & Powders | 100g | ৳130 | — |
| Avalon Garlic Powder | Spices & Powders | 100g | ৳180 | — |
| Avalon Ginger Powder | Spices & Powders | 100g | ৳240 | — |
| Essential Spice Trio | Combo | 3 × 100g | ৳499 | ৳550 |

The three powders are `featured`. The Trio is `promo`. Taglines come from the
mockup ("A convenient fine-ground onion ingredient for everyday cooking.",
"A convenient way to add garlic flavor and aroma to your favorite dishes.",
"Finely ground ginger for curries, soups, marinades and everyday recipes.",
"Three essential kitchen ingredients in one convenient 3-piece set.").
The Trio is a bundle price, not a launch discount, so it fits plan §0/§12.

The live shop currently has zero products and zero categories; nothing is removed.

## 4. Visual system

- Colors (CSS custom properties in `app.css`): leaf green `#1f5d2e` (primary
  buttons, headings), cream paper `#faf6ec` (section grounds), gold `#f3e7a1`
  (hero highlight word), deal red `#b3261e` (sale price only), plus the
  existing ink/muted/line tokens retuned to the warmer palette.
- Type: Lora (self-hosted, one variable woff2, preloaded) for h1–h3; Manrope
  stays for body. The handwritten hero note ("Good Ingredients, Brighter
  Meals") is a small inline SVG, not a font.
- Logo: cleaned SVG (§8) in header and footer; leaf-only favicon.
- Icons: inline SVG `<symbol>` sprite in the layout: leaf, bowl, shield-check,
  Bangladesh map pin, WhatsApp, Facebook, cart, search, arrow, check.

## 5. Pages

### 5.1 Layout (all store pages)

- Header: logo · Home · About · Products · How it's made · Contact · search
  link · **Shop Now** button · cart (existing htmx drawer). On narrow screens
  the nav collapses into a native `<details>` menu.
- Footer: logo + "Good Food. A Better Tomorrow.", links (Products, About,
  Contact, Delivery & Returns, Privacy, Terms), WhatsApp/phone/email, Facebook
  icon, "© 2026 Avalon Foods. All rights reserved." Other social icons are
  omitted until links exist.
- Contact constants live in one place (a `Site` struct passed to templates):
  - Phone/WhatsApp: +880 1933-309009 → `tel:+8801933309009`,
    `https://wa.me/8801933309009?text=<url-encoded message>`
  - Email: shop@avalonbd.com
  - Address: Katakhali, Rajshahi, Bangladesh
  - Hours: Sat–Thu, 10am–8pm
  - Facebook: https://www.facebook.com/share/1Lzv9YkGFs/

### 5.2 Home `/`

1. **Hero**: clean jars scene as background, dark-green gradient on the left
   for legibility. Heading "From Nature / to *Your Kitchen*" (gold), intro
   paragraph from the mockup, **Explore Products** (solid → `/products`) and
   **Order on WhatsApp** (outline). Row of four features: Carefully Selected
   Ingredients, Convenient Food Products, Thoughtful Processing, Made in
   Bangladesh.
2. **Our Essential Spices**: leaf-paper background, heading + subheading +
   intro from the mockup. One card per featured product (up to 3; grid wraps
   if more): photo, name, variant name, tagline, price (strikethrough when
   applicable), **View Product** and a quick **Add to cart** (single-variant
   products only; multi-variant show View only). Empty state keeps the section
   out entirely.
3. **Promo banner**: rendered only when a `promo` product exists. Wood
   background, product name, tagline, a checklist built from the product's
   description lines, price box (regular struck through, sale price large,
   "Save ৳X"), **Add to cart** (solid) and **Order on WhatsApp** (outline,
   message names the product). Trio box photo and the raw-ingredients plate as
   decoration.
4. **Why Avalon Foods?**: four icon + text blocks (copy from the mockup) and a
   side list "From Nature to Your Kitchen" (Raw materials → Careful processing
   → Quality & consistency → Thoughtful packaging → Your kitchen) linking to
   `/journey`.
5. **Sunset band**: landscape image, "Good Food. A Better Tomorrow." in the
   script SVG, **Explore All Products**.

### 5.3 Existing store pages

Products list, product detail, cart, cart drawer, checkout, order, login,
register, forgot, reset, account and 404 take the new tokens, fonts, header
and footer. Layouts stay as they are, except:
- Product card and product detail show `regular_price` struck through with
  "Save ৳X" when `regular_price > price`.
- Product detail adds an **Order on WhatsApp** outline button under Add to
  cart, with the product name in the message.

### 5.4 Content pages

Static templates under `templates/pages/`, served by one handler keyed by
slug, each with its own title, meta description and canonical URL, all added
to `sitemap.xml`.

| Path | Content |
|---|---|
| `/about` | Who we are: a small Rajshahi food business starting with everyday spice powders, made in small batches with carefully selected ingredients. What we make now. Why (convenience and consistency for home kitchens). Where (Katakhali, Rajshahi). Link to Contact and Shop. |
| `/journey` | "How it's made": sourcing → cleaning and drying → grinding → packing → delivery, each step one honest paragraph. No dates, timeline or capacity numbers. |
| `/contact` | Address, tap-to-call phone, WhatsApp link, email, hours, Facebook. No contact form (YAGNI; WhatsApp and email cover it). |
| `/delivery-returns` | Delivery: cash on delivery across Bangladesh; typical times Rajshahi 1–2 days, rest of Bangladesh 2–4 days; delivered by courier partners; charges by area shown as a table read live from `delivery_zones`. Returns: damaged or wrong item → tell us within 48 hours of delivery with a photo, we replace or refund; opened food items cannot otherwise be returned. |
| `/privacy` | Data collected (name, phone, address, email, order history), purpose (fulfil and support orders), session and cart cookies only, never sold, how to ask for deletion (email). |
| `/terms` | Ordering and confirmation call, prices in BDT including VAT where applicable, cash on delivery, cancellation before dispatch, product information accuracy, liability limits, governing law Bangladesh, contact. |

## 6. Data and API

### 6.1 Migration `0003_foods.sql`

```sql
alter table variants add column regular_price int check (regular_price >= 0);
alter table products add column promo boolean not null default false;
alter table products add column tagline text not null default '';
create unique index products_one_promo on products (promo) where promo;
```

The partial unique index guarantees at most one promo. The store's
create/update sets `promo = false` on all other products in the same
transaction before setting it on the target, so callers never hit the index.

`Variant.RegularPrice *int`, `Product.Promo bool`, `Product.Tagline string`
are threaded through `store` structs, card queries (`ProductCard` gains the
min-price variant's regular price and the tagline), the admin product form
(new fields: tagline, promo checkbox, regular price per variant row) and a new
`store.GetPromoCard`.

Validation: `regular_price` is optional; when present it must be ≥ 0. It is
only displayed when greater than `price`, so an equal or lower value is
harmless and allowed.

### 6.2 JSON API

Mounted under `/api/admin/`. Enabled only when `ADMIN_API_TOKEN` is set
(minimum 32 characters, else config fails at boot); when unset every
`/api/admin/*` path returns 404. Auth is `Authorization: Bearer <token>`,
compared with `crypto/subtle.ConstantTimeCompare`; failure returns 401. No
cookies, so CSRF does not apply and the session/CSRF middleware is skipped for
these routes. Requests are rate limited with the existing limiter.

| Method | Path | Body / result |
|---|---|---|
| GET | `/api/admin/categories` | `[{id, slug, name}]` |
| POST | `/api/admin/categories` | `{slug, name}` → 201 `{id}`; 409 if slug exists |
| GET | `/api/admin/products` | all products incl. inactive, with variants |
| GET | `/api/admin/products/{id}` | product + variants + images |
| POST | `/api/admin/products` | product JSON → 201 `{id, slug}` |
| PUT | `/api/admin/products/{id}` | full replace of fields and variants (same `UpdateProduct` path as the admin form; variants matched by `id`, missing ones removed) |
| DELETE | `/api/admin/products/{id}` | 204; image files removed |
| POST | `/api/admin/products/{id}/images` | multipart `images` (+ optional `alt`), same pipeline and limits as admin upload → `[{id, file}]` |
| DELETE | `/api/admin/images/{id}` | 204 |

Product JSON:

```json
{
  "slug": "onion-powder",            // optional; derived from name when empty
  "name": "Avalon Onion Powder",
  "tagline": "…",
  "description": "…",
  "meta_description": "…",
  "category_id": 1,
  "active": true,
  "featured": true,
  "promo": false,
  "variants": [
    {"id": 0, "name": "100g", "sku": "AV-ONION-100", "price": 130, "regular_price": null, "stock": 100}
  ]
}
```

Errors: `{"error": "message", "fields": {"variants[0].price": "must be 0 or more"}}`
with 400 (validation / bad JSON), 401, 404, 409 (duplicate slug or SKU), 413
(upload too large). Validation reuses the admin form's rules, extracted into a
shared function so the two paths cannot drift.

### 6.3 Docs and seed

- `avalonshop/docs/api.md`: setup (token), every endpoint with a `curl`
  example, error format, and the seed workflow.
- `avalonshop/scripts/seed/products.json` + images, and
  `avalonshop/scripts/seed-spices.sh` (bash + curl + jq): creates the two
  categories (Spices & Powders, Combo) if missing, skips products whose slug
  already exists, creates the rest, uploads images.
  Takes `BASE_URL` and `ADMIN_API_TOKEN` from the environment.

## 7. Assets

`avalonshop/scripts/slice-assets.py` (Pillow; re-runnable) crops from the two
Gemini sheets (`~/Downloads/Gemini_Generated_Image_hik8aphik8aphik8.jpeg`,
`…8ga8vo8ga8vo8ga8.jpeg`, both 1684×2528):

| Output | Source | Use |
|---|---|---|
| `static/img/hero-{960,1920}.webp` | sheet 2, hero scene | home hero, preloaded |
| `static/img/sunset-{960,1920}.webp` | sheet 2, landscape strip | sunset band, lazy |
| `static/img/paper.webp` | sheet 2, leaf paper swatch | tiled section ground |
| `static/img/wood.webp` | sheet 2, wood swatch | promo banner ground |
| `static/img/plate.webp` | sheet 1, ingredients plate, bg removed | promo decoration |
| `static/img/leaf.webp` | sheet 1, bay leaf, bg removed | corner decoration |
| `scripts/seed/{onion,garlic,ginger}.png` | sheet 1, jars, bg removed | product photos via API |
| `scripts/seed/trio.png` | sheet 1, gift box | Trio photo via API |

Background removal is a near-white flood fill from the crop edges with a
feathered alpha edge (the sheet backgrounds are flat white), not a model.

Known limits, accepted: label text on the jars is AI gibberish and the garlic
cap is green on sheet 1 but black in the hero. These are placeholders to be
replaced by real packaging photos through the API/admin with no code change.

Logo: from `~/Downloads/813244257_…_n.svg` remove the background `rect` and
all paths whose fill is near-white (all channels ≥ 0xF0), crop the viewBox to
the artwork bounds, run `svgo` (precision 1) → `static/logo.svg`, target
≤ 60 KB (source 322 KB). Leaf-only paths → `static/favicon.svg`.

Performance: CSS budget raised from 15 KB to 25 KB in `seo_test.go` with a
comment explaining why (new sections, not bloat). HTML and JS budgets are
unchanged and must still pass. Hero image is preloaded with `imagesrcset`;
all other images are `loading="lazy"` with width/height set.

## 8. Testing

Test-first, following existing test style (`storetest` for DB tests,
`httptest` for handlers).

- Store: `regular_price` round-trips through create/update/get; setting promo
  on one product clears it on others; `GetPromoCard` returns nothing when no
  promo; migration applies on a fresh DB.
- API: 404 when token unset; 401 on missing/wrong token; create → get → update
  → delete; duplicate slug → 409; validation errors → 400 with `fields`;
  image upload → 201 and image appears on product; categories create/list.
- Pages: each content page returns 200 with title and canonical, appears in
  the sitemap; delivery page lists zones from the DB; home shows promo banner
  only when a promo product exists; strikethrough renders only when
  `regular_price > price`; WhatsApp links are URL-encoded and point at
  `wa.me/8801933309009`.
- Budgets: `TestPerformanceBudget` passes for all public pages, including the
  new ones.
- Visual: Playwright screenshots of home, products, product, cart, checkout
  and the six content pages at 1280 px and 390 px, checked by eye against the
  mockup.

## 9. Rollout

1. Branch `feat/foods-redesign`; all tests green.
2. Owner sets `ADMIN_API_TOKEN` in Coolify (a generated 64-hex value is
   provided) and deploys.
3. Run `seed-spices.sh` against https://avalonbd.com; confirm four products
   and images are live.
4. Production smoke test: home → Trio → add to cart → checkout page renders.
   Stop before placing an order.
5. Add `web-access.txt` to `.gitignore` (done with this spec's commit).

## 10. Out of scope (follow-ups)

- ৳400 minimum order and free delivery at ৳1,000+ (plan §0, §12).
- Root static `index.html` brand page.
- Real product photography and final packaging.
- Contact form, reviews, other social links.
