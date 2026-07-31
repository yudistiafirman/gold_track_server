CREATE TABLE stock_opnames (
    id            BIGSERIAL PRIMARY KEY,
    public_id     UUID        NOT NULL DEFAULT gen_random_uuid(),
    opname_code   VARCHAR(30) NOT NULL,
    opname_date   DATE        NOT NULL,
    status        VARCHAR(20) NOT NULL CHECK (status IN ('IN_PROGRESS', 'COMPLETED', 'CANCELLED')),
    notes         TEXT,
    created_by    BIGINT      NOT NULL REFERENCES users (id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_stock_opnames_opname_code UNIQUE (opname_code),
    CONSTRAINT uq_stock_opnames_public_id UNIQUE (public_id)
);
