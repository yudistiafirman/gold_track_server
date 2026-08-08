ALTER TABLE stock_items DROP CONSTRAINT stock_items_status_check;
ALTER TABLE stock_items ADD CONSTRAINT stock_items_status_check
    CHECK (status IN ('AVAILABLE', 'SOLD', 'VOID', 'ARCHIVED'));

-- stock_opname_items.system_status mirrors stock_items.status at scan time.
-- It only ever allowed ('AVAILABLE', 'SOLD') even after VOID was added to
-- stock_items (migration 000032), so scanning a VOID unit during a stock
-- opname session already violated this constraint — fixed here alongside
-- adding ARCHIVED so both non-AVAILABLE statuses a physically-present unit
-- can carry are representable.
ALTER TABLE stock_opname_items DROP CONSTRAINT stock_opname_items_system_status_check;
ALTER TABLE stock_opname_items ADD CONSTRAINT stock_opname_items_system_status_check
    CHECK (system_status IN ('AVAILABLE', 'SOLD', 'VOID', 'ARCHIVED'));
