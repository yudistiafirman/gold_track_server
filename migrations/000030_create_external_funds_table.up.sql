CREATE TABLE external_funds (
    id          BIGSERIAL PRIMARY KEY,
    public_id   UUID          NOT NULL DEFAULT gen_random_uuid(),
    description VARCHAR(300)  NOT NULL,
    amount      NUMERIC(15,2) NOT NULL,
    created_at  TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT uq_external_funds_public_id UNIQUE (public_id)
);
