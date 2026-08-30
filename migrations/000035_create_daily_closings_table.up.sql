CREATE TABLE daily_closings (
    id               BIGSERIAL PRIMARY KEY,
    public_id        UUID          NOT NULL DEFAULT gen_random_uuid(),
    closing_date     DATE          NOT NULL,
    total_balance    NUMERIC(15,2) NOT NULL,
    total_gold_value NUMERIC(15,2) NOT NULL,
    total_saldo      NUMERIC(15,2) NOT NULL,
    created_by       BIGINT        REFERENCES users(id),
    created_at       TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT uq_daily_closings_closing_date UNIQUE (closing_date),
    CONSTRAINT uq_daily_closings_public_id UNIQUE (public_id)
);
