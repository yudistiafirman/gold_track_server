CREATE TABLE purchase_order_items (
    id              BIGSERIAL PRIMARY KEY,
    po_id           BIGINT         NOT NULL REFERENCES purchase_orders (id),
    product_id      BIGINT         NOT NULL REFERENCES products (id),
    quantity        INTEGER        NOT NULL CHECK (quantity > 0),
    purchase_price  NUMERIC(15,2)  NOT NULL
);
