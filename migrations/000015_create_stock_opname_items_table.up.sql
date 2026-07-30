CREATE TABLE stock_opname_items (
    id               BIGSERIAL PRIMARY KEY,
    opname_id        BIGINT      NOT NULL REFERENCES stock_opnames (id),
    stock_item_id    BIGINT      NOT NULL REFERENCES stock_items (id),
    system_status    VARCHAR(20) NOT NULL CHECK (system_status IN ('AVAILABLE', 'SOLD')),
    physical_status  VARCHAR(20) CHECK (physical_status IN ('FOUND', 'NOT_FOUND')),
    result           VARCHAR(20) NOT NULL CHECK (result IN ('MATCH', 'MISSING', 'UNEXPECTED')),
    notes            TEXT
);
