package history

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"gopengai/internal/db"
)

// maxContentLength is the maximum allowed size for user-supplied content
// in EditMessage and ForkSession operations.
const maxContentLength = 100 * 1024 // 100 KB

// validRoles lists the allowed role values for EditMessage.
var validRoles = map[string]bool{
	"user":      true,
	"assistant": true,
	"tool":      true,
	"system":    true,
}

// ---------------------------------------------------------------------------
// Branch selection
// ---------------------------------------------------------------------------

// SelectLeaf explicitly sets a leaf as the active branch for a session.
// It verifies that the leaf exists, belongs to the session, and is actually
// a leaf (no children). The session is updated with the new active_leaf_id.
func (r *Repository) SelectLeaf(ctx context.Context, sessionID, leafID string) error {
	// Load the leaf message.
	msg, err := r.q.GetMessage(ctx, leafID)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("leaf %s not found", leafID)
		}
		return fmt.Errorf("get leaf message: %w", err)
	}

	// Verify the message belongs to the specified session.
	if msg.SessionID != sessionID {
		return fmt.Errorf("leaf %s does not belong to session %s", leafID, sessionID)
	}

	// Verify it's actually a leaf (no children in this session).
	leaves, err := r.q.GetAllLeaves(ctx, db.GetAllLeavesParams{
		SessionID:   sessionID,
		SessionID_2: sessionID,
	})
	if err != nil {
		return fmt.Errorf("get all leaves: %w", err)
	}

	isLeaf := false
	for _, leaf := range leaves {
		if leaf.ID == leafID {
			isLeaf = true
			break
		}
	}
	if !isLeaf {
		return fmt.Errorf("message %s is not a leaf (has children)", leafID)
	}

	// Update the session's active leaf.
	return r.UpdateActiveLeaf(ctx, sessionID, leafID)
}

// ---------------------------------------------------------------------------
// Message editing (branch creation)
// ---------------------------------------------------------------------------

// EditMessageParams defines the parameters for editing (branching from) a
// previous message. A new message is created as a sibling of the target
// message (same parent), effectively creating a new branch.
type EditMessageParams struct {
	SessionID string // session to edit in
	TargetID  string // the message being "edited" (new msg becomes its sibling)
	Content   string // new content for the edited message
	Role      string // usually "user"; validated against allowlist
}

// validate checks EditMessageParams for invalid or dangerous values.
func (p EditMessageParams) validate() error {
	if p.SessionID == "" {
		return fmt.Errorf("session_id is required")
	}
	if p.TargetID == "" {
		return fmt.Errorf("target_id is required")
	}
	if !validRoles[p.Role] {
		return fmt.Errorf("invalid role %q: must be one of user, assistant, tool, system", p.Role)
	}
	if len(p.Content) > maxContentLength {
		return fmt.Errorf("content exceeds maximum length of %d bytes", maxContentLength)
	}
	return nil
}

// EditMessage creates a new message that branches from the parent of the
// target message. The target message itself is not modified (messages are
// immutable). The new message has the same parent_id as the target, creating
// a fork in the conversation tree. The session's active leaf is updated to
// point to the new message.
//
// All write operations are wrapped in a SQL transaction for atomicity.
// Returns the ID of the newly created message.
func (r *Repository) EditMessage(ctx context.Context, params EditMessageParams) (string, error) {
	if err := params.validate(); err != nil {
		return "", fmt.Errorf("invalid edit params: %w", err)
	}

	// Begin transaction.
	tx, err := r.sqldb.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() // no-op if already committed

	txq := r.q.(*db.Queries).WithTx(tx)

	// Load the target message to determine its parent.
	target, err := txq.GetMessage(ctx, params.TargetID)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("target message %s not found", params.TargetID)
		}
		return "", fmt.Errorf("get target message: %w", err)
	}

	// Verify the target belongs to the given session.
	if target.SessionID != params.SessionID {
		return "", fmt.Errorf("target message %s does not belong to session %s",
			params.TargetID, params.SessionID)
	}

	// Determine parent_id: the new message becomes a sibling of the target,
	// so it gets the same parent_id as the target.
	var parentID sql.NullString
	if target.ParentID.Valid {
		parentID = target.ParentID
	}
	// If target has no parent (it's a root), the new message also has no parent
	// (becomes another root — edge case, but handled gracefully).

	// Create the new message.
	now := time.Now().UnixMilli()
	newMsgID := newID()
	_, err = txq.CreateMessage(ctx, db.CreateMessageParams{
		ID:        newMsgID,
		SessionID: params.SessionID,
		ParentID:  parentID,
		Role:      params.Role,
		Parts:     "[]",
		Content:   sql.NullString{String: params.Content, Valid: params.Content != ""},
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		return "", fmt.Errorf("create edited message: %w", err)
	}

	// Update the session's active leaf to point to the new message.
	session, err := txq.GetSessionByID(ctx, params.SessionID)
	if err != nil {
		return "", fmt.Errorf("get session for leaf update: %w", err)
	}
	_, err = txq.UpdateSession(ctx, db.UpdateSessionParams{
		ID:           params.SessionID,
		Title:        session.Title,
		AgentName:    session.AgentName,
		ActiveLeafID: sql.NullString{String: newMsgID, Valid: true},
		Status:       session.Status,
	})
	if err != nil {
		return "", fmt.Errorf("update active leaf after edit: %w", err)
	}

	// Commit transaction.
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit edit tx: %w", err)
	}

	return newMsgID, nil
}

// ---------------------------------------------------------------------------
// Session forking
// ---------------------------------------------------------------------------

// ForkSessionParams defines the parameters for forking a session.
type ForkSessionParams struct {
	OriginalSessionID string // the session to fork from
	AgentName         string // agent for the new session (empty = inherit from original)
	Title             string // title for the new session (empty = "Fork of ...")
	FromMessageID     string // the last message to include from the original branch
	NewContent        string // the first user message in the forked session
}

// ForkSession creates a new session that is a fork of an existing session.
// It copies messages from the original session's active branch up to (and
// including) FromMessageID, then appends a new user message (NewContent) as
// a child of the last copied message. The new session has parent_session_id
// pointing to the original session.
//
// All write operations are wrapped in a SQL transaction for atomicity.
// Returns the ID of the new session.
func (r *Repository) ForkSession(ctx context.Context, params ForkSessionParams) (string, error) {
	if params.OriginalSessionID == "" {
		return "", fmt.Errorf("original_session_id is required")
	}
	if params.FromMessageID == "" {
		return "", fmt.Errorf("from_message_id is required")
	}
	if len(params.NewContent) > maxContentLength {
		return "", fmt.Errorf("new content exceeds maximum length of %d bytes", maxContentLength)
	}

	// Begin transaction.
	tx, err := r.sqldb.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() // no-op if already committed

	txq := r.q.(*db.Queries).WithTx(tx)

	// Load the original session.
	originalSession, err := txq.GetSessionByID(ctx, params.OriginalSessionID)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("original session %s not found", params.OriginalSessionID)
		}
		return "", fmt.Errorf("get original session: %w", err)
	}

	// Verify the FromMessageID belongs to the original session.
	fromMsg, err := txq.GetMessage(ctx, params.FromMessageID)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("from_message %s not found", params.FromMessageID)
		}
		return "", fmt.Errorf("get from_message: %w", err)
	}
	if fromMsg.SessionID != params.OriginalSessionID {
		return "", fmt.Errorf("from_message %s does not belong to session %s",
			params.FromMessageID, params.OriginalSessionID)
	}

	// Resolve agent name.
	agentName := params.AgentName
	if agentName == "" {
		agentName = originalSession.AgentName
	}

	// Resolve title.
	title := params.Title
	if title == "" {
		title = fmt.Sprintf("Fork of %s", originalSession.Title)
	}

	// Load the branch from root to the specified from_message.
	branchMessages, err := txq.GetBranchFromRootTo(ctx, params.FromMessageID)
	if err != nil {
		return "", fmt.Errorf("get branch to fork point: %w", err)
	}

	now := time.Now().UnixMilli()

	// Create the new session.
	newSession, err := txq.CreateSession(ctx, db.CreateSessionParams{
		ID:              newID(),
		ParentSessionID: sql.NullString{String: params.OriginalSessionID, Valid: true},
		AgentName:       agentName,
		Title:           title,
		Status:          "idle",
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	if err != nil {
		return "", fmt.Errorf("create forked session: %w", err)
	}

	// Copy messages from the original branch into the new session.
	idMap := make(map[string]string) // old ID -> new ID
	var lastNewID string

	for _, row := range branchMessages {
		newMsgID := newID()
		idMap[row.ID] = newMsgID

		var newParentID sql.NullString
		if row.ParentID.Valid {
			if mappedID, ok := idMap[row.ParentID.String]; ok {
				newParentID = sql.NullString{String: mappedID, Valid: true}
			}
		}

		_, err := txq.CreateMessage(ctx, db.CreateMessageParams{
			ID:         newMsgID,
			SessionID:  newSession.ID,
			ParentID:   newParentID,
			Role:       row.Role,
			Parts:      row.Parts,
			Content:    row.Content,
			AgentName:  row.AgentName,
			ToolName:   row.ToolName,
			ToolCallID: row.ToolCallID,
			ToolArgs:   row.ToolArgs,
			Model:      row.Model,
			TokenCount: row.TokenCount,
			CreatedAt:  now,
			UpdatedAt:  now,
		})
		if err != nil {
			return "", fmt.Errorf("copy message %s: %w", row.ID, err)
		}
		lastNewID = newMsgID
	}

	// Add the new user message as a child of the last copied message.
	firstMsgID := newID()
	_, err = txq.CreateMessage(ctx, db.CreateMessageParams{
		ID:        firstMsgID,
		SessionID: newSession.ID,
		ParentID:  sql.NullString{String: lastNewID, Valid: lastNewID != ""},
		Role:      "user",
		Parts:     "[]",
		Content:   sql.NullString{String: params.NewContent, Valid: params.NewContent != ""},
		CreatedAt: now + 1,
		UpdatedAt: now + 1,
	})
	if err != nil {
		return "", fmt.Errorf("create first fork message: %w", err)
	}

	// Set the session's active leaf to the first new message.
	_, err = txq.UpdateSession(ctx, db.UpdateSessionParams{
		ID:           newSession.ID,
		Title:        title,
		AgentName:    agentName,
		ActiveLeafID: sql.NullString{String: firstMsgID, Valid: true},
		Status:       "idle",
	})
	if err != nil {
		return "", fmt.Errorf("set active leaf on forked session: %w", err)
	}

	// Commit transaction.
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit fork tx: %w", err)
	}

	return newSession.ID, nil
}
