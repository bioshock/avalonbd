-- Task 21: keep a history of a product's old slugs so a rename can 301 the
-- old URL to the current one instead of losing search ranking and shared
-- links to a 404.
--
-- slug is the primary key: one retired slug names exactly one product. If a
-- slug is reused by a different product and later retired again, the write
-- side (Store.RecordOldSlug) upserts on this key so the newest owner wins
-- instead of erroring on the duplicate primary key.
create table product_slugs (
  slug       text primary key,
  product_id bigint not null references products(id) on delete cascade,
  created_at timestamptz not null default now()
);
create index product_slugs_product_idx on product_slugs (product_id);
