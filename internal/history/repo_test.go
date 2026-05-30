package history

import (
	"context"
	"database/sql"
	"testing"

	"gopengai/internal/db"
)

func TestRepository_GetSession(t *testing.T) {
	sqldb, q, repo := setupRepoDB(t)
	defer sqldb.Close()

	t.Run("existing session", func(t *testing.T) {
		session, err := q.CreateSession(context.Background(), db.CreateSessionParams{
			ID: newID(), AgentName: "agent1", Title: "Test", Status: "active",
		})
		if err != nil {
			t.Fatalf("create session: %v", err)
		}
		got, err := repo.GetSession(context.Background(), session.ID)
		if err != nil {
			t.Fatalf("GetSession() error = %v", err)
		}
		if got.ID != session.ID {
			t.Errorf("id = %q, want %q", got.ID, session.ID)
		}
	})

	t.Run("non-existent session", func(t *testing.T) {
		_, err := repo.GetSession(context.Background(), "nonexistent")
		if err == nil {
			t.Fatal("expected error for non-existent session")
		}
	})
}

func TestRepository_InsertMessage(t *testing.T) {
	sqldb, q, repo := setupRepoDB(t)
	defer sqldb.Close()

	session, _ := q.CreateSession(context.Background(), db.CreateSessionParams{
		ID: newID(), AgentName: "agent1", Title: "Test", Status: "active",
	})

	msg, err := repo.InsertMessage(context.Background(), db.CreateMessageParams{
		ID:        newID(),
		SessionID: session.ID,
		Role:      "user",
		Parts:     "[]",
		Content:   ns("Hello"),
	})
	if err != nil {
		t.Fatalf("InsertMessage() error = %v", err)
	}
	if msg.Role != "user" {
		t.Errorf("role = %q", msg.Role)
	}
	if msg.Content.String != "Hello" {
		t.Errorf("content = %q", msg.Content.String)
	}
}

func TestRepository_GetMessagesForSession(t *testing.T) {
	sqldb, q, repo := setupRepoDB(t)
	defer sqldb.Close()

	session, _ := q.CreateSession(context.Background(), db.CreateSessionParams{
		ID: newID(), AgentName: "agent1", Title: "Test", Status: "active",
	})

	t.Run("empty session", func(t *testing.T) {
		msgs, err := repo.GetMessagesForSession(context.Background(), session.ID)
		if err != nil {
			t.Fatalf("GetMessagesForSession() error = %v", err)
		}
		if len(msgs) != 0 {
			t.Errorf("expected 0 messages, got %d", len(msgs))
		}
	})

	t.Run("session with messages", func(t *testing.T) {
		q.CreateMessage(context.Background(), db.CreateMessageParams{
			ID: newID(), SessionID: session.ID, Role: "user", Parts: "[]", Content: ns("msg1"),
		})
		q.CreateMessage(context.Background(), db.CreateMessageParams{
			ID: newID(), SessionID: session.ID, Role: "assistant", Parts: "[]", Content: ns("msg2"),
		})
		msgs, err := repo.GetMessagesForSession(context.Background(), session.ID)
		if err != nil {
			t.Fatalf("GetMessagesForSession() error = %v", err)
		}
		if len(msgs) != 2 {
			t.Errorf("expected 2 messages, got %d", len(msgs))
		}
	})
}

func TestRepository_GetMessageByID(t *testing.T) {
	sqldb, q, repo := setupRepoDB(t)
	defer sqldb.Close()

	session, _ := q.CreateSession(context.Background(), db.CreateSessionParams{
		ID: newID(), AgentName: "agent1", Title: "Test", Status: "active",
	})

	inserted, err := q.CreateMessage(context.Background(), db.CreateMessageParams{
		ID: newID(), SessionID: session.ID, Role: "user", Parts: "[]", Content: ns("test"),
	})
	if err != nil {
		t.Fatalf("create message: %v", err)
	}

	got, err := repo.GetMessageByID(context.Background(), inserted.ID)
	if err != nil {
		t.Fatalf("GetMessageByID() error = %v", err)
	}
	if got.ID != inserted.ID {
		t.Errorf("id = %q, want %q", got.ID, inserted.ID)
	}

	// Non-existent.
	_, err = repo.GetMessageByID(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent message")
	}
}

func TestRepository_UpdateActiveLeaf(t *testing.T) {
	sqldb, q, repo := setupRepoDB(t)
	defer sqldb.Close()

	session, _ := q.CreateSession(context.Background(), db.CreateSessionParams{
		ID: newID(), AgentName: "agent1", Title: "Test", Status: "active",
	})

	leafID := newID()
	err := repo.UpdateActiveLeaf(context.Background(), session.ID, leafID)
	if err != nil {
		t.Fatalf("UpdateActiveLeaf() error = %v", err)
	}

	// Verify update.
	updated, _ := q.GetSessionByID(context.Background(), session.ID)
	if updated.ActiveLeafID.String != leafID {
		t.Errorf("active_leaf_id = %q, want %q", updated.ActiveLeafID.String, leafID)
	}

	// Non-existent session.
	err = repo.UpdateActiveLeaf(context.Background(), "nonexistent", leafID)
	if err == nil {
		t.Fatal("expected error for non-existent session")
	}
}

func TestRepository_GetAllLeaves(t *testing.T) {
	sqldb, q, repo := setupRepoDB(t)
	defer sqldb.Close()

	session, _ := q.CreateSession(context.Background(), db.CreateSessionParams{
		ID: newID(), AgentName: "agent1", Title: "Test", Status: "active",
	})

	// No messages -> no leaves.
	leaves, err := repo.GetAllLeaves(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("GetAllLeaves() error = %v", err)
	}
	if len(leaves) != 0 {
		t.Errorf("expected 0 leaves, got %d", len(leaves))
	}

	// Single message -> it's a leaf.
	rootID := newID()
	q.CreateMessage(context.Background(), db.CreateMessageParams{
		ID: rootID, SessionID: session.ID, Role: "user", Parts: "[]", Content: ns("root"),
	})
	leaves, err = repo.GetAllLeaves(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("GetAllLeaves() error = %v", err)
	}
	if len(leaves) != 1 {
		t.Errorf("expected 1 leaf, got %d", len(leaves))
	}
	if leaves[0].ID != rootID {
		t.Errorf("leaf id = %q, want %q", leaves[0].ID, rootID)
	}

	// Add a child -> root is no longer a leaf.
	childID := newID()
	q.CreateMessage(context.Background(), db.CreateMessageParams{
		ID: childID, SessionID: session.ID, ParentID: ns(rootID),
		Role: "assistant", Parts: "[]", Content: ns("child"),
	})
	leaves, err = repo.GetAllLeaves(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("GetAllLeaves() error = %v", err)
	}
	if len(leaves) != 1 {
		t.Errorf("expected 1 leaf, got %d", len(leaves))
	}
	if leaves[0].ID != childID {
		t.Errorf("leaf id = %q, want %q", leaves[0].ID, childID)
	}
}

func TestRepository_GetActiveBranch(t *testing.T) {
	sqldb, q, repo := setupRepoDB(t)
	defer sqldb.Close()

	t.Run("session with active_leaf_id", func(t *testing.T) {
		session, _ := q.CreateSession(context.Background(), db.CreateSessionParams{
			ID: newID(), AgentName: "agent1", Title: "Branch Test", Status: "active",
		})

		// Create message chain: root -> mid -> leaf.
		rootID := newID()
		q.CreateMessage(context.Background(), db.CreateMessageParams{
			ID: rootID, SessionID: session.ID, Role: "user", Parts: "[]", Content: ns("root"),
		})
		midID := newID()
		q.CreateMessage(context.Background(), db.CreateMessageParams{
			ID: midID, SessionID: session.ID, ParentID: ns(rootID),
			Role: "assistant", Parts: "[]", Content: ns("mid"),
		})
		leafID := newID()
		q.CreateMessage(context.Background(), db.CreateMessageParams{
			ID: leafID, SessionID: session.ID, ParentID: ns(midID),
			Role: "assistant", Parts: "[]", Content: ns("leaf"),
		})

		// Set active_leaf_id to mid (not leaf).
		q.UpdateSession(context.Background(), db.UpdateSessionParams{
			ID: session.ID, Title: session.Title, AgentName: session.AgentName,
			ActiveLeafID: ns(midID), Status: session.Status,
		})

		branch, err := repo.GetActiveBranch(context.Background(), session.ID)
		if err != nil {
			t.Fatalf("GetActiveBranch() error = %v", err)
		}
		if len(branch) != 2 {
			t.Fatalf("expected 2 messages in branch, got %d", len(branch))
		}
		if branch[0].ID != rootID {
			t.Errorf("first msg = %q, want root %q", branch[0].ID, rootID)
		}
		if branch[1].ID != midID {
			t.Errorf("last msg = %q, want mid %q", branch[1].ID, midID)
		}
	})

	t.Run("session without active_leaf_id picks longest", func(t *testing.T) {
		session, _ := q.CreateSession(context.Background(), db.CreateSessionParams{
			ID: newID(), AgentName: "agent1", Title: "No Leaf", Status: "active",
		})

		// Create a branching tree:
		//         root
		//        /    \
		//    short    long
		rootID := newID()
		q.CreateMessage(context.Background(), db.CreateMessageParams{
			ID: rootID, SessionID: session.ID, Role: "user", Parts: "[]", Content: ns("root"),
		})
		shortID := newID()
		q.CreateMessage(context.Background(), db.CreateMessageParams{
			ID: shortID, SessionID: session.ID, ParentID: ns(rootID),
			Role: "assistant", Parts: "[]", Content: ns("short"),
		})
		longID := newID()
		q.CreateMessage(context.Background(), db.CreateMessageParams{
			ID: longID, SessionID: session.ID, ParentID: ns(rootID),
			Role: "assistant", Parts: "[]", Content: ns("long"),
		})
		longerID := newID()
		q.CreateMessage(context.Background(), db.CreateMessageParams{
			ID: longerID, SessionID: session.ID, ParentID: ns(longID),
			Role: "assistant", Parts: "[]", Content: ns("longer"),
		})

		// No active_leaf_id set — should pick the longest chain (root->long->longer).
		branch, err := repo.GetActiveBranch(context.Background(), session.ID)
		if err != nil {
			t.Fatalf("GetActiveBranch() error = %v", err)
		}
		if len(branch) != 3 {
			t.Fatalf("expected 3 messages in branch, got %d", len(branch))
		}
		if branch[2].ID != longerID {
			t.Errorf("last msg = %q, want %q (deepest leaf)", branch[2].ID, longerID)
		}
	})

	t.Run("empty session", func(t *testing.T) {
		session, _ := q.CreateSession(context.Background(), db.CreateSessionParams{
			ID: newID(), AgentName: "agent1", Title: "Empty", Status: "active",
		})
		branch, err := repo.GetActiveBranch(context.Background(), session.ID)
		if err != nil {
			t.Fatalf("GetActiveBranch() error = %v", err)
		}
		if branch != nil {
			t.Errorf("expected nil branch for empty session, got %d messages", len(branch))
		}
	})

	t.Run("non-existent session", func(t *testing.T) {
		_, err := repo.GetActiveBranch(context.Background(), "nonexistent")
		if err == nil {
			t.Fatal("expected error for non-existent session")
		}
	})
}

func TestRepository_Querier(t *testing.T) {
	sqldb, _, repo := setupRepoDB(t)
	defer sqldb.Close()

	got := repo.Querier()
	if got == nil {
		t.Fatal("Querier() returned nil")
	}

	// Verify it works by calling a method.
	_, err := got.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("Querier().ListSessions() error = %v", err)
	}
}

func TestBranchRowConversion(t *testing.T) {
	row := db.GetBranchFromRootToRow{
		ID:        "id1",
		SessionID: "s1",
		ParentID:  sql.NullString{String: "parent1", Valid: true},
		Role:      "user",
		Parts:     "[]",
		Content:   sql.NullString{String: "hello", Valid: true},
		AgentName: sql.NullString{String: "agent1", Valid: true},
	}
	msg := branchRowToMessage(row)
	if msg.ID != "id1" {
		t.Errorf("ID = %q", msg.ID)
	}
	if msg.ParentID.String != "parent1" {
		t.Errorf("ParentID = %q", msg.ParentID.String)
	}
	if msg.Content.String != "hello" {
		t.Errorf("Content = %q", msg.Content.String)
	}
	if msg.AgentName.String != "agent1" {
		t.Errorf("AgentName = %q", msg.AgentName.String)
	}
}

func TestBranchRowsConversion(t *testing.T) {
	t.Run("nil input", func(t *testing.T) {
		result := branchRowsToMessages(nil)
		if result != nil {
			t.Error("expected nil for nil input")
		}
	})

	t.Run("single row", func(t *testing.T) {
		rows := []db.GetBranchFromRootToRow{
			{ID: "a", SessionID: "s", Role: "user"},
		}
		result := branchRowsToMessages(rows)
		if len(result) != 1 {
			t.Fatalf("expected 1, got %d", len(result))
		}
		if result[0].ID != "a" {
			t.Errorf("ID = %q", result[0].ID)
		}
	})
}
