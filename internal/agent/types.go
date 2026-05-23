package agent

// ---------------------------------------------------------------------------
// Agent represents a loaded agent configuration from a .md file.
// ---------------------------------------------------------------------------

// Agent is the in-memory representation of an agent.
type Agent struct {
	Name         string            // agent name (from frontmatter or filename)
	SystemPrompt string            // body of the .md file (system instruction)
	Model        string            // LLM model override (empty = use default from config)
	Tools        []string          // list of tool names this agent can use
	ParentAgent  string            // parent agent name for delegation chains
	Permissions  map[string]string // tool_name → "allow"/"deny"
	ConfigPath   string            // path to the .md file

	// OpenCode-style fields (parsed from frontmatter).
	Description string            // short description of the agent's role
	Mode        string            // "primary" | "all" | "subagent" | "tool"
	Color       string            // UI accent colour (e.g. "#2196F3")
	AgentPerms  *AgentPermissions // structured permission block
}

// HasTool returns true if the agent has the given tool in its Tools list.
func (a *Agent) HasTool(name string) bool {
	for _, t := range a.Tools {
		if t == name {
			return true
		}
	}
	return false
}

// IsToolAllowed returns true if the tool is explicitly allowed (default deny).
// If permissions is empty, all tools in the Tools list are allowed.
func (a *Agent) IsToolAllowed(name string) bool {
	if len(a.Permissions) == 0 {
		return a.HasTool(name)
	}
	return a.Permissions[name] == "allow"
}

// ---------------------------------------------------------------------------
// In-memory message types for the agent engine.
// These are at a higher abstraction level than llm.Message.
// ---------------------------------------------------------------------------

// Message represents a single message within the agent engine.
type Message struct {
	Role       string     // "system" | "user" | "assistant" | "tool"
	Content    string     // text content (for role=user|assistant|tool)
	ToolCalls  []ToolCall // tool invocations (for role=assistant)
	ToolCallID string     // tool call ID this message is responding to (for role=tool)
	Name       string     // tool name (for role=tool)
}

// ToolCall represents a function call requested by the LLM (agent-level simplified).
type ToolCall struct {
	ID        string // unique call ID (e.g. "call_abc123")
	Name      string // tool/function name
	Arguments string // JSON-encoded arguments
}

// Response represents the final result from an agent processing cycle.
type Response struct {
	Content    string // final text response
	Usage      *Usage // token usage (nil if unknown)
	StopReason string // "stop" | "tool_calls" | "length" | "error"
	Error      string // error message if the agent failed
}

// Usage tracks token consumption for a response.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// ---------------------------------------------------------------------------
// AgentPermissions models the structured permission block used in opencode
// frontmatter. Each key is a tool name and the value is "allow" | "deny" | "ask".
// ---------------------------------------------------------------------------

// AgentPermissions defines file-level and tool-level access rules.
type AgentPermissions struct {
	Read       map[string]string `yaml:"read"`  // glob → "allow"|"deny"|"ask"
	Write      map[string]string `yaml:"write"` // glob → "allow"|"deny"|"ask"
	Edit       map[string]string `yaml:"edit"`  // glob → "allow"|"deny"|"ask"
	Bash       map[string]string `yaml:"bash"`  // glob → "allow"|"deny"|"ask"
	Glob       string            `yaml:"glob"`  // "allow" | "deny" | "ask"
	Grep       string            `yaml:"grep"`  // "allow" | "deny" | "ask"
	Task       map[string]string `yaml:"task"`  // subagent_name → "allow"|"deny"
	Skill      map[string]string `yaml:"skill"` // skill_name → "allow"|"deny"
	WebSearch  string            `yaml:"websearch"`
	WebFetch   string            `yaml:"webfetch"`
	CodeSearch string            `yaml:"codesearch"`
	Question   string            `yaml:"question"`
}

// ---------------------------------------------------------------------------
// Helper: YAML frontmatter model (used by loader.go)
// ---------------------------------------------------------------------------

// frontmatter is the YAML structure parsed from agent .md files.
// It supports both native gopengai fields and opencode-style fields.
type frontmatter struct {
	Name        string            `yaml:"name"`
	Model       string            `yaml:"model"`
	Tools       []string          `yaml:"tools"`
	ParentAgent string            `yaml:"parent_agent"`
	Permissions map[string]string `yaml:"permissions"`

	// OpenCode-style fields.
	Description string            `yaml:"description"`
	Mode        string            `yaml:"mode"`
	Color       string            `yaml:"color"`
	AgentPerms  *AgentPermissions `yaml:"permission"`
}
