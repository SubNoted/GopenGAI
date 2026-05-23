package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

// Open opens a SQLite database at the given path and configures it with
// recommended pragmas for performance and safety. Uses ncruces/go-sqlite3
// (pure Go, no CGo).
//
// Pragma settings:
//   - foreign_keys=ON       — enforce referential integrity
//   - journal_mode=WAL      — concurrent reads + writes
//   - page_size=4096        — default page size
//   - cache_size=-8000      — ~8 MB page cache (negative = KB)
//   - synchronous=NORMAL    — balance safety/speed with WAL
//   - busy_timeout=5000     — wait up to 5s on locked database
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Apply recommended pragmas
	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA page_size = 4096",
		"PRAGMA cache_size = -8000",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA busy_timeout = 5000",
	}

	for _, p := range pragmas {
		if _, err := db.ExecContext(context.Background(), p); err != nil {
			db.Close()
			return nil, fmt.Errorf("set pragma %q: %w", p, err)
		}
	}

	// Connection pooling: SQLite is single-writer, so limit connections.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)

	return db, nil
}
