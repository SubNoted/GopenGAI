package history

import (
	"database/sql"

	"gopengai/internal/agent"
	"gopengai/internal/db"
)

// TreeNode represents a single node in the conversation tree.
type TreeNode struct {
	Message  db.Message
	Parent   *TreeNode // pointer to parent (set during BuildTree)
	Children []*TreeNode
	Depth    int
}

// ---------------------------------------------------------------------------
// Tree construction
// ---------------------------------------------------------------------------

// BuildTree takes a flat list of messages (with parent_id pointers) and
// constructs a tree, returning the root nodes (messages with no parent).
// Orphan messages (parent_id pointing to a message not in the set) are also
// treated as roots so they are not silently dropped.
func BuildTree(messages []db.Message) []*TreeNode {
	if len(messages) == 0 {
		return nil
	}

	// Index all messages by ID.
	index := make(map[string]*TreeNode, len(messages))
	for i := range messages {
		index[messages[i].ID] = &TreeNode{
			Message:  messages[i],
			Children: nil,
			Depth:    0,
		}
	}

	// Link children to parents and set Parent pointers.
	for _, node := range index {
		parentID := node.Message.ParentID
		if !parentID.Valid {
			continue // root node
		}
		parent, ok := index[parentID.String]
		if !ok {
			continue // parent not in this set (orphan), treat as root
		}
		parent.Children = append(parent.Children, node)
		node.Parent = parent
	}

	// Collect root nodes: messages with no parent_id OR whose parent is
	// not in this set (orphans).
	roots := make([]*TreeNode, 0, len(messages))
	visited := make(map[*TreeNode]bool, len(index))
	for _, node := range index {
		if !node.Message.ParentID.Valid {
			node.Depth = 0
			roots = append(roots, node)
			visited[node] = true
		}
	}

	// Also collect orphan messages (valid parent_id but parent not in set).
	for _, node := range index {
		if visited[node] {
			continue
		}
		parentID := node.Message.ParentID
		if !parentID.Valid {
			continue
		}
		if _, ok := index[parentID.String]; !ok {
			node.Depth = 0
			roots = append(roots, node)
			visited[node] = true
		}
	}

	// If we still have no roots but we have messages, all messages have a
	// parent_id pointing to another message in the set with no true root.
	// Treat every node as a root (should not happen in a valid tree, but
	// handles edge cases gracefully).
	if len(roots) == 0 {
		for _, node := range index {
			if !visited[node] {
				node.Depth = 0
				roots = append(roots, node)
				visited[node] = true
			}
		}
	}

	// Assign depths via BFS from each root.
	for _, root := range roots {
		assignDepths(root, 0)
	}

	return roots
}

// assignDepths recursively assigns Depth to each node.
func assignDepths(node *TreeNode, depth int) {
	node.Depth = depth
	for _, child := range node.Children {
		assignDepths(child, depth+1)
	}
}

// ---------------------------------------------------------------------------
// Tree navigation
// ---------------------------------------------------------------------------

// FindNode searches the tree for a node by message ID.
// It searches breadth-first across all roots.
func FindNode(roots []*TreeNode, id string) *TreeNode {
	if len(roots) == 0 {
		return nil
	}

	queue := make([]*TreeNode, 0, len(roots))
	queue = append(queue, roots...)

	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		if node.Message.ID == id {
			return node
		}
		queue = append(queue, node.Children...)
	}
	return nil
}

// GetPathFromRoot walks up from a node to the root using Parent pointers,
// collecting nodes in order (root first, leaf last). Returns nil if node is nil.
func GetPathFromRoot(node *TreeNode) []*TreeNode {
	if node == nil {
		return nil
	}

	// Walk up to root using Parent pointers.
	var reversed []*TreeNode
	for curr := node; curr != nil; curr = curr.Parent {
		reversed = append(reversed, curr)
	}

	// Reverse so root is first.
	path := make([]*TreeNode, len(reversed))
	for i, n := range reversed {
		path[len(reversed)-1-i] = n
	}
	return path
}

// ---------------------------------------------------------------------------
// Leaf selection
// ---------------------------------------------------------------------------

// IsLeaf returns true if the node has no children.
func IsLeaf(node *TreeNode) bool {
	return len(node.Children) == 0
}

// GetAllLeavesAsNodes returns all leaf nodes from the given roots.
func GetAllLeavesAsNodes(roots []*TreeNode) []*TreeNode {
	if len(roots) == 0 {
		return nil
	}

	var leaves []*TreeNode
	queue := make([]*TreeNode, 0, len(roots))
	queue = append(queue, roots...)

	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		if IsLeaf(node) {
			leaves = append(leaves, node)
		}
		queue = append(queue, node.Children...)
	}
	return leaves
}

// GetLongestLeaf returns the leaf node with the deepest depth (longest path).
// If there are multiple leaves at the same max depth, the first one encountered
// (by BFS order) is returned.
func GetLongestLeaf(roots []*TreeNode) *TreeNode {
	leaves := GetAllLeavesAsNodes(roots)
	if len(leaves) == 0 {
		return nil
	}

	best := leaves[0]
	for _, leaf := range leaves[1:] {
		if leaf.Depth > best.Depth {
			best = leaf
		}
	}
	return best
}

// GetLeafByID finds a leaf node by its message ID across all roots.
// Returns nil if the ID is not found or the node is not a leaf.
func GetLeafByID(roots []*TreeNode, id string) *TreeNode {
	node := FindNode(roots, id)
	if node == nil || !IsLeaf(node) {
		return nil
	}
	return node
}

// ---------------------------------------------------------------------------
// Tree mutation
// ---------------------------------------------------------------------------

// InsertNode adds a new message as a child of the node identified by parentID
// within the given tree roots. The tree is mutated in-place by appending the
// new node to the parent's Children slice. Returns the newly created TreeNode,
// or nil if the parent was not found in the tree.
func InsertNode(roots []*TreeNode, parentID string, msg db.Message) *TreeNode {
	parent := FindNode(roots, parentID)
	if parent == nil {
		return nil
	}

	node := &TreeNode{
		Message:  msg,
		Parent:   parent,
		Children: nil,
		Depth:    parent.Depth + 1,
	}
	parent.Children = append(parent.Children, node)
	return node
}

// ---------------------------------------------------------------------------
// Context building (convert tree path to agent/LLM messages)
// ---------------------------------------------------------------------------

// ToAgentMessages converts a root-to-leaf path of db.Messages into
// agent.Message slices, filtering and mapping fields appropriately.
func ToAgentMessages(messages []db.Message) []agent.Message {
	if len(messages) == 0 {
		return nil
	}

	out := make([]agent.Message, 0, len(messages))
	for _, m := range messages {
		switch m.Role {
		case "user", "assistant":
			msg := agent.Message{
				Role:    m.Role,
				Content: nullStringValue(m.Content),
			}
			if m.AgentName.Valid {
				msg.Name = m.AgentName.String
			}
			out = append(out, msg)
		case "tool":
			msg := agent.Message{
				Role:       "tool",
				Content:    nullStringValue(m.Content),
				ToolCallID: nullStringValue(m.ToolCallID),
				Name:       nullStringValue(m.ToolName),
			}
			out = append(out, msg)
		case "system":
			out = append(out, agent.Message{
				Role:    "system",
				Content: nullStringValue(m.Content),
			})
		default:
			out = append(out, agent.Message{
				Role:    m.Role,
				Content: nullStringValue(m.Content),
			})
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func nullStringValue(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}
