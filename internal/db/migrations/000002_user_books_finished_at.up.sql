ALTER TABLE user_books ADD COLUMN finished_at DATETIME;

-- Backfill existing read books so they still appear in by-year stats.
UPDATE user_books SET finished_at = updated_at WHERE status = 'read';

CREATE INDEX IF NOT EXISTS idx_user_books_finished_at ON user_books(user_id, finished_at);
