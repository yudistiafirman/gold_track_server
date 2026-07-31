CREATE TABLE categories (
    id           BIGSERIAL PRIMARY KEY,
    public_id    UUID         NOT NULL DEFAULT gen_random_uuid(),
    name         VARCHAR(100) NOT NULL,
    is_active    BOOLEAN      NOT NULL DEFAULT true,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uq_categories_public_id UNIQUE (public_id)
);
CREATE UNIQUE INDEX uq_categories_name_ci ON categories (lower(name));
