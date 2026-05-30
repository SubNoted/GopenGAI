package history

import (
	"database/sql"
	"testing"

	"gopengai/internal/db"
)

// makeMsg is a test helper for creating db.Message structs.
func makeMsg(id, sessionID string, parentID sql.NullString, role, content string) db.Message {
	return db.Message{
		ID:        id,
		SessionID: sessionID,
		ParentID:  parentID,
		Role:      role,
		Parts:     "[]",
		Content:   sql.NullString{String: content, Valid: content != ""},
	}
}

// msgID creates a non-null parent_id reference.
func msgID(id string) sql.NullString {
	return sql.NullString{String: id, Valid: true}
}

func TestBuildTree(t *testing.T) {
	t.Run("empty messages returns nil", func(t *testing.T) {
		roots := BuildTree(nil)
		if roots != nil {
			t.Errorf("BuildTree(nil) = %v, want nil", roots)
		}
		roots = BuildTree([]db.Message{})
		if roots != nil {
			t.Errorf("BuildTree([]) = %v, want nil", roots)
		}
	})

	t.Run("single root message", func(t *testing.T) {
		msgs := []db.Message{
			makeMsg("1", "s1", sql.NullString{}, "user", "hello"),
		}
		roots := BuildTree(msgs)
		if len(roots) != 1 {
			t.Fatalf("len(roots) = %d, want 1", len(roots))
		}
		if roots[0].Message.ID != "1" {
			t.Errorf("root ID = %q, want %q", roots[0].Message.ID, "1")
		}
		if roots[0].Depth != 0 {
			t.Errorf("root Depth = %d, want 0", roots[0].Depth)
		}
		if roots[0].Parent != nil {
			t.Error("root Parent should be nil")
		}
		if len(roots[0].Children) != 0 {
			t.Errorf("root has %d children, want 0", len(roots[0].Children))
		}
	})

	t.Run("linear chain of 3 messages", func(t *testing.T) {
		msgs := []db.Message{
			makeMsg("1", "s1", sql.NullString{}, "user", "q1"),
			makeMsg("2", "s1", msgID("1"), "assistant", "a1"),
			makeMsg("3", "s1", msgID("2"), "user", "q2"),
		}
		roots := BuildTree(msgs)
		if len(roots) != 1 {
			t.Fatalf("len(roots) = %d, want 1", len(roots))
		}

		root := roots[0]
		if root.Message.ID != "1" || root.Depth != 0 {
			t.Errorf("root: ID=%s Depth=%d", root.Message.ID, root.Depth)
		}

		child1 := root.Children[0]
		if child1.Message.ID != "2" || child1.Depth != 1 {
			t.Errorf("child1: ID=%s Depth=%d", child1.Message.ID, child1.Depth)
		}
		if child1.Parent != root {
			t.Error("child1.Parent != root")
		}

		child2 := child1.Children[0]
		if child2.Message.ID != "3" || child2.Depth != 2 {
			t.Errorf("child2: ID=%s Depth=%d", child2.Message.ID, child2.Depth)
		}
		if child2.Parent != child1 {
			t.Error("child2.Parent != child1")
		}
	})

	t.Run("branching tree", func(t *testing.T) {
		//    1
		//   / \
		//  2   3
		//  |
		//  4
		msgs := []db.Message{
			makeMsg("1", "s1", sql.NullString{}, "user", "root"),
			makeMsg("2", "s1", msgID("1"), "assistant", "left"),
			makeMsg("3", "s1", msgID("1"), "assistant", "right"),
			makeMsg("4", "s1", msgID("2"), "user", "leaf"),
		}
		roots := BuildTree(msgs)
		if len(roots) != 1 {
			t.Fatalf("len(roots) = %d, want 1", len(roots))
		}

		root := roots[0]
		if len(root.Children) != 2 {
			t.Errorf("root has %d children, want 2", len(root.Children))
		}

		// Child "2" should have child "4".
		for _, c := range root.Children {
			switch c.Message.ID {
			case "2":
				if len(c.Children) != 1 || c.Children[0].Message.ID != "4" {
					t.Error("child 2 structure incorrect")
				}
			case "3":
				if len(c.Children) != 0 {
					t.Error("child 3 should have no children")
				}
			}
		}
	})

	t.Run("orphan message treated as root", func(t *testing.T) {
		msgs := []db.Message{
			makeMsg("1", "s1", sql.NullString{}, "user", "root"),
			makeMsg("2", "s1", msgID("orphan"), "assistant", "orphan"),
		}
		roots := BuildTree(msgs)
		if len(roots) != 2 {
			t.Fatalf("len(roots) = %d, want 2", len(roots))
		}
		// Both are roots.
		for _, r := range roots {
			if r.Depth != 0 {
				t.Errorf("root %s has Depth %d, want 0", r.Message.ID, r.Depth)
			}
		}
	})

	t.Run("all children reference each other (no root)", func(t *testing.T) {
		msgs := []db.Message{
			makeMsg("1", "s1", msgID("2"), "assistant", "child of 2"),
			makeMsg("2", "s1", msgID("1"), "user", "child of 1"),
		}
		roots := BuildTree(msgs)
		if len(roots) != 2 {
			t.Fatalf("len(roots) = %d, want 2 (both treated as roots)", len(roots))
		}
	})
}

func TestFindNode(t *testing.T) {
	msgs := []db.Message{
		makeMsg("1", "s1", sql.NullString{}, "user", "root"),
		makeMsg("2", "s1", msgID("1"), "assistant", "child"),
		makeMsg("3", "s1", msgID("1"), "assistant", "sibling"),
	}
	roots := BuildTree(msgs)

	t.Run("find existing node", func(t *testing.T) {
		node := FindNode(roots, "2")
		if node == nil {
			t.Fatal("FindNode returned nil for existing node")
		}
		if node.Message.ID != "2" {
			t.Errorf("node.ID = %q, want %q", node.Message.ID, "2")
		}
	})

	t.Run("find non-existent node", func(t *testing.T) {
		node := FindNode(roots, "99")
		if node != nil {
			t.Error("FindNode should return nil for non-existent node")
		}
	})

	t.Run("find from nil roots", func(t *testing.T) {
		node := FindNode(nil, "1")
		if node != nil {
			t.Error("FindNode with nil roots should return nil")
		}
	})

	t.Run("find from empty roots", func(t *testing.T) {
		node := FindNode([]*TreeNode{}, "1")
		if node != nil {
			t.Error("FindNode with empty roots should return nil")
		}
	})
}

func TestGetPathFromRoot(t *testing.T) {
	msgs := []db.Message{
		makeMsg("1", "s1", sql.NullString{}, "user", "root"),
		makeMsg("2", "s1", msgID("1"), "assistant", "mid"),
		makeMsg("3", "s1", msgID("2"), "user", "leaf"),
	}
	roots := BuildTree(msgs)
	leaf := FindNode(roots, "3")

	t.Run("correct path order", func(t *testing.T) {
		path := GetPathFromRoot(leaf)
		if len(path) != 3 {
			t.Fatalf("len(path) = %d, want 3", len(path))
		}
		if path[0].Message.ID != "1" {
			t.Errorf("path[0] = %q, want %q", path[0].Message.ID, "1")
		}
		if path[1].Message.ID != "2" {
			t.Errorf("path[1] = %q, want %q", path[1].Message.ID, "2")
		}
		if path[2].Message.ID != "3" {
			t.Errorf("path[2] = %q, want %q", path[2].Message.ID, "3")
		}
	})

	t.Run("nil node returns nil", func(t *testing.T) {
		if GetPathFromRoot(nil) != nil {
			t.Error("GetPathFromRoot(nil) should return nil")
		}
	})

	t.Run("root node path has one element", func(t *testing.T) {
		path := GetPathFromRoot(roots[0])
		if len(path) != 1 {
			t.Fatalf("len(path) = %d, want 1", len(path))
		}
		if path[0].Message.ID != "1" {
			t.Errorf("path[0] = %q, want %q", path[0].Message.ID, "1")
		}
	})
}

func TestIsLeaf(t *testing.T) {
	msgs := []db.Message{
		makeMsg("1", "s1", sql.NullString{}, "user", "root"),
		makeMsg("2", "s1", msgID("1"), "assistant", "leaf1"),
		makeMsg("3", "s1", msgID("1"), "assistant", "leaf2"),
	}
	roots := BuildTree(msgs)

	t.Run("node with children is not a leaf", func(t *testing.T) {
		if IsLeaf(roots[0]) {
			t.Error("root should not be a leaf (has 2 children)")
		}
	})

	t.Run("node without children is a leaf", func(t *testing.T) {
		leaf := FindNode(roots, "2")
		if !IsLeaf(leaf) {
			t.Error("node 2 should be a leaf")
		}
	})
}

func TestGetAllLeavesAsNodes(t *testing.T) {
	msgs := []db.Message{
		makeMsg("1", "s1", sql.NullString{}, "user", "root"),
		makeMsg("2", "s1", msgID("1"), "assistant", "a1"),
		makeMsg("3", "s1", msgID("1"), "assistant", "a2"),
		makeMsg("4", "s1", msgID("2"), "user", "deep"),
	}
	roots := BuildTree(msgs)

	t.Run("correct leaf count", func(t *testing.T) {
		leaves := GetAllLeavesAsNodes(roots)
		if len(leaves) != 2 {
			t.Fatalf("len(leaves) = %d, want 2 (nodes 3 and 4)", len(leaves))
		}
		ids := make(map[string]bool)
		for _, l := range leaves {
			ids[l.Message.ID] = true
		}
		if !ids["3"] {
			t.Error("node 3 not in leaves")
		}
		if !ids["4"] {
			t.Error("node 4 not in leaves")
		}
	})

	t.Run("nil roots returns nil", func(t *testing.T) {
		if GetAllLeavesAsNodes(nil) != nil {
			t.Error("expected nil")
		}
	})
}

func TestGetLongestLeaf(t *testing.T) {
	t.Run("deepest leaf wins", func(t *testing.T) {
		msgs := []db.Message{
			makeMsg("1", "s1", sql.NullString{}, "user", "root"),
			makeMsg("2", "s1", msgID("1"), "assistant", "shallow"),
			makeMsg("3", "s1", msgID("2"), "user", "deep"),
		}
		roots := BuildTree(msgs)
		leaf := GetLongestLeaf(roots)
		if leaf == nil {
			t.Fatal("GetLongestLeaf returned nil")
		}
		if leaf.Message.ID != "3" {
			t.Errorf("longest leaf = %q, want %q (deepest)", leaf.Message.ID, "3")
		}
	})

	t.Run("tie picks first by BFS order", func(t *testing.T) {
		msgs := []db.Message{
			makeMsg("1", "s1", sql.NullString{}, "user", "root"),
			makeMsg("2", "s1", msgID("1"), "assistant", "same-depth-1"),
			makeMsg("3", "s1", msgID("1"), "assistant", "same-depth-2"),
		}
		roots := BuildTree(msgs)
		leaf := GetLongestLeaf(roots)
		if leaf == nil {
			t.Fatal("GetLongestLeaf returned nil")
		}
		// Both have depth 1. BFS visits 2 first (it's first in parent's Children).
		if leaf.Message.ID != "2" {
			t.Errorf("longest leaf = %q, want %q (first in BFS)", leaf.Message.ID, "2")
		}
	})

	t.Run("no leaves returns nil", func(t *testing.T) {
		if GetLongestLeaf(nil) != nil {
			t.Error("expected nil for empty tree")
		}
	})
}

func TestGetLeafByID(t *testing.T) {
	msgs := []db.Message{
		makeMsg("1", "s1", sql.NullString{}, "user", "root"),
		makeMsg("2", "s1", msgID("1"), "assistant", "leaf"),
	}
	roots := BuildTree(msgs)

	t.Run("find leaf by ID", func(t *testing.T) {
		leaf := GetLeafByID(roots, "2")
		if leaf == nil || leaf.Message.ID != "2" {
			t.Error("GetLeafByID should find leaf 2")
		}
	})

	t.Run("non-leaf returns nil", func(t *testing.T) {
		leaf := GetLeafByID(roots, "1")
		if leaf != nil {
			t.Error("node 1 is not a leaf, should return nil")
		}
	})

	t.Run("non-existent returns nil", func(t *testing.T) {
		if GetLeafByID(roots, "99") != nil {
			t.Error("should return nil for non-existent ID")
		}
	})
}

func TestInsertNode(t *testing.T) {
	msgs := []db.Message{
		makeMsg("1", "s1", sql.NullString{}, "user", "root"),
	}
	roots := BuildTree(msgs)

	t.Run("insert child under root", func(t *testing.T) {
		newMsg := makeMsg("2", "s1", msgID("1"), "assistant", "new child")
		node := InsertNode(roots, "1", newMsg)
		if node == nil {
			t.Fatal("InsertNode returned nil")
		}
		if node.Message.ID != "2" {
			t.Errorf("inserted node ID = %q, want %q", node.Message.ID, "2")
		}
		if node.Depth != 1 {
			t.Errorf("inserted Depth = %d, want 1", node.Depth)
		}
		if node.Parent != roots[0] {
			t.Error("Parent not set correctly")
		}
		if len(roots[0].Children) != 1 {
			t.Errorf("root has %d children, want 1", len(roots[0].Children))
		}
	})

	t.Run("insert under non-existent parent", func(t *testing.T) {
		newMsg := makeMsg("99", "s1", msgID("nonexistent"), "assistant", "orphan")
		node := InsertNode(roots, "nonexistent", newMsg)
		if node != nil {
			t.Error("InsertNode should return nil for non-existent parent")
		}
	})
}

func TestToAgentMessages(t *testing.T) {
	t.Run("nil messages returns nil", func(t *testing.T) {
		if ToAgentMessages(nil) != nil {
			t.Error("expected nil")
		}
	})

	t.Run("empty messages returns nil", func(t *testing.T) {
		result := ToAgentMessages([]db.Message{})
		if result != nil {
			t.Error("expected nil")
		}
	})

	t.Run("converts user and assistant roles", func(t *testing.T) {
		msgs := []db.Message{
			makeMsg("1", "s1", sql.NullString{}, "user", "hello"),
			makeMsg("2", "s1", msgID("1"), "assistant", "hi there"),
		}
		result := ToAgentMessages(msgs)
		if len(result) != 2 {
			t.Fatalf("len = %d, want 2", len(result))
		}
		if result[0].Role != "user" || result[0].Content != "hello" {
			t.Errorf("msg 0: role=%s content=%s", result[0].Role, result[0].Content)
		}
		if result[1].Role != "assistant" || result[1].Content != "hi there" {
			t.Errorf("msg 1: role=%s content=%s", result[1].Role, result[1].Content)
		}
	})

	t.Run("converts tool messages with tool_call_id", func(t *testing.T) {
		msg := db.Message{
			ID:         "3",
			SessionID:  "s1",
			Role:       "tool",
			Parts:      "[]",
			Content:    sql.NullString{String: `{"result": "ok"}`, Valid: true},
			ToolCallID: sql.NullString{String: "call_123", Valid: true},
			ToolName:   sql.NullString{String: "web_fetch", Valid: true},
		}
		result := ToAgentMessages([]db.Message{msg})
		if len(result) != 1 {
			t.Fatalf("len = %d, want 1", len(result))
		}
		if result[0].Role != "tool" {
			t.Errorf("role = %q, want %q", result[0].Role, "tool")
		}
		if result[0].ToolCallID != "call_123" {
			t.Errorf("ToolCallID = %q, want %q", result[0].ToolCallID, "call_123")
		}
		if result[0].Name != "web_fetch" {
			t.Errorf("Name = %q, want %q", result[0].Name, "web_fetch")
		}
	})

	t.Run("converts system messages", func(t *testing.T) {
		msg := db.Message{
			ID:      "sys",
			Role:    "system",
			Parts:   "[]",
			Content: sql.NullString{String: "you are a bot", Valid: true},
		}
		result := ToAgentMessages([]db.Message{msg})
		if len(result) != 1 || result[0].Role != "system" || result[0].Content != "you are a bot" {
			t.Errorf("system message: %+v", result[0])
		}
	})

	t.Run("handles unknown role gracefully", func(t *testing.T) {
		msg := db.Message{
			ID:        "unk",
			SessionID: "s1",
			Role:      "unknown",
			Parts:     "[]",
			Content:   sql.NullString{String: "weird", Valid: true},
		}
		result := ToAgentMessages([]db.Message{msg})
		if len(result) != 1 || result[0].Role != "unknown" || result[0].Content != "weird" {
			t.Errorf("unknown role message: %+v", result[0])
		}
	})

	t.Run("handles null content", func(t *testing.T) {
		msg := db.Message{
			ID:        "null-c",
			SessionID: "s1",
			Role:      "user",
			Parts:     "[]",
			Content:   sql.NullString{},
		}
		result := ToAgentMessages([]db.Message{msg})
		if result[0].Content != "" {
			t.Errorf("Content = %q, want empty (null)", result[0].Content)
		}
	})
}
