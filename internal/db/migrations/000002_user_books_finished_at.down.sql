DROP INDEX IF EXISTS idx_user_books_finished_at;

ALTER TABLE user_books DROP COLUMN finished_at;
