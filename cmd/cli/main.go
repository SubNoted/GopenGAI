package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// API response types (mirror of server-side, for JSON decoding)
// ---------------------------------------------------------------------------

// SessionView mirrors the server's SessionView.
type SessionView struct {
	ID           string        `json:"id"`
	AgentName    string        `json:"agent_name"`
	Title        string        `json:"title"`
	Status       string        `json:"status"`
	MessageCount int64         `json:"message_count"`
	CreatedAt    int64         `json:"created_at"`
	UpdatedAt    int64         `json:"updated_at"`
	Messages     []MessageView `json:"messages,omitempty"`
}

// MessageView mirrors the server's MessageView.
type MessageView struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	AgentName string `json:"agent_name,omitempty"`
	CreatedAt int64  `json:"created_at"`
}

// AgentView mirrors the server's AgentView.
type AgentView struct {
	Name        string   `json:"name"`
	Model       string   `json:"model,omitempty"`
	Tools       []string `json:"tools,omitempty"`
	ParentAgent string   `json:"parent_agent,omitempty"`
	Description string   `json:"description,omitempty"`
	Mode        string   `json:"mode,omitempty"`
}

// MemoryView mirrors the server's MemoryView.
type MemoryView struct {
	ID        string `json:"id"`
	AgentName string `json:"agent_name"`
	Key       string `json:"key"`
	Value     string `json:"value"`
	Category  string `json:"category,omitempty"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// SSEEvent mirrors the server's SSEEvent for streaming.
type SSEEvent struct {
	Type       string          `json:"type"`
	Properties json.RawMessage `json:"properties,omitempty"`
}

// MessageCompleted is the payload of a message.complete SSE event.
type MessageCompleted struct {
	SessionID string      `json:"session_id"`
	MessageID string      `json:"message_id"`
	Content   string      `json:"content"`
	Role      string      `json:"role"`
	Model     string      `json:"model"`
	Usage     *ModelUsage `json:"usage,omitempty"`
}

// ModelUsage holds token usage info from the completion response.
type ModelUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ContentPart is the payload of a message.part.added event.
type ContentPart struct {
	SessionID string `json:"session_id"`
	Content   string `json:"content"`
	Index     int    `json:"index"`
}

// ---------------------------------------------------------------------------
// globals
// ---------------------------------------------------------------------------

var (
	serverURL string
	apiKey    string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "gopengai",
		Short: "GoPengAI — AI agent framework",
		Long: `GoPengAI is an AI agent framework with conversation history,
tool calling, and multi-agent delegation.

Start the server first: gopengai server (or ./api)
Then use these commands to interact.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			serverURL = strings.TrimSuffix(serverURL, "/")
			return nil
		},
	}

	rootCmd.PersistentFlags().StringVarP(&serverURL, "server", "s", "http://localhost:8080", "API server URL")
	rootCmd.PersistentFlags().StringVarP(&apiKey, "api-key", "k", "", "API key (set if server requires auth)")

	rootCmd.AddCommand(newChatCmd())
	rootCmd.AddCommand(newSessionCmd())
	rootCmd.AddCommand(newAgentsCmd())
	rootCmd.AddCommand(newMemoryCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

// httpClient is reused for all CLI requests.
var httpClient = &http.Client{}

// newRequest builds a request with the Authorization header if apiKey is set.
func newRequest(method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	return req, nil
}

func apiGET(path string) (*http.Response, error) {
	req, err := newRequest("GET", serverURL+path, nil)
	if err != nil {
		return nil, err
	}
	return httpClient.Do(req)
}

func apiPOST(path string, body any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode body: %w", err)
	}
	req, err := newRequest("POST", serverURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return httpClient.Do(req)
}

func apiPATCH(path string, body any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode body: %w", err)
	}
	req, err := newRequest("PATCH", serverURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return httpClient.Do(req)
}

func apiPUT(path string, body any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode body: %w", err)
	}
	req, err := newRequest("PUT", serverURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return httpClient.Do(req)
}

func apiDELETE(path string) (*http.Response, error) {
	req, err := newRequest("DELETE", serverURL+path, nil)
	if err != nil {
		return nil, err
	}
	return httpClient.Do(req)
}

// apiGETRaw makes a GET request and returns the response body without
// standard application/json content-type (used for SSE streaming).
func apiGETRaw(path string) (*http.Response, error) {
	req, err := newRequest("GET", serverURL+path, nil)
	if err != nil {
		return nil, err
	}
	return httpClient.Do(req)
}

// ---------------------------------------------------------------------------
// Chat command
// ---------------------------------------------------------------------------

func newChatCmd() *cobra.Command {
	var sessionID string
	var agentName string
	var stream bool

	cmd := &cobra.Command{
		Use:   "chat [message]",
		Short: "Send a message to the AI or enter interactive mode",
		Long: `Send a single message or enter interactive REPL mode.

With a message argument:
  gopengai chat "What is Go?"          → new session, send, print reply
  gopengai chat -s <id> "Hello again"  → continue existing session
  gopengai chat --stream "Tell me..."  → stream tokens via SSE

Without arguments:
  gopengai chat                         → interactive REPL mode
  gopengai chat --stream                → interactive SSE-streaming mode
  (type messages, see responses, type /quit to exit)`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				if stream {
					return runChatSSEREPL(sessionID, agentName)
				}
				return runChatREPL(sessionID, agentName)
			}
			if stream {
				return runChatSSEMessage(args[0], sessionID, agentName)
			}
			return runChatMessage(args[0], sessionID, agentName)
		},
	}

	cmd.Flags().StringVarP(&sessionID, "session", "S", "", "Session ID (omit to create new)")
	cmd.Flags().StringVarP(&agentName, "agent", "a", "", "Agent name for new session")
	cmd.Flags().BoolVar(&stream, "stream", false, "Stream response tokens via SSE")
	return cmd
}

// ---------------------------------------------------------------------------
// Sync chat (no SSE — awaits 202 + polls for completion)
// ---------------------------------------------------------------------------

func runChatMessage(message, sessionID, agentName string) error {
	if sessionID == "" {
		var err error
		sessionID, err = createSession(agentName, messageTrunc(message, 40))
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Session %s created\n", sessionID)
	}

	resp, err := apiPOST("/session/"+sessionID+"/message", map[string]string{
		"content": message,
	})
	if err != nil {
		return fmt.Errorf("send message: %w", err)
	}
	defer resp.Body.Close()

	// The async endpoint returns 202. For sync mode, just print status.
	if resp.StatusCode == http.StatusAccepted {
		fmt.Printf("Message accepted (session %s). Use --stream for live output.\n", sessionID)
		return nil
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	fmt.Println(prettyJSON(result))
	return nil
}

func runChatREPL(sessionID, agentName string) error {
	if sessionID == "" {
		var err error
		sessionID, err = createSession(agentName, "REPL chat")
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Session %s created. Type /quit to exit.\n", sessionID)
	}

	scanner := bufio.NewScanner(os.Stdin)
	fmt.Fprintf(os.Stderr, "You: ")
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			fmt.Fprintf(os.Stderr, "You: ")
			continue
		}
		if line == "/quit" || line == "/exit" {
			break
		}
		if line == "/session" {
			fmt.Fprintf(os.Stderr, "Session ID: %s\n", sessionID)
			fmt.Fprintf(os.Stderr, "You: ")
			continue
		}

		resp, err := apiPOST("/session/"+sessionID+"/message", map[string]string{
			"content": line,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			fmt.Fprintf(os.Stderr, "You: ")
			continue
		}

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			fmt.Fprintf(os.Stderr, "You: ")
			continue
		}
		resp.Body.Close()

		if errMsg, ok := result["error"]; ok {
			fmt.Fprintf(os.Stderr, "Error: %v\n", errMsg)
		} else if status, ok := result["status"]; ok && status == "accepted" {
			fmt.Println("[async — use --stream for live output]")
		} else {
			fmt.Println(prettyJSON(result))
		}

		fmt.Fprintf(os.Stderr, "\nYou: ")
	}

	return nil
}

// ---------------------------------------------------------------------------
// SSE streaming chat (subscribe → send → stream tokens)
// ---------------------------------------------------------------------------

func runChatSSEMessage(message, sessionID, agentName string) error {
	if sessionID == "" {
		var err error
		sessionID, err = createSession(agentName, messageTrunc(message, 40))
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Session %s created\n", sessionID)
	}
	return streamChat(message, sessionID)
}

func runChatSSEREPL(sessionID, agentName string) error {
	if sessionID == "" {
		var err error
		sessionID, err = createSession(agentName, "REPL chat")
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Session %s created. Type /quit to exit.\n", sessionID)
	}

	scanner := bufio.NewScanner(os.Stdin)
	fmt.Fprintf(os.Stderr, "You: ")
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			fmt.Fprintf(os.Stderr, "You: ")
			continue
		}
		if line == "/quit" || line == "/exit" {
			break
		}
		if line == "/session" {
			fmt.Fprintf(os.Stderr, "Session ID: %s\n", sessionID)
			fmt.Fprintf(os.Stderr, "You: ")
			continue
		}

		if err := streamChat(line, sessionID); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		fmt.Fprintf(os.Stderr, "\nYou: ")
	}

	return nil
}

// streamChat subscribes to the session SSE stream, sends a message, and
// prints tokens as they arrive. It blocks until the stream is done.
func streamChat(message, sessionID string) error {
	// Connect to SSE stream.
	resp, err := apiGETRaw("/session/" + sessionID + "/events")
	if err != nil {
		return fmt.Errorf("connect SSE: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("SSE connect failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	// Read the initial ": connected" comment.
	firstLine, _ := readSSELine(resp.Body)
	if firstLine != ": connected" {
		fmt.Fprintf(os.Stderr, "(unexpected SSE greeting: %q)\n", firstLine)
	}

	// Send message.
	msgResp, err := apiPOST("/session/"+sessionID+"/message", map[string]string{
		"content": message,
	})
	if err != nil {
		return fmt.Errorf("send message: %w", err)
	}
	body, _ := io.ReadAll(io.LimitReader(msgResp.Body, 1024))
	msgResp.Body.Close()

	if msgResp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("send failed (%d): %s", msgResp.StatusCode, strings.TrimSpace(string(body)))
	}

	// Stream tokens.
	var currentContent strings.Builder
	printed := false

	for {
		event, err := parseSSE(resp.Body)
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("read SSE: %w", err)
		}

		switch event.Type {
		case "message.part.added":
			var part ContentPart
			if err := json.Unmarshal(event.Properties, &part); err != nil {
				continue
			}
			if !printed {
				fmt.Print("\nAssistant: ")
				printed = true
			}
			fmt.Print(part.Content)
			currentContent.WriteString(part.Content)

		case "message.part.updated":
			var part ContentPart
			if err := json.Unmarshal(event.Properties, &part); err != nil {
				continue
			}
			// Replace content at index (simplified: just print delta)
			// For now, just print additional content.
			fmt.Print(part.Content)

		case "message.complete":
			var completed MessageCompleted
			if err := json.Unmarshal(event.Properties, &completed); err != nil {
				continue
			}
			if completed.Usage != nil {
				fmt.Printf("\n\n[model: %s | prompt: %d | completion: %d | total: %d tokens]\n",
					completed.Model,
					completed.Usage.PromptTokens,
					completed.Usage.CompletionTokens,
					completed.Usage.TotalTokens,
				)
			} else {
				fmt.Printf("\n\n[model: %s]\n", completed.Model)
			}
			return nil

		case "message.tool.started":
			// Brief tool call indicator.
			var props struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(event.Properties, &props); err == nil {
				fmt.Fprintf(os.Stderr, "\n  [tool: %s...]", props.Name)
			}

		case "message.tool.completed":
			fmt.Fprintf(os.Stderr, " done]")

		case "message.tool.error":
			fmt.Fprintf(os.Stderr, " error]")

		case "message.error":
			var props struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(event.Properties, &props); err == nil {
				fmt.Fprintf(os.Stderr, "\n[error: %s]\n", props.Error)
			} else {
				fmt.Fprintf(os.Stderr, "\n[an error occurred]\n")
			}
			return fmt.Errorf("agent error")

		case "session.status":
			var props struct {
				Status string `json:"status"`
			}
			if json.Unmarshal(event.Properties, &props) == nil && props.Status == "idle" {
				return nil
			}

		case "heartbeat":
			// Keepalive — ignore.
		}
	}
	return nil
}

// readSSELine reads one line from the SSE stream. SSE lines end with \n.
func readSSELine(r io.Reader) (string, error) {
	var line []byte
	buf := make([]byte, 1)
	for {
		_, err := r.Read(buf)
		if err != nil {
			return "", err
		}
		if buf[0] == '\n' {
			return string(line), nil
		}
		line = append(line, buf[0])
	}
}

// parseSSE reads and parses one complete SSE event (event + data blocks,
// terminated by an empty line).
func parseSSE(r io.Reader) (SSEEvent, error) {
	var evt SSEEvent
	for {
		line, err := readSSELine(r)
		if err != nil {
			return evt, err
		}
		if line == "" {
			// Empty line = end of event block.
			return evt, nil
		}
		if strings.HasPrefix(line, "event: ") {
			evt.Type = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			evt.Properties = json.RawMessage(strings.TrimPrefix(line, "data: "))
		}
		// Ignore comment lines (starting with ':') and empty event types.
	}
}

// ---------------------------------------------------------------------------
// Session command
// ---------------------------------------------------------------------------

func newSessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Manage chat sessions",
		Long:  "List, show, create, delete, fork, and switch branches.",
	}

	// --- list ---
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := apiGET("/session")
			if err != nil {
				return fmt.Errorf("list sessions: %w", err)
			}
			defer resp.Body.Close()

			var sessions []SessionView
			if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
				return fmt.Errorf("decode: %w", err)
			}

			if len(sessions) == 0 {
				fmt.Println("No sessions.")
				return nil
			}

			fmt.Printf("%-36s %-20s %-10s %s\n", "ID", "TITLE", "STATUS", "MSGS")
			for _, s := range sessions {
				fmt.Printf("%-36s %-20s %-10s %d\n",
					s.ID, messageTrunc(s.Title, 18), s.Status, s.MessageCount)
			}
			return nil
		},
	})

	// --- show ---
	cmd.AddCommand(&cobra.Command{
		Use:   "show <session-id>",
		Short: "Show session details and messages",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			resp, err := apiGET("/session/" + id)
			if err != nil {
				return fmt.Errorf("get session: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusNotFound {
				fmt.Println("Session not found.")
				return nil
			}

			var s SessionView
			if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
				return fmt.Errorf("decode: %w", err)
			}

			fmt.Printf("ID:     %s\n", s.ID)
			fmt.Printf("Title:  %s\n", s.Title)
			fmt.Printf("Agent:  %s\n", s.AgentName)
			fmt.Printf("Status: %s\n", s.Status)
			fmt.Printf("Msgs:   %d\n", s.MessageCount)
			fmt.Println("---")
			for _, m := range s.Messages {
				role := fmt.Sprintf("%-10s", m.Role+":")
				content := m.Content
				if len(content) > 120 {
					content = content[:120] + "..."
				}
				fmt.Printf("  %s %s\n", role, content)
			}
			return nil
		},
	})

	// --- create ---
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new session",
		RunE: func(cmd *cobra.Command, args []string) error {
			title, _ := cmd.Flags().GetString("title")
			agent, _ := cmd.Flags().GetString("agent")
			resp, err := apiPOST("/session", map[string]string{
				"title":      title,
				"agent_name": agent,
			})
			if err != nil {
				return fmt.Errorf("create session: %w", err)
			}
			defer resp.Body.Close()

			var s SessionView
			if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
				return fmt.Errorf("decode: %w", err)
			}

			fmt.Printf("Created session: %s\n", s.ID)
			fmt.Printf("Title: %s\n", s.Title)
			return nil
		},
	}
	createCmd.Flags().String("title", "", "Session title")
	createCmd.Flags().String("agent", "", "Agent name")
	cmd.AddCommand(createCmd)

	// --- delete ---
	cmd.AddCommand(&cobra.Command{
		Use:   "delete <session-id>",
		Short: "Delete a session and all its messages",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			resp, err := apiDELETE("/session/" + id)
			if err != nil {
				return fmt.Errorf("delete session: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusNotFound {
				fmt.Println("Session not found.")
				return nil
			}
			fmt.Println("Session deleted.")
			return nil
		},
	})

	// --- branches ---
	cmd.AddCommand(&cobra.Command{
		Use:   "branches <session-id>",
		Short: "List all branch leaf nodes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			resp, err := apiGET("/session/" + id + "/branches")
			if err != nil {
				return fmt.Errorf("list branches: %w", err)
			}
			defer resp.Body.Close()

			var leaves []MessageView
			if err := json.NewDecoder(resp.Body).Decode(&leaves); err != nil {
				return fmt.Errorf("decode: %w", err)
			}

			if len(leaves) == 0 {
				fmt.Println("No branches.")
				return nil
			}

			fmt.Printf("%-36s %-10s %-20s %s\n", "ID", "ROLE", "AGENT", "CONTENT")
			for _, l := range leaves {
				fmt.Printf("%-36s %-10s %-20s %s\n",
					l.ID, l.Role, l.AgentName, messageTrunc(l.Content, 60))
			}
			return nil
		},
	})

	// --- fork ---
	forkCmd := &cobra.Command{
		Use:   "fork <session-id>",
		Short: "Fork a session at a message",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			messageID, _ := cmd.Flags().GetString("message")
			content, _ := cmd.Flags().GetString("content")
			title, _ := cmd.Flags().GetString("title")
			agent, _ := cmd.Flags().GetString("agent")

			if messageID == "" {
				return fmt.Errorf("--message flag is required (message ID to fork at)")
			}
			if content == "" {
				return fmt.Errorf("--content flag is required (new user message)")
			}

			resp, err := apiPOST("/session/"+id+"/fork", map[string]string{
				"message_id": messageID,
				"content":    content,
				"title":      title,
				"agent_name": agent,
			})
			if err != nil {
				return fmt.Errorf("fork session: %w", err)
			}
			defer resp.Body.Close()

			var s SessionView
			if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
				return fmt.Errorf("decode: %w", err)
			}

			fmt.Printf("Forked session: %s\n", s.ID)
			fmt.Printf("Title: %s\n", s.Title)
			return nil
		},
	}
	forkCmd.Flags().StringP("message", "m", "", "Source message ID to fork at (required)")
	forkCmd.Flags().StringP("content", "c", "", "New user message content (required)")
	forkCmd.Flags().StringP("title", "t", "", "Title for the new session")
	forkCmd.Flags().StringP("agent", "a", "", "Agent name for the new session")
	cmd.AddCommand(forkCmd)

	// --- switch ---
	switchCmd := &cobra.Command{
		Use:   "switch <session-id>",
		Short: "Select an active branch by leaf ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			leaf, _ := cmd.Flags().GetString("leaf")

			if leaf == "" {
				return fmt.Errorf("--leaf flag is required (leaf message ID)")
			}

			resp, err := apiPUT("/session/"+id+"/branch", map[string]string{
				"leaf_id": leaf,
			})
			if err != nil {
				return fmt.Errorf("switch branch: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusBadRequest {
				body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
				return fmt.Errorf("switch failed: %s", strings.TrimSpace(string(body)))
			}

			fmt.Println("Branch switched.")
			return nil
		},
	}
	switchCmd.Flags().StringP("leaf", "l", "", "Leaf message ID to select (required)")
	cmd.AddCommand(switchCmd)

	return cmd
}

// ---------------------------------------------------------------------------
// Agents command
// ---------------------------------------------------------------------------

func newAgentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Manage agents",
		Long:  "List available agents and show agent details.",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all available agents",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := apiGET("/agents")
			if err != nil {
				return fmt.Errorf("list agents: %w", err)
			}
			defer resp.Body.Close()

			var agents []AgentView
			if err := json.NewDecoder(resp.Body).Decode(&agents); err != nil {
				return fmt.Errorf("decode: %w", err)
			}

			if len(agents) == 0 {
				fmt.Println("No agents available.")
				return nil
			}

			fmt.Printf("%-20s %-15s %-12s %s\n", "NAME", "MODEL", "TOOLS", "DESCRIPTION")
			for _, a := range agents {
				tools := strings.Join(a.Tools, ", ")
				if tools == "" {
					tools = "—"
				}
				desc := messageTrunc(a.Description, 50)
				fmt.Printf("%-20s %-15s %-12s %s\n", a.Name, a.Model, tools, desc)
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "info <agent-name>",
		Short: "Show agent details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			resp, err := apiGET("/agents/" + name)
			if err != nil {
				return fmt.Errorf("get agent: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusNotFound {
				fmt.Printf("Agent %q not found.\n", name)
				return nil
			}

			var a AgentView
			if err := json.NewDecoder(resp.Body).Decode(&a); err != nil {
				return fmt.Errorf("decode: %w", err)
			}

			fmt.Printf("Name:        %s\n", a.Name)
			fmt.Printf("Model:       %s\n", a.Model)
			fmt.Printf("Parent:      %s\n", a.ParentAgent)
			fmt.Printf("Mode:        %s\n", a.Mode)
			fmt.Printf("Description: %s\n", a.Description)
			fmt.Printf("Tools:       %s\n", strings.Join(a.Tools, ", "))
			return nil
		},
	})

	return cmd
}

// ---------------------------------------------------------------------------
// Memory command
// ---------------------------------------------------------------------------

func newMemoryCmd() *cobra.Command {
	var agentName string

	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Manage agent memory",
		Long:  "List and retrieve memory facts for an agent.",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Trim trailing slash (root PersistentPreRun already handles)
			return nil
		},
	}

	cmd.PersistentFlags().StringVarP(&agentName, "agent", "a", "", "Agent name (default: server default agent)")

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List memory facts for an agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "/memory"
			if agentName != "" {
				path += "?agent=" + agentName
			}
			resp, err := apiGET(path)
			if err != nil {
				return fmt.Errorf("list memory: %w", err)
			}
			defer resp.Body.Close()

			var facts []MemoryView
			if err := json.NewDecoder(resp.Body).Decode(&facts); err != nil {
				return fmt.Errorf("decode: %w", err)
			}

			if len(facts) == 0 {
				fmt.Println("No memory facts.")
				return nil
			}

			fmt.Printf("%-20s %-15s %s\n", "KEY", "CATEGORY", "VALUE")
			for _, f := range facts {
				cat := f.Category
				if cat == "" {
					cat = "—"
				}
				fmt.Printf("%-20s %-15s %s\n", f.Key, cat, messageTrunc(f.Value, 60))
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "get <key>",
		Short: "Get a specific memory fact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			path := "/memory/" + key
			if agentName != "" {
				path += "?agent=" + agentName
			}
			resp, err := apiGET(path)
			if err != nil {
				return fmt.Errorf("get memory: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusNotFound {
				fmt.Println("Memory fact not found.")
				return nil
			}

			var fact MemoryView
			if err := json.NewDecoder(resp.Body).Decode(&fact); err != nil {
				return fmt.Errorf("decode: %w", err)
			}

			fmt.Printf("Key:      %s\n", fact.Key)
			fmt.Printf("Agent:    %s\n", fact.AgentName)
			fmt.Printf("Category: %s\n", fact.Category)
			fmt.Printf("Value:    %s\n", fact.Value)
			return nil
		},
	})

	return cmd
}

// ---------------------------------------------------------------------------
// Utils
// ---------------------------------------------------------------------------

// createSession creates a new session and returns its ID.
func createSession(agentName, title string) (string, error) {
	resp, err := apiPOST("/session", map[string]string{
		"title":      title,
		"agent_name": agentName,
	})
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	defer resp.Body.Close()
	var s SessionView
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return "", fmt.Errorf("decode session: %w", err)
	}
	return s.ID, nil
}

// messageTrunc truncates a string to max runes, appending "..." if needed.
func messageTrunc(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}

// prettyJSON pretty-prints any value as indented JSON. Returns the raw value
// as a string on encoding failure.
func prettyJSON(v interface{}) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
