CREATE TABLE stock_opnames (
    id            BIGSERIAL PRIMARY KEY,
    opname_code   VARCHAR(30) NOT NULL,
    opname_date   DATE        NOT NULL,
    status        VARCHAR(20) NOT NULL CHECK (status IN ('IN_PROGRESS', 'COMPLETED', 'CANCELLED')),
    notes         TEXT,
    created_by    BIGINT      NOT NULL REFERENCES users (id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_stock_opnames_opname_code UNIQUE (opname_code)
);
