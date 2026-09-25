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