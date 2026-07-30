CREATE TABLE gold_prices (
    id               BIGSERIAL PRIMARY KEY,
    price_buy        NUMERIC(15,2) NOT NULL,
    price_sell       NUMERIC(15,2) NOT NULL,
    price_reference  NUMERIC(15,2),
    spread           NUMERIC(15,2),
    effective_date   DATE          NOT NULL,
    effective_from   TIMESTAMPTZ   NOT NULL,
    effective_until  TIMESTAMPTZ,
    is_active        BOOLEAN       NOT NULL DEFAULT true,
    source           VARCHAR(100),
    notes            TEXT,
    created_by       BIGINT        NOT NULL REFERENCES users (id),
    created_at       TIMESTAMPTZ   NOT NULL DEFAULT now()
);

CREATE INDEX idx_gold_prices_is_active ON gold_prices (is_active);
