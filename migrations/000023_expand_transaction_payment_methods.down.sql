ALTER TABLE transactions DROP CONSTRAINT transactions_payment_method_check;
ALTER TABLE transactions ADD CONSTRAINT transactions_payment_method_check
    CHECK (payment_method IN ('CASH', 'TRANSFER', 'QRIS'));
