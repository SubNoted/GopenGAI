package history

import (
	"context"
	"database/sql"
	"fmt"

	"gopengai/internal/db"
)

// Repository wraps db.Querier with history-tree-specific operations.
// It provides methods for loading active branches, listing leaves,
// inserting messages, and managing the session's active leaf pointer.
type Repository struct {
	q     db.Querier
	sqldb *sql.DB
}

// NewRepository creates a new Repository.
func NewRepository(q db.Querier, sqldb *sql.DB) *Repository {
	return &Repository{q: q, sqldb: sqldb}
}

// Querier exposes the underlying Querier for direct DB access.
// This is intentionally public for callers that need escape-hatch access
// to sqlc-generated query methods not wrapped by Repository.
func (r *Repository) Querier() db.Querier {
	return r.q
}

// GetSession returns the session by ID.
func (r *Repository) GetSession(ctx context.Context, id string) (db.Session, error) {
	return r.q.GetSessionByID(ctx, id)
}

// ---------------------------------------------------------------------------
// Message CRUD wrappers
// ---------------------------------------------------------------------------

// InsertMessage creates a new message in the database.
func (r *Repository) InsertMessage(ctx context.Context, params db.CreateMessageParams) (db.Message, error) {
	return r.q.CreateMessage(ctx, params)
}

// GetMessagesForSession returns all messages for a session (flat list, linear order).
func (r *Repository) GetMessagesForSession(ctx context.Context, sessionID string) ([]db.Message, error) {
	return r.q.ListMessagesBySession(ctx, sessionID)
}

// GetMessageByID returns a single message by ID.
func (r *Repository) GetMessageByID(ctx context.Context, id string) (db.Message, error) {
	return r.q.GetMessage(ctx, id)
}

// ---------------------------------------------------------------------------
// Active branch loading
// ---------------------------------------------------------------------------

// GetActiveBranch returns the root-to-leaf message path for the session's
// active branch. If the session has an active_leaf_id set, that leaf is used.
// Otherwise, all messages are loaded, a tree is built, and the longest leaf
// is selected as the default active branch.
func (r *Repository) GetActiveBranch(ctx context.Context, sessionID string) ([]db.Message, error) {
	session, err := r.q.GetSessionByID(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}

	// If active_leaf_id is explicitly set, use the recursive CTE query.
	if session.ActiveLeafID.Valid {
		return r.GetActiveBranchByLeafID(ctx, session.ActiveLeafID.String)
	}

	// No active leaf — load all messages, build tree, pick longest leaf.
	allMessages, err := r.q.ListMessagesBySession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	if len(allMessages) == 0 {
		return nil, nil
	}

	roots := BuildTree(allMessages)
	leaf := GetLongestLeaf(roots)
	if leaf == nil {
		return nil, nil
	}

	path := GetPathFromRoot(leaf)
	result := make([]db.Message, len(path))
	for i, n := range path {
		result[i] = n.Message
	}
	return result, nil
}

// GetActiveBranchByLeafID returns the root-to-leaf message path starting
// from the given leaf ID using the recursive CTE query.
func (r *Repository) GetActiveBranchByLeafID(ctx context.Context, leafID string) ([]db.Message, error) {
	rows, err := r.q.GetBranchFromRootTo(ctx, leafID)
	if err != nil {
		return nil, fmt.Errorf("get branch: %w", err)
	}
	return branchRowsToMessages(rows), nil
}

// GetAllLeaves returns all leaf messages in a session.
func (r *Repository) GetAllLeaves(ctx context.Context, sessionID string) ([]db.Message, error) {
	return r.q.GetAllLeaves(ctx, db.GetAllLeavesParams{
		SessionID:   sessionID,
		SessionID_2: sessionID,
	})
}

// ---------------------------------------------------------------------------
// Session active leaf management
// ---------------------------------------------------------------------------

// UpdateActiveLeaf updates the session's active_leaf_id using a targeted
// UPDATE (no read-modify-write). It does NOT validate the leaf exists or
// belongs to the session — callers (e.g. SelectLeaf) should do that first.
func (r *Repository) UpdateActiveLeaf(ctx context.Context, sessionID, leafID string) error {
	// Use a targeted UPDATE via UpdateSession with only the active_leaf_id
	// changed. Load the current session state to preserve other fields.
	session, err := r.q.GetSessionByID(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("get session for leaf update: %w", err)
	}

	_, err = r.q.UpdateSession(ctx, db.UpdateSessionParams{
		ID:           sessionID,
		Title:        session.Title,
		AgentName:    session.AgentName,
		ActiveLeafID: sql.NullString{String: leafID, Valid: true},
		Status:       session.Status,
	})
	if err != nil {
		return fmt.Errorf("update active leaf: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Conversion helpers: db.GetBranchFromRootToRow -> db.Message
// ---------------------------------------------------------------------------

func branchRowToMessage(r db.GetBranchFromRootToRow) db.Message {
	return db.Message{
		ID:         r.ID,
		SessionID:  r.SessionID,
		ParentID:   r.ParentID,
		Role:       r.Role,
		Parts:      r.Parts,
		Content:    r.Content,
		AgentName:  r.AgentName,
		ToolName:   r.ToolName,
		ToolCallID: r.ToolCallID,
		ToolArgs:   r.ToolArgs,
		Model:      r.Model,
		TokenCount: r.TokenCount,
		CreatedAt:  r.CreatedAt,
		UpdatedAt:  r.UpdatedAt,
		FinishedAt: r.FinishedAt,
	}
}

func branchRowsToMessages(rows []db.GetBranchFromRootToRow) []db.Message {
	if rows == nil {
		return nil
	}
	msgs := make([]db.Message, len(rows))
	for i, r := range rows {
		msgs[i] = branchRowToMessage(r)
	}
	return msgs
}
