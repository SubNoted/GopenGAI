package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// API response types (mirror of server-side, for JSON decoding)
// ---------------------------------------------------------------------------

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

type MessageView struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	AgentName string `json:"agent_name,omitempty"`
	CreatedAt int64  `json:"created_at"`
}

type ChatResponse struct {
	SessionID string `json:"session_id"`
	MessageID string `json:"message_id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	Model     string `json:"model"`
	Error     string `json:"error,omitempty"`
}

// ---------------------------------------------------------------------------
// globals
// ---------------------------------------------------------------------------

var serverURL string

func main() {
	rootCmd := &cobra.Command{
		Use:   "gopengai",
		Short: "GoPengAI — AI agent framework",
		Long: `GoPengAI is an AI agent framework with conversation history,
tool calling, and multi-agent delegation.

Start the server first: gopengai server (or ./api)
Then use these commands to interact.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Trim trailing slash
			serverURL = strings.TrimSuffix(serverURL, "/")
			return nil
		},
	}

	rootCmd.PersistentFlags().StringVarP(&serverURL, "server", "s", "http://localhost:8080", "API server URL")

	// Subcommands
	rootCmd.AddCommand(newChatCmd())
	rootCmd.AddCommand(newSessionCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

func apiGET(path string) (*http.Response, error) {
	return http.Get(serverURL + path)
}

func apiPOST(path string, body any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode body: %w", err)
	}
	return http.Post(serverURL+path, "application/json", bytes.NewReader(data))
}

func apiDELETE(path string) (*http.Response, error) {
	req, err := http.NewRequest("DELETE", serverURL+path, nil)
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(req)
}

// ---------------------------------------------------------------------------
// Chat command
// ---------------------------------------------------------------------------

func newChatCmd() *cobra.Command {
	var sessionID string
	var agentName string

	cmd := &cobra.Command{
		Use:   "chat [message]",
		Short: "Send a message to the AI or enter interactive mode",
		Long: `Send a single message or enter interactive REPL mode.

With a message argument:
  gopengai chat "What is Go?"          → new session, send, print reply
  gopengai chat -s <id> "Hello again"  → continue existing session

Without arguments:
  gopengai chat                         → interactive REPL mode
  (type messages, see responses, type /quit to exit)`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return runChatREPL(sessionID, agentName)
			}
			return runChatMessage(args[0], sessionID, agentName)
		},
	}

	cmd.Flags().StringVarP(&sessionID, "session", "S", "", "Session ID (omit to create new)")
	cmd.Flags().StringVarP(&agentName, "agent", "a", "", "Agent name for new session")
	return cmd
}

func runChatMessage(message, sessionID, agentName string) error {
	// Create session if not provided.
	if sessionID == "" {
		resp, err := apiPOST("/session", map[string]string{
			"title":      messageTrunc(message, 40),
			"agent_name": agentName,
		})
		if err != nil {
			return fmt.Errorf("create session: %w", err)
		}
		var s SessionView
		if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
			resp.Body.Close()
			return fmt.Errorf("decode session: %w", err)
		}
		resp.Body.Close()
		sessionID = s.ID
		fmt.Fprintf(os.Stderr, "Session %s created\n", sessionID)
	}

	// Send message.
	resp, err := apiPOST("/session/"+sessionID+"/message", map[string]string{
		"content": message,
	})
	if err != nil {
		return fmt.Errorf("send message: %w", err)
	}
	defer resp.Body.Close()

	var cr ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	if cr.Error != "" {
		fmt.Fprintf(os.Stderr, "Error: %s\n", cr.Error)
		return nil
	}

	// Print assistant response only (role is "assistant").
	fmt.Println(cr.Content)
	return nil
}

func runChatREPL(sessionID, agentName string) error {
	if sessionID == "" {
		resp, err := apiPOST("/session", map[string]string{
			"title":      "REPL chat",
			"agent_name": agentName,
		})
		if err != nil {
			return fmt.Errorf("create session: %w", err)
		}
		var s SessionView
		if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
			resp.Body.Close()
			return fmt.Errorf("decode session: %w", err)
		}
		resp.Body.Close()
		sessionID = s.ID
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

		// Send message.
		resp, err := apiPOST("/session/"+sessionID+"/message", map[string]string{
			"content": line,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			fmt.Fprintf(os.Stderr, "You: ")
			continue
		}

		var cr ChatResponse
		if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
			resp.Body.Close()
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			fmt.Fprintf(os.Stderr, "You: ")
			continue
		}
		resp.Body.Close()

		if cr.Error != "" {
			fmt.Fprintf(os.Stderr, "Error: %s\n", cr.Error)
		} else {
			fmt.Printf("%s\n\n", cr.Content)
		}

		fmt.Fprintf(os.Stderr, "You: ")
	}

	return nil
}

// ---------------------------------------------------------------------------
// Session command
// ---------------------------------------------------------------------------

func newSessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Manage chat sessions",
		Long:  "List, show, create, and delete chat sessions.",
	}

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
				title := messageTrunc(s.Title, 18)
				fmt.Printf("%-36s %-20s %-10s %d\n", s.ID, title, s.Status, s.MessageCount)
			}
			return nil
		},
	})

	showCmd := &cobra.Command{
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
	}

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

	deleteCmd := &cobra.Command{
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
	}

	cmd.AddCommand(showCmd)
	cmd.AddCommand(createCmd)
	cmd.AddCommand(deleteCmd)
	return cmd
}

// ---------------------------------------------------------------------------
// Utils
// ---------------------------------------------------------------------------

func messageTrunc(s string, max int) string {
	// Count runes, not bytes, to avoid cutting multi-byte characters.
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	// Truncate by rune count.
	runes := []rune(s)
	return string(runes[:max]) + "..."
}
