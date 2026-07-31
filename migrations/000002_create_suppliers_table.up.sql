CREATE TABLE suppliers (
    id         BIGSERIAL PRIMARY KEY,
    public_id  UUID         NOT NULL DEFAULT gen_random_uuid(),
    name       VARCHAR(200) NOT NULL,
    phone      VARCHAR(20),
    address    TEXT,
    notes      TEXT,
    is_active  BOOLEAN      NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uq_suppliers_public_id UNIQUE (public_id)
);
