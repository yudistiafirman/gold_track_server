CREATE TABLE expenses (
    id            BIGSERIAL PRIMARY KEY,
    public_id     UUID           NOT NULL DEFAULT gen_random_uuid(),
    category_id   BIGINT         NOT NULL REFERENCES expense_categories (id),
    amount        NUMERIC(15,2)  NOT NULL,
    description   VARCHAR(300),
    expense_date  DATE           NOT NULL,
    created_by    BIGINT         NOT NULL REFERENCES users (id),
    created_at    TIMESTAMPTZ    NOT NULL DEFAULT now(),
    CONSTRAINT uq_expenses_public_id UNIQUE (public_id)
);

CREATE INDEX idx_expenses_expense_date ON expenses (expense_date);
CREATE INDEX idx_expenses_category_id ON expenses (category_id);
