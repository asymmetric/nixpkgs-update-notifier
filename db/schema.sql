CREATE TABLE IF NOT EXISTS subscriptions (
  id INTEGER PRIMARY KEY,
  roomid TEXT NOT NULL,
  mxid TEXT NOT NULL,
  attr_path TEXT NOT NULL,
  FOREIGN KEY(attr_path) REFERENCES packages(attr_path)
) STRICT;

CREATE TABLE IF NOT EXISTS packages (
  attr_path TEXT PRIMARY KEY,
  last_visited TEXT,
  error INTEGER NOT NULL DEFAULT 0
) STRICT;

CREATE TRIGGER IF NOT EXISTS ensure_packages_last_visited_set BEFORE INSERT ON subscriptions
  BEGIN
    SELECT CASE
      WHEN (SELECT last_visited FROM packages WHERE attr_path = NEW.attr_path) IS NULL THEN
        RAISE(ABORT, 'Insert aborted: last_visited is NULL')
      END;
  END;
