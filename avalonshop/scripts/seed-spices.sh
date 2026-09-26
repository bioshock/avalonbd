#!/usr/bin/env bash
# Creates the launch catalog through the admin JSON API (docs/api.md).
# Safe to re-run: categories and products whose slug already exists are skipped.
#   BASE_URL=https://avalonbd.com ADMIN_API_TOKEN=... scripts/seed-spices.sh
set -euo pipefail
: "${BASE_URL:?set BASE_URL}" "${ADMIN_API_TOKEN:?set ADMIN_API_TOKEN}"
dir="$(cd "$(dirname "$0")" && pwd)/seed"
api() { curl -sS --fail-with-body -H "Authorization: Bearer $ADMIN_API_TOKEN" "$@"; }

cats="$(api "$BASE_URL/api/admin/categories")"
while read -r c; do
  slug="$(jq -r .slug <<<"$c")"
  if jq -e --arg s "$slug" 'any(.[]; .slug == $s)' <<<"$cats" >/dev/null; then
    echo "category $slug: exists"
  else
    api -X POST -H 'Content-Type: application/json' -d "$c" "$BASE_URL/api/admin/categories" >/dev/null
    echo "category $slug: created"
  fi
done < <(jq -c '.categories[]' "$dir/products.json")
cats="$(api "$BASE_URL/api/admin/categories")"

have="$(api "$BASE_URL/api/admin/products")"
while read -r p; do
  slug="$(jq -r .slug <<<"$p")"
  if jq -e --arg s "$slug" 'any(.[]; .slug == $s)' <<<"$have" >/dev/null; then
    echo "product $slug: exists, skipped"
    continue
  fi
  cid="$(jq --arg s "$(jq -r .category <<<"$p")" '.[] | select(.slug == $s) | .id' <<<"$cats")"
  body="$(jq --argjson cid "$cid" 'del(.category, .image, .image_alt) + {category_id: $cid}' <<<"$p")"
  id="$(api -X POST -H 'Content-Type: application/json' -d "$body" "$BASE_URL/api/admin/products" | jq .id)"
  api -F "images=@$dir/$(jq -r .image <<<"$p")" --form-string "alt=$(jq -r .image_alt <<<"$p")" \
    "$BASE_URL/api/admin/products/$id/images" >/dev/null
  echo "product $slug: created (id $id) with image"
done < <(jq -c '.products[]' "$dir/products.json")
