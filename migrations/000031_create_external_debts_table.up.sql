CREATE TABLE external_debts (
    id          BIGSERIAL PRIMARY KEY,
    public_id   UUID          NOT NULL DEFAULT gen_random_uuid(),
    debtor_name VARCHAR(200)  NOT NULL,
    amount      NUMERIC(15,2) NOT NULL,
    created_at  TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT uq_external_debts_public_id UNIQUE (public_id)
);
