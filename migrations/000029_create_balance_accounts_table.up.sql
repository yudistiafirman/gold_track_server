CREATE TABLE balance_accounts (
    id         BIGSERIAL PRIMARY KEY,
    public_id  UUID          NOT NULL DEFAULT gen_random_uuid(),
    name       VARCHAR(200)  NOT NULL,
    balance    NUMERIC(15,2) NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT uq_balance_accounts_name UNIQUE (name),
    CONSTRAINT uq_balance_accounts_public_id UNIQUE (public_id)
);
