-- Site-wide switches the owner flips from /admin (e.g. hide the site from search engines).
create table settings (key text primary key, value text not null);
