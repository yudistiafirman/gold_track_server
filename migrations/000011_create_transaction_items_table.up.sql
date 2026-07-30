CREATE TABLE transaction_items (
    id              BIGSERIAL PRIMARY KEY,
    transaction_id  BIGINT         NOT NULL REFERENCES transactions (id),
    stock_item_id   BIGINT         NOT NULL REFERENCES stock_items (id),
    product_name    VARCHAR(200)   NOT NULL,
    weight_gram     NUMERIC(10,3)  NOT NULL,
    price_per_gram  NUMERIC(15,2)  NOT NULL,
    price_total     NUMERIC(15,2)  NOT NULL,
    cogs            NUMERIC(15,2),
    created_at      TIMESTAMPTZ    NOT NULL DEFAULT now()
);
