CREATE TABLE transactions (
    id                BIGSERIAL PRIMARY KEY,
    public_id         UUID           NOT NULL DEFAULT gen_random_uuid(),
    transaction_code  VARCHAR(30)    NOT NULL,
    type              VARCHAR(15)    NOT NULL CHECK (type IN ('SELL', 'BUY', 'SELL_SUPPLIER')),
    customer_id       BIGINT         REFERENCES customers (id),
    supplier_id       BIGINT         REFERENCES suppliers (id),
    total_amount      NUMERIC(15,2)  NOT NULL,
    total_weight      NUMERIC(10,3)  NOT NULL,
    payment_method    VARCHAR(30)    NOT NULL CHECK (payment_method IN ('CASH', 'TRANSFER', 'QRIS')),
    payment_ref       VARCHAR(200),
    gold_price_id     BIGINT         REFERENCES gold_prices (id),
    notes             TEXT,
    invoice_url       VARCHAR(500),
    status            VARCHAR(20)    NOT NULL DEFAULT 'COMPLETED' CHECK (status IN ('COMPLETED', 'CANCELLED')),
    created_by        BIGINT         NOT NULL REFERENCES users (id),
    created_at        TIMESTAMPTZ    NOT NULL DEFAULT now(),
    completed_at      TIMESTAMPTZ,
    CONSTRAINT uq_transactions_transaction_code UNIQUE (transaction_code),
    CONSTRAINT uq_transactions_public_id UNIQUE (public_id)
);

CREATE INDEX idx_transactions_type ON transactions (type);
CREATE INDEX idx_transactions_created_at ON transactions (created_at);
