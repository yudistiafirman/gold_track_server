ALTER TABLE stock_items DROP CONSTRAINT stock_items_status_check;
ALTER TABLE stock_items ADD CONSTRAINT stock_items_status_check
    CHECK (status IN ('AVAILABLE', 'SOLD'));
