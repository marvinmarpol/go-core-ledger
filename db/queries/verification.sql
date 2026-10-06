-- name: GetBalanceMismatches :many
-- Verification job: returns accounts whose stored balance diverges from the
-- sum of their postings. Any row here is a ledger invariant violation.
SELECT
    a.id,
    a.currency,
    a.balance                                                         AS stored_balance,
    COALESCE(SUM(
        CASE WHEN p.direction = 'DEBIT' THEN -p.amount
             ELSE p.amount END
    ), 0)::bigint                                                     AS computed_balance
FROM accounts a
LEFT JOIN postings p ON p.account_id = a.id
GROUP BY a.id, a.currency, a.balance
HAVING a.balance <> COALESCE(SUM(
    CASE WHEN p.direction = 'DEBIT' THEN -p.amount
         ELSE p.amount END
), 0);
