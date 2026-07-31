CREATE TABLE products (
    id           BIGSERIAL PRIMARY KEY,
    public_id    UUID           NOT NULL DEFAULT gen_random_uuid(),
    name         VARCHAR(200)   NOT NULL,
    sku          VARCHAR(50)    NOT NULL,
    category     VARCHAR(50)    NOT NULL CHECK (category IN ('BATANGAN', 'KOIN', 'PERHIASAN', 'GALERI')),
    brand        VARCHAR(100)   NOT NULL,
    weight_gram  NUMERIC(10,3)  NOT NULL,
    description  TEXT,
    is_active    BOOLEAN        NOT NULL DEFAULT true,
    created_by   BIGINT         NOT NULL REFERENCES users (id),
    created_at   TIMESTAMPTZ    NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ    NOT NULL DEFAULT now(),
    CONSTRAINT uq_products_sku UNIQUE (sku),
    CONSTRAINT uq_products_public_id UNIQUE (public_id)
);
