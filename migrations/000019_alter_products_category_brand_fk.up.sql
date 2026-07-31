ALTER TABLE products DROP COLUMN category;
ALTER TABLE products DROP COLUMN brand;
ALTER TABLE products ADD COLUMN category_id BIGINT NOT NULL REFERENCES categories (id);
ALTER TABLE products ADD COLUMN brand_id BIGINT NOT NULL REFERENCES brands (id);
