CREATE TABLE customers (
    id         BIGSERIAL PRIMARY KEY,
    public_id  UUID        NOT NULL DEFAULT gen_random_uuid(),
    name       VARCHAR(200) NOT NULL,
    phone      VARCHAR(20),
    email      VARCHAR(200),
    id_type    VARCHAR(20) CHECK (id_type IN ('KTP', 'SIM', 'PASSPORT')),
    id_number  VARCHAR(50),
    address    TEXT,
    notes      TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_customers_public_id UNIQUE (public_id)
);
