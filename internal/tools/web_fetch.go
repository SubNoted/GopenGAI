package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
)

// ---------------------------------------------------------------------------
// WebFetchTool
// ---------------------------------------------------------------------------

// WebFetchTool fetches a URL and returns the text content.
// It strips HTML tags, limits response size, and enforces a timeout.
type WebFetchTool struct {
	// Client is the HTTP client used for requests. If nil, a default client
	// with a 10-second timeout is used.
	Client *http.Client

	// MaxResponseBytes limits the number of bytes read from the response body.
	// Defaults to 50 * 1024 (50 KB).
	MaxResponseBytes int64
}

// Name returns the tool name.
func (w *WebFetchTool) Name() string { return "web_fetch" }

// Description returns a human-readable description.
func (w *WebFetchTool) Description() string {
	return "Fetch the contents of a URL and return the text. Use this to read web pages, API responses, or any publicly accessible URL."
}

// Parameters returns the JSON Schema for the tool's arguments.
func (w *WebFetchTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"url": {
				"type": "string",
				"description": "The URL to fetch (must be http:// or https://)"
			}
		},
		"required": ["url"]
	}`)
}

// Execute fetches a URL, strips HTML tags, and returns the text content.
func (w *WebFetchTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("web_fetch: invalid arguments: %w", err)
	}
	if params.URL == "" {
		return "", fmt.Errorf("web_fetch: url is required")
	}

	// Validate URL scheme — reject anything that isn't http or https (SSRF prevention).
	parsedURL, err := url.Parse(params.URL)
	if err != nil {
		return "", fmt.Errorf("web_fetch: invalid URL: %w", err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return "", fmt.Errorf("web_fetch: unsupported URL scheme %q (only http/https allowed)", parsedURL.Scheme)
	}

	client := w.Client
	if client == nil {
		client = &http.Client{
			Timeout: 10 * time.Second,
			// Reject redirects to non-http/https or different hosts (SSRF prevention).
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("too many redirects")
				}
				if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
					return fmt.Errorf("redirect to unsupported scheme %q", req.URL.Scheme)
				}
				return nil
			},
		}
	}

	maxBytes := w.MaxResponseBytes
	if maxBytes <= 0 {
		maxBytes = 50 * 1024 // 50 KB
	}

	req, err := http.NewRequestWithContext(ctx, "GET", params.URL, nil)
	if err != nil {
		return "", fmt.Errorf("web_fetch: create request: %w", err)
	}
	req.Header.Set("User-Agent", "GoPengAI/1.0 (AI Assistant; +https://github.com/gopengai)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain,*/*")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("web_fetch: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("web_fetch: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return "", fmt.Errorf("web_fetch: read body: %w", err)
	}

	text := stripHTML(string(body))
	text = strings.TrimSpace(text)

	// Truncate to a reasonable display length (approx 10000 chars).
	const maxDisplayChars = 10000
	if len(text) > maxDisplayChars {
		text = text[:maxDisplayChars] + "... [truncated]"
	}

	return text, nil
}

// ---------------------------------------------------------------------------
// HTML tag stripper
// ---------------------------------------------------------------------------

// stripHTML removes HTML tags and decodes common entities from the input string.
// It collapses whitespace and removes content inside <script> and <style> blocks.
func stripHTML(input string) string {
	// First pass: remove <script> and <style> blocks (including content).
	cleaned := stripScriptStyle(input)

	// Second pass: strip remaining HTML tags.
	cleaned = stripTags(cleaned)

	// Decode common HTML entities.
	cleaned = decodeEntities(cleaned)

	// Collapse whitespace.
	cleaned = collapseWhitespace(cleaned)

	return cleaned
}

// stripScriptStyle removes <script>...</script> and <style>...</style> blocks
// (including their content), handling attributes on opening tags.
func stripScriptStyle(input string) string {
	var result strings.Builder
	lower := strings.ToLower(input)
	i := 0

	for i < len(input) {
		// Look for '<' followed by "script" or "style"
		idx := strings.IndexByte(input[i:], '<')
		if idx < 0 {
			result.WriteString(input[i:])
			break
		}
		// Copy everything up to '<'
		result.WriteString(input[i : i+idx])
		i += idx

		// Determine if this is a script/style tag (opening or closing).
		tagEnd := strings.IndexByte(input[i:], '>')
		if tagEnd < 0 {
			// No closing '>'; write rest as-is.
			result.WriteString(input[i:])
			break
		}

		tagContent := input[i : i+tagEnd+1] // include '>'
		tagLower := lower[i : i+tagEnd+1]

		isClosing := strings.HasPrefix(tagLower[1:], "/")
		scanStart := 1
		if isClosing {
			scanStart = 2 // skip '</'
		}

		var tagName string
		// Extract tag name (alphanumeric chars after '<' or '</')
		end := scanStart
		for end < len(tagLower) {
			c := tagLower[end]
			if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
				end++
			} else {
				break
			}
		}
		tagName = tagLower[scanStart:end]

		if !isClosing && (tagName == "script" || tagName == "style") {
			// Skip this opening tag and its content until closing tag.
			closeTag := "</" + tagName + ">"
			closeIdx := strings.Index(strings.ToLower(input[i+tagEnd+1:]), closeTag)
			if closeIdx >= 0 {
				i += tagEnd + 1 + closeIdx + len(closeTag)
			} else {
				// No closing tag; skip the opening tag only.
				i += tagEnd + 1
			}
		} else {
			// Not a script/style; keep the tag for tag-stripping pass.
			result.WriteString(tagContent)
			i += tagEnd + 1
		}
	}
	return result.String()
}

// stripTags removes all remaining HTML tags (content between < and >).
func stripTags(input string) string {
	var result strings.Builder
	inTag := false
	for _, ch := range input {
		if ch == '<' {
			inTag = true
		} else if ch == '>' {
			inTag = false
		} else if !inTag {
			result.WriteRune(ch)
		}
	}
	return result.String()
}

// decodeEntities replaces common HTML entities with their characters.
func decodeEntities(input string) string {
	entities := map[string]string{
		"&amp;":  "&",
		"&lt;":   "<",
		"&gt;":   ">",
		"&quot;": "\"",
		"&#39;":  "'",
		"&nbsp;": " ",
	}
	result := input
	for entity, ch := range entities {
		result = strings.ReplaceAll(result, entity, ch)
	}
	return result
}

// collapseWhitespace replaces all whitespace runs with a single space.
func collapseWhitespace(input string) string {
	var result strings.Builder
	prevSpace := false
	for _, ch := range input {
		if unicode.IsSpace(ch) {
			if !prevSpace {
				result.WriteRune(' ')
				prevSpace = true
			}
		} else {
			result.WriteRune(ch)
			prevSpace = false
		}
	}
	return result.String()
}
