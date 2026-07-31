CREATE TABLE expense_categories (
    id         BIGSERIAL PRIMARY KEY,
    public_id  UUID         NOT NULL DEFAULT gen_random_uuid(),
    name       VARCHAR(100) NOT NULL,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uq_expense_categories_name UNIQUE (name),
    CONSTRAINT uq_expense_categories_public_id UNIQUE (public_id)
);
