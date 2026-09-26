# Admin JSON API

A small API for managing the catalog from scripts. It does the same things as
the admin pages, with the same validation.

## Setup

The API is **off** unless `ADMIN_API_TOKEN` is set. Generate a token and put it
in the server's environment (Coolify → Environment Variables), then redeploy:

    openssl rand -hex 32

A token shorter than 32 characters stops the server at boot. When the variable
is unset, every `/api/admin/*` URL answers 404.

Every request sends the token as a bearer token:

    export BASE_URL=https://avalonbd.com
    export ADMIN_API_TOKEN=...   # the same value as on the server
    curl -H "Authorization: Bearer $ADMIN_API_TOKEN" "$BASE_URL/api/admin/products"

There are no cookies and no CSRF token. Requests are rate limited to 300 per
minute per IP address (429 after that).

## Errors

Errors are JSON with an HTTP status:

    {"error": "validation failed", "fields": {"variants[0].price": "must be 0 or more"}}

| Status | Meaning |
|---|---|
| 400 | Bad JSON, unknown field, validation failed (see `fields`), or an image that could not be processed |
| 401 | Missing or wrong token |
| 404 | No such product, image or endpoint (or the API is disabled) |
| 409 | Slug or SKU already used; or deleting a product that appears in orders |
| 413 | Request body too large |
| 429 | Too many requests |

Unknown JSON fields are rejected, so a typo like `regularPrice` is a 400
instead of being silently ignored.

## Categories

    # list
    curl -H "Authorization: Bearer $ADMIN_API_TOKEN" "$BASE_URL/api/admin/categories"
    # → [{"id":1,"slug":"spices-powders","name":"Spices & Powders"}]

    # create (slug is optional; derived from the name)
    curl -H "Authorization: Bearer $ADMIN_API_TOKEN" -H 'Content-Type: application/json' \
      -d '{"name":"Spices & Powders"}' "$BASE_URL/api/admin/categories"
    # → 201 {"id":1,"slug":"spices-powders"}; 409 if the slug exists

## Products

A product:

    {
      "slug": "onion-powder",
      "name": "Avalon Onion Powder",
      "tagline": "A convenient fine-ground onion ingredient for everyday cooking.",
      "description": "One paragraph per line.",
      "meta_description": "Up to 160 characters for search results.",
      "category_id": 1,
      "active": true,
      "featured": true,
      "promo": false,
      "variants": [
        {"id": 0, "name": "100g", "sku": "AV-ONION-100", "price": 130, "regular_price": null, "stock": 100}
      ]
    }

- `slug` is optional on create; it is made from the name. A slug that exists gives 409.
- `active`, `featured` and `promo` default to `false` when left out.
- `promo: true` makes this the home page banner product and clears it on any other product.
- `regular_price` is optional. When it is higher than `price`, the shop shows it crossed out with "Save ৳X".
- Prices are whole taka. Variant order in the array is the display order.
- GET responses also include `id` and `images` (`[{id, file, alt, width, height}]`).

    # list all products, including inactive ones
    curl -H "Authorization: Bearer $ADMIN_API_TOKEN" "$BASE_URL/api/admin/products"

    # get one
    curl -H "Authorization: Bearer $ADMIN_API_TOKEN" "$BASE_URL/api/admin/products/7"

    # create → 201 {"id":7,"slug":"onion-powder"}
    curl -H "Authorization: Bearer $ADMIN_API_TOKEN" -H 'Content-Type: application/json' \
      -d @product.json "$BASE_URL/api/admin/products"

    # replace → 200 with the saved product
    curl -X PUT -H "Authorization: Bearer $ADMIN_API_TOKEN" -H 'Content-Type: application/json' \
      -d @product.json "$BASE_URL/api/admin/products/7"

    # delete → 204 (409 if it appears in orders: set "active": false instead)
    curl -X DELETE -H "Authorization: Bearer $ADMIN_API_TOKEN" "$BASE_URL/api/admin/products/7"

`PUT` replaces every field. Variants are matched by `id`: send the existing
`id` to update a variant, `0` to add one, and leave a variant out to remove it.
An `id` that belongs to another product is a 400. The easiest way to edit is to
GET the product, change the JSON, and PUT it back (the `images` field is ignored
on PUT).

**`PUT` never changes the stock of an existing variant.** Whatever `stock`
you send for a variant that already has an `id` is ignored — the row keeps
whatever stock it had in the database. This protects against overwriting
order decrements: if a customer buys the last few units between your GET and
your PUT, a stale `stock` number in the JSON you send back can never revert
that sale. To restock or correct an existing variant's stock, use the admin
(`/admin/products`) instead. A *new* variant (`id: 0`) does get the stock you
send, and so does every variant when you `POST` a new product — the guard
only applies to updating a variant that already exists.

## Images

    # upload one or more images (JPEG, PNG, WebP or GIF, up to 10 files of 10 MB)
    curl -H "Authorization: Bearer $ADMIN_API_TOKEN" \
      -F images=@onion.png -F 'alt=Jar of Avalon Onion Powder' \
      "$BASE_URL/api/admin/products/7/images"
    # → 201 [{"id":12,"file":"3f9c…","alt":"Jar of Avalon Onion Powder","width":320,"height":480}]

    # delete → 204
    curl -X DELETE -H "Authorization: Bearer $ADMIN_API_TOKEN" "$BASE_URL/api/admin/images/12"

Images go through the same pipeline as the admin upload (resized WebP at up
to 400/900/1600 px wide). The pipeline only ever shrinks an image, never
enlarges it: a source narrower than 400 px — like the ~320 px jar cutouts
used for the launch catalog — comes back as a single size, at its own
original width and height, exactly as in the example above. A wider photo
comes back with the largest of 400/900/1600 that fits under its width; `GET`
the product afterwards to see every size an image has. If one file in a batch
fails, the response is 400 with the reason, and the files that did succeed
stay on the product. Reorder images in the admin.

## Seeding the launch catalog

`scripts/seed-spices.sh` creates the categories and the four launch products
from `scripts/seed/products.json`, and uploads their photos. It skips anything
whose slug already exists, so it is safe to run twice.

    BASE_URL=https://avalonbd.com ADMIN_API_TOKEN=... scripts/seed-spices.sh

Needs `bash`, `curl` and `jq`. If a product is created but its image upload
fails, upload the image in the admin (or delete the product and rerun).
