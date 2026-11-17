-- Sprint 15 billing: a milestone's price and its treasury invoice (amount, amount_pct, invoice_id,
-- invoice_number, invoice_ref). Read only, never filtered, so no index.
-- Modify "milestones" table
ALTER TABLE "milestones" ADD COLUMN "metadata" jsonb NULL;
