-- GET /library?status=... filters by (user_id, status); the existing
-- idx_user_books_finished_at covers (user_id, finished_at) and does not help.
CREATE INDEX IF NOT EXISTS idx_user_books_user_status
    ON user_books(user_id, status);
