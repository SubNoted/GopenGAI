package db

import (
	"context"
	"testing"
)

func TestOpen(t *testing.T) {
	t.Run("in-memory database opens", func(t *testing.T) {
		db, err := Open(":memory:")
		if err != nil {
			t.Fatalf("Open(:memory:) error = %v", err)
		}
		defer db.Close()

		// In-memory databases use journal_mode=memory, not wal.
		var journalMode string
		err = db.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
		if err != nil {
			t.Fatalf("query journal_mode: %v", err)
		}
		if journalMode != "memory" {
			t.Logf("journal_mode = %s (expected memory for in-memory DB)", journalMode)
		}
	})

	t.Run("connection pool settings", func(t *testing.T) {
		db, err := Open(":memory:")
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		defer db.Close()

		stats := db.Stats()
		if stats.MaxOpenConnections != 1 {
			t.Errorf("MaxOpenConnections = %d, want 1", stats.MaxOpenConnections)
		}
	})
}

func TestMigrate(t *testing.T) {
	t.Run("migrations apply without error", func(t *testing.T) {
		db, err := Open(":memory:")
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		defer db.Close()

		if err := Migrate(db); err != nil {
			t.Fatalf("Migrate() error = %v", err)
		}

		// Verify all tables exist.
		tables := []string{"sessions", "messages", "agents", "memory", "delegation_logs"}
		for _, table := range tables {
			var count int
			err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count)
			if err != nil {
				t.Errorf("check table %s: %v", table, err)
			}
			if count != 1 {
				t.Errorf("table %s not found in schema", table)
			}
		}
	})

	t.Run("migrations are idempotent", func(t *testing.T) {
		db, err := Open(":memory:")
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		defer db.Close()

		// First migration.
		if err := Migrate(db); err != nil {
			t.Fatalf("first Migrate() error = %v", err)
		}
		// Second migration should be a no-op.
		if err := Migrate(db); err != nil {
			t.Fatalf("second Migrate() error = %v", err)
		}
	})

	t.Run("migrated DB can be queried", func(t *testing.T) {
		db, err := Open(":memory:")
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		defer db.Close()

		if err := Migrate(db); err != nil {
			t.Fatalf("Migrate() error = %v", err)
		}

		queries := New(db)

		// Create a session.
		session, err := queries.CreateSession(context.Background(), CreateSessionParams{
			ID:        "test-session-1",
			AgentName: "default",
			Title:     "Test",
			Status:    "idle",
			CreatedAt: 1000,
			UpdatedAt: 1000,
		})
		if err != nil {
			t.Fatalf("CreateSession() error = %v", err)
		}
		if session.ID != "test-session-1" {
			t.Errorf("session.ID = %q, want %q", session.ID, "test-session-1")
		}
		if session.Title != "Test" {
			t.Errorf("session.Title = %q, want %q", session.Title, "Test")
		}

		// Create a message.
		msg, err := queries.CreateMessage(context.Background(), CreateMessageParams{
			ID:        "msg-1",
			SessionID: "test-session-1",
			Role:      "user",
			Parts:     "[]",
			CreatedAt: 1001,
			UpdatedAt: 1001,
		})
		if err != nil {
			t.Fatalf("CreateMessage() error = %v", err)
		}
		if msg.ID != "msg-1" {
			t.Errorf("msg.ID = %q", msg.ID)
		}

		// List sessions.
		sessions, err := queries.ListSessions(context.Background())
		if err != nil {
			t.Fatalf("ListSessions() error = %v", err)
		}
		if len(sessions) != 1 {
			t.Errorf("len(sessions) = %d, want 1", len(sessions))
		}

		// List messages.
		msgs, err := queries.ListMessagesBySession(context.Background(), "test-session-1")
		if err != nil {
			t.Fatalf("ListMessagesBySession() error = %v", err)
		}
		if len(msgs) != 1 {
			t.Errorf("len(msgs) = %d, want 1", len(msgs))
		}
	})
}
