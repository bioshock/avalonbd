-- Avalon Foods redesign: a crossed-out regular price per variant, a one-line
-- tagline per product, and at most one "promo" product for the home banner.
alter table variants add column regular_price int check (regular_price >= 0);
alter table products add column promo boolean not null default false;
alter table products add column tagline text not null default '';
-- At most one promo. Store.CreateProduct/UpdateProduct clear the old one in
-- the same transaction, so callers never hit this index.
create unique index products_one_promo on products (promo) where promo;
