CREATE TABLE purchase_orders (
    id            BIGSERIAL PRIMARY KEY,
    po_code       VARCHAR(30)    NOT NULL,
    supplier_id   BIGINT         NOT NULL REFERENCES suppliers (id),
    total_amount  NUMERIC(15,2)  NOT NULL,
    status        VARCHAR(20)    NOT NULL CHECK (status IN ('BELUM_DITERIMA', 'DITERIMA', 'DIBATALKAN')),
    notes         TEXT,
    created_by    BIGINT         NOT NULL REFERENCES users (id),
    created_at    TIMESTAMPTZ    NOT NULL DEFAULT now(),
    received_at   TIMESTAMPTZ,
    CONSTRAINT uq_purchase_orders_po_code UNIQUE (po_code)
);

CREATE INDEX idx_purchase_orders_status ON purchase_orders (status);
