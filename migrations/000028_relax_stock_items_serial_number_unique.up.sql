-- Serial numbers must stay unique among units currently in stock, but a
-- serial number should become reusable once its unit is SOLD (e.g. the
-- exact same physical item is bought back later) — SOLD rows are never
-- deleted (kept for transaction history), so the old global unique
-- constraint permanently locked out any serial number that was ever sold.
ALTER TABLE stock_items DROP CONSTRAINT uq_stock_items_serial_number;

CREATE UNIQUE INDEX uq_stock_items_serial_number_available
    ON stock_items (serial_number)
    WHERE status = 'AVAILABLE';
