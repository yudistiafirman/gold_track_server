ALTER TABLE stock_opname_items DROP CONSTRAINT stock_opname_items_system_status_check;
ALTER TABLE stock_opname_items ADD CONSTRAINT stock_opname_items_system_status_check
    CHECK (system_status IN ('AVAILABLE', 'SOLD'));

ALTER TABLE stock_items DROP CONSTRAINT stock_items_status_check;
ALTER TABLE stock_items ADD CONSTRAINT stock_items_status_check
    CHECK (status IN ('AVAILABLE', 'SOLD', 'VOID'));
