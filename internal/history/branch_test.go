package history

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"gopengai/internal/db"
)

// setupRepoDB creates an in-memory SQLite DB with migrations applied,
// returns *sql.DB, *db.Queries, and a Repository tied to them.
func setupRepoDB(t *testing.T) (*sql.DB, *db.Queries, *Repository) {
	t.Helper()

	sqldb, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open in-memory DB: %v", err)
	}

	if err := db.Migrate(sqldb); err != nil {
		sqldb.Close()
		t.Fatalf("migrate: %v", err)
	}

	q := db.New(sqldb)
	return sqldb, q, NewRepository(q, sqldb)
}

// setupTestSession creates a session and a linear chain of messages
// msg0 -> msg1 -> msg2 (leaf), with active_leaf_id set to msg2.
// Returns session and message IDs.
func setupTestSession(ctx context.Context, t *testing.T, q *db.Queries) (sessionID, rootID, leafID string) {
	t.Helper()

	session, err := q.CreateSession(ctx, db.CreateSessionParams{
		ID:        newID(),
		AgentName: "test-agent",
		Title:     "Test Session",
		Status:    "active",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	root := db.CreateMessageParams{
		ID:        newID(),
		SessionID: session.ID,
		Role:      "user",
		Parts:     "[]",
		Content:   ns("Hello"),
	}
	_, err = q.CreateMessage(ctx, root)
	if err != nil {
		t.Fatalf("create root message: %v", err)
	}

	mid := db.CreateMessageParams{
		ID:        newID(),
		SessionID: session.ID,
		ParentID:  ns(root.ID),
		Role:      "assistant",
		Parts:     "[]",
		Content:   ns("Hi there"),
	}
	_, err = q.CreateMessage(ctx, mid)
	if err != nil {
		t.Fatalf("create mid message: %v", err)
	}

	leaf := db.CreateMessageParams{
		ID:        newID(),
		SessionID: session.ID,
		ParentID:  ns(mid.ID),
		Role:      "user",
		Parts:     "[]",
		Content:   ns("How are you?"),
	}
	_, err = q.CreateMessage(ctx, leaf)
	if err != nil {
		t.Fatalf("create leaf message: %v", err)
	}

	// Set active leaf.
	_, err = q.UpdateSession(ctx, db.UpdateSessionParams{
		ID:           session.ID,
		Title:        session.Title,
		AgentName:    session.AgentName,
		ActiveLeafID: ns(leaf.ID),
		Status:       session.Status,
	})
	if err != nil {
		t.Fatalf("set active leaf: %v", err)
	}

	return session.ID, root.ID, leaf.ID
}

// ns is a shorthand for sql.NullString{String: s, Valid: s != ""}.
func ns(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// ---------------------------------------------------------------------------
// SelectLeaf tests
// ---------------------------------------------------------------------------

func TestSelectLeaf(t *testing.T) {
	sqldb, q, repo := setupRepoDB(t)
	defer sqldb.Close()

	sessionID, _, leafID := setupTestSession(context.Background(), t, q)

	t.Run("valid leaf selection", func(t *testing.T) {
		err := repo.SelectLeaf(context.Background(), sessionID, leafID)
		if err != nil {
			t.Fatalf("SelectLeaf() error = %v", err)
		}
		// Verify active_leaf_id was updated.
		session, err := q.GetSessionByID(context.Background(), sessionID)
		if err != nil {
			t.Fatalf("get session: %v", err)
		}
		if session.ActiveLeafID.String != leafID {
			t.Errorf("active_leaf_id = %q, want %q", session.ActiveLeafID.String, leafID)
		}
	})

	t.Run("non-existent leaf", func(t *testing.T) {
		err := repo.SelectLeaf(context.Background(), sessionID, "nonexistent")
		if err == nil {
			t.Fatal("expected error for non-existent leaf")
		}
	})

	t.Run("wrong session", func(t *testing.T) {
		err := repo.SelectLeaf(context.Background(), "wrong-session-id", leafID)
		if err == nil {
			t.Fatal("expected error for wrong session")
		}
	})

	t.Run("message is not a leaf (has children)", func(t *testing.T) {
		// Create a session with a known non-leaf message.
		session, err := q.CreateSession(context.Background(), db.CreateSessionParams{
			ID:        newID(),
			AgentName: "branch-agent",
			Title:     "Branch Test",
			Status:    "active",
		})
		if err != nil {
			t.Fatalf("create session: %v", err)
		}
		parent := db.CreateMessageParams{
			ID:        newID(),
			SessionID: session.ID,
			Role:      "user",
			Parts:     "[]",
			Content:   ns("parent"),
		}
		q.CreateMessage(context.Background(), parent)
		child := db.CreateMessageParams{
			ID:        newID(),
			SessionID: session.ID,
			ParentID:  ns(parent.ID),
			Role:      "assistant",
			Parts:     "[]",
			Content:   ns("child"),
		}
		q.CreateMessage(context.Background(), child)

		err = repo.SelectLeaf(context.Background(), session.ID, parent.ID)
		if err == nil {
			t.Fatal("expected error for non-leaf message")
		}
	})
}

// ---------------------------------------------------------------------------
// EditMessage tests
// ---------------------------------------------------------------------------

func TestEditMessage_ParamsValidation(t *testing.T) {
	tests := []struct {
		name    string
		params  EditMessageParams
		wantErr string
	}{
		{
			name:    "empty session_id",
			params:  EditMessageParams{TargetID: "t1", Content: "new", Role: "user"},
			wantErr: "session_id is required",
		},
		{
			name:    "empty target_id",
			params:  EditMessageParams{SessionID: "s1", Content: "new", Role: "user"},
			wantErr: "target_id is required",
		},
		{
			name:    "invalid role",
			params:  EditMessageParams{SessionID: "s1", TargetID: "t1", Content: "new", Role: "owner"},
			wantErr: "invalid role",
		},
		{
			name: "content too long",
			params: EditMessageParams{
				SessionID: "s1",
				TargetID:  "t1",
				Content:   strings.Repeat("x", maxContentLength+1),
				Role:      "user",
			},
			wantErr: "content exceeds maximum length",
		},
	}

	repo := &Repository{} // doesn't need DB for validation
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := repo.EditMessage(context.Background(), tt.params)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestEditMessage_Success(t *testing.T) {
	sqldb, q, repo := setupRepoDB(t)
	defer sqldb.Close()

	sessionID, _, leafID := setupTestSession(context.Background(), t, q)

	// Edit the leaf message to create a sibling (branch).
	originalLeaf, err := q.GetMessage(context.Background(), leafID)
	if err != nil {
		t.Fatalf("get original leaf: %v", err)
	}

	newMsgID, err := repo.EditMessage(context.Background(), EditMessageParams{
		SessionID: sessionID,
		TargetID:  leafID,
		Content:   "Edited content",
		Role:      "user",
	})
	if err != nil {
		t.Fatalf("EditMessage() error = %v", err)
	}

	// Verify the new message exists.
	newMsg, err := q.GetMessage(context.Background(), newMsgID)
	if err != nil {
		t.Fatalf("get new message: %v", err)
	}
	if newMsg.Content.String != "Edited content" {
		t.Errorf("content = %q, want %q", newMsg.Content.String, "Edited content")
	}
	if newMsg.Role != "user" {
		t.Errorf("role = %q, want %q", newMsg.Role, "user")
	}
	// New message should have same parent as original leaf.
	if newMsg.ParentID.String != originalLeaf.ParentID.String {
		t.Errorf("parent_id = %q, want %q", newMsg.ParentID.String, originalLeaf.ParentID.String)
	}

	// Verify session's active leaf was updated to new message.
	session, _ := q.GetSessionByID(context.Background(), sessionID)
	if session.ActiveLeafID.String != newMsgID {
		t.Errorf("active_leaf_id = %q, want %q", session.ActiveLeafID.String, newMsgID)
	}
}

func TestEditMessage_TargetNotFound(t *testing.T) {
	sqldb, q, repo := setupRepoDB(t)
	defer sqldb.Close()

	sessionID, _, _ := setupTestSession(context.Background(), t, q)

	_, err := repo.EditMessage(context.Background(), EditMessageParams{
		SessionID: sessionID,
		TargetID:  "nonexistent",
		Content:   "new",
		Role:      "user",
	})
	if err == nil {
		t.Fatal("expected error for non-existent target")
	}
}

func TestEditMessage_WrongSession(t *testing.T) {
	sqldb, q, repo := setupRepoDB(t)
	defer sqldb.Close()

	_, _, leafID := setupTestSession(context.Background(), t, q)

	_, err := repo.EditMessage(context.Background(), EditMessageParams{
		SessionID: "wrong-session",
		TargetID:  leafID,
		Content:   "new",
		Role:      "user",
	})
	if err == nil {
		t.Fatal("expected error for wrong session")
	}
}

func TestEditMessage_AllRoles(t *testing.T) {
	sqldb, q, repo := setupRepoDB(t)
	defer sqldb.Close()

	sessionID, _, leafID := setupTestSession(context.Background(), t, q)

	roles := []string{"user", "assistant", "tool", "system"}
	for _, role := range roles {
		t.Run("role="+role, func(t *testing.T) {
			_, err := repo.EditMessage(context.Background(), EditMessageParams{
				SessionID: sessionID,
				TargetID:  leafID,
				Content:   "content for " + role,
				Role:      role,
			})
			if err != nil {
				t.Fatalf("EditMessage() with role %q error = %v", role, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ForkSession tests
// ---------------------------------------------------------------------------

func TestForkSession(t *testing.T) {
	sqldb, q, repo := setupRepoDB(t)
	defer sqldb.Close()

	sessionID, _, leafID := setupTestSession(context.Background(), t, q)

	t.Run("successful fork", func(t *testing.T) {
		newSessionID, err := repo.ForkSession(context.Background(), ForkSessionParams{
			OriginalSessionID: sessionID,
			FromMessageID:     leafID,
			NewContent:        "Let's start over",
			AgentName:         "forked-agent",
			Title:             "Forked Session",
		})
		if err != nil {
			t.Fatalf("ForkSession() error = %v", err)
		}

		// Verify new session exists and has correct fields.
		newSession, err := q.GetSessionByID(context.Background(), newSessionID)
		if err != nil {
			t.Fatalf("get forked session: %v", err)
		}
		if newSession.Title != "Forked Session" {
			t.Errorf("title = %q", newSession.Title)
		}
		if newSession.AgentName != "forked-agent" {
			t.Errorf("agent_name = %q", newSession.AgentName)
		}
		if newSession.ParentSessionID.String != sessionID {
			t.Errorf("parent_session_id = %q, want %q",
				newSession.ParentSessionID.String, sessionID)
		}

		// Verify messages were copied to new session.
		forkedMsgs, err := q.ListMessagesBySession(context.Background(), newSessionID)
		if err != nil {
			t.Fatalf("list forked messages: %v", err)
		}
		// Should have: root + mid + leaf + new user message = 4 messages.
		if len(forkedMsgs) != 4 {
			t.Errorf("forked session has %d messages, want 4", len(forkedMsgs))
		}
		// New session should have an active leaf.
		if !newSession.ActiveLeafID.Valid {
			t.Error("active_leaf_id should be set")
		}
	})

	t.Run("inherit agent name and auto-title", func(t *testing.T) {
		newSessionID, err := repo.ForkSession(context.Background(), ForkSessionParams{
			OriginalSessionID: sessionID,
			FromMessageID:     leafID,
			NewContent:        "Hello again",
		})
		if err != nil {
			t.Fatalf("ForkSession() error = %v", err)
		}
		newSession, _ := q.GetSessionByID(context.Background(), newSessionID)
		if newSession.AgentName != "test-agent" {
			t.Errorf("agent_name = %q, want inherited 'test-agent'", newSession.AgentName)
		}
		if !strings.HasPrefix(newSession.Title, "Fork of ") {
			t.Errorf("title = %q, want prefix 'Fork of '", newSession.Title)
		}
	})

	t.Run("empty original_session_id", func(t *testing.T) {
		_, err := repo.ForkSession(context.Background(), ForkSessionParams{
			FromMessageID: leafID,
			NewContent:    "x",
		})
		if err == nil {
			t.Fatal("expected error for empty original_session_id")
		}
	})

	t.Run("empty from_message_id", func(t *testing.T) {
		_, err := repo.ForkSession(context.Background(), ForkSessionParams{
			OriginalSessionID: sessionID,
			NewContent:        "x",
		})
		if err == nil {
			t.Fatal("expected error for empty from_message_id")
		}
	})

	t.Run("content exceeds max length", func(t *testing.T) {
		_, err := repo.ForkSession(context.Background(), ForkSessionParams{
			OriginalSessionID: sessionID,
			FromMessageID:     leafID,
			NewContent:        strings.Repeat("x", maxContentLength+1),
		})
		if err == nil {
			t.Fatal("expected error for oversized content")
		}
	})

	t.Run("original session not found", func(t *testing.T) {
		_, err := repo.ForkSession(context.Background(), ForkSessionParams{
			OriginalSessionID: "nonexistent",
			FromMessageID:     leafID,
			NewContent:        "test",
		})
		if err == nil {
			t.Fatal("expected error for non-existent original session")
		}
	})

	t.Run("from_message not found", func(t *testing.T) {
		_, err := repo.ForkSession(context.Background(), ForkSessionParams{
			OriginalSessionID: sessionID,
			FromMessageID:     "nonexistent",
			NewContent:        "test",
		})
		if err == nil {
			t.Fatal("expected error for non-existent from_message")
		}
	})

	t.Run("from_message not in original session", func(t *testing.T) {
		// Create another session with its own message.
		otherSess, _ := q.CreateSession(context.Background(), db.CreateSessionParams{
			ID: newID(), AgentName: "other", Title: "Other", Status: "active",
		})
		otherMsg, _ := q.CreateMessage(context.Background(), db.CreateMessageParams{
			ID: newID(), SessionID: otherSess.ID, Role: "user", Parts: "[]",
		})

		_, err := repo.ForkSession(context.Background(), ForkSessionParams{
			OriginalSessionID: sessionID,
			FromMessageID:     otherMsg.ID,
			NewContent:        "test",
		})
		if err == nil {
			t.Fatal("expected error for message from different session")
		}
	})
}

func TestForkSession_CopiesBranchCorrectly(t *testing.T) {
	sqldb, q, repo := setupRepoDB(t)
	defer sqldb.Close()

	// Create session with a simple 2-message chain: root -> leaf.
	session, err := q.CreateSession(context.Background(), db.CreateSessionParams{
		ID: newID(), AgentName: "fork-agent", Title: "Original", Status: "active",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	rootID := newID()
	if _, err := q.CreateMessage(context.Background(), db.CreateMessageParams{
		ID: rootID, SessionID: session.ID, Role: "user", Parts: "[]",
		Content: ns("original question"),
	}); err != nil {
		t.Fatalf("create root msg: %v", err)
	}

	childID := newID()
	if _, err := q.CreateMessage(context.Background(), db.CreateMessageParams{
		ID: childID, SessionID: session.ID, ParentID: ns(rootID),
		Role: "assistant", Parts: "[]", Content: ns("original answer"),
	}); err != nil {
		t.Fatalf("create child msg: %v", err)
	}

	// Fork from childID.
	newSessionID, err := repo.ForkSession(context.Background(), ForkSessionParams{
		OriginalSessionID: session.ID,
		FromMessageID:     childID,
		NewContent:        "Let me rephrase",
	})
	if err != nil {
		t.Fatalf("ForkSession() error = %v", err)
	}

	// Verify 3 messages in fork: root copy, child copy, new user message.
	forkMsgs, _ := q.ListMessagesBySession(context.Background(), newSessionID)
	if len(forkMsgs) != 3 {
		t.Fatalf("expected 3 messages in fork, got %d", len(forkMsgs))
	}

	// Verify the fork contains the expected messages.
	var rootCopy, childCopy, newMsg db.Message
	for _, m := range forkMsgs {
		if !m.ParentID.Valid {
			rootCopy = m
		} else if m.Role == "assistant" {
			childCopy = m
		} else if m.Role == "user" {
			newMsg = m
		}
	}
	if rootCopy.ID == "" {
		t.Fatal("no root copy found (NULL parent)")
	}
	if childCopy.ID == "" {
		t.Fatal("no child copy found (role=assistant)")
	}
	if newMsg.ID == "" {
		t.Fatal("no new message found (role=user, with parent)")
	}
	if rootCopy.Content.String != "original question" {
		t.Errorf("root copy content = %q", rootCopy.Content.String)
	}
	if childCopy.ParentID.String != rootCopy.ID {
		t.Errorf("child copy parent = %q, want root copy %q", childCopy.ParentID.String, rootCopy.ID)
	}
	// New user message should be a child of the root copy (same parent as child copy).
	if newMsg.ParentID.String != rootCopy.ID {
		t.Errorf("new msg parent = %q, want root copy %q", newMsg.ParentID.String, rootCopy.ID)
	}
}
