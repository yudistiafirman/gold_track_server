CREATE TABLE customers (
    id         BIGSERIAL PRIMARY KEY,
    name       VARCHAR(200) NOT NULL,
    phone      VARCHAR(20),
    email      VARCHAR(200),
    id_type    VARCHAR(20) CHECK (id_type IN ('KTP', 'SIM', 'PASSPORT')),
    id_number  VARCHAR(50),
    address    TEXT,
    notes      TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
