CREATE UNIQUE INDEX uq_stock_opnames_single_in_progress ON stock_opnames (status) WHERE status = 'IN_PROGRESS';
