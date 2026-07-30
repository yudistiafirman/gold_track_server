CREATE TABLE stock_items (
    id              BIGSERIAL PRIMARY KEY,
    product_id      BIGINT         NOT NULL REFERENCES products (id),
    barcode         VARCHAR(50)    NOT NULL,
    serial_number   VARCHAR(100)   NOT NULL,
    condition       VARCHAR(10)    NOT NULL CHECK (condition IN ('GOOD', 'BAD')),
    purchase_price  NUMERIC(15,2)  NOT NULL,
    purchase_date   DATE           NOT NULL,
    supplier_id     BIGINT         REFERENCES suppliers (id),
    po_id           BIGINT         REFERENCES purchase_orders (id),
    status          VARCHAR(20)    NOT NULL CHECK (status IN ('AVAILABLE', 'SOLD')),
    sold_at         TIMESTAMPTZ,
    notes           TEXT,
    created_by      BIGINT         NOT NULL REFERENCES users (id),
    created_at      TIMESTAMPTZ    NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ    NOT NULL DEFAULT now(),
    CONSTRAINT uq_stock_items_barcode UNIQUE (barcode)
);

CREATE INDEX idx_stock_items_status ON stock_items (status);
CREATE INDEX idx_stock_items_condition ON stock_items (condition);
CREATE INDEX idx_stock_items_product_id ON stock_items (product_id);
