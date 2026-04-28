package executors

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// HTTPExecutor provides base HTTP execution with configurable transport
type HTTPExecutor struct {
	client *http.Client
}

// NewHTTPExecutor creates a new HTTP executor with optional custom client
func NewHTTPExecutor(client *http.Client) *HTTPExecutor {
	if client == nil {
		client = http.DefaultClient
	}
	return &HTTPExecutor{client: client}
}

// Execute performs an HTTP request and returns the response
func (e *HTTPExecutor) Execute(ctx context.Context, req *http.Request) (*http.Response, error) {
	req = req.WithContext(ctx)
	return e.client.Do(req)
}

// ExecuteJSON performs an HTTP request and decodes JSON response
func (e *HTTPExecutor) ExecuteJSON(ctx context.Context, req *http.Request, result interface{}) error {
	resp, err := e.Execute(ctx, req)
	if err != nil {
		return fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("http %d: %s", resp.StatusCode, string(body))
	}

	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return fmt.Errorf("json decode failed: %w", err)
	}

	return nil
}

// SSEEvent represents a Server-Sent Event
type SSEEvent struct {
	Event string
	Data  string
	ID    string
	Retry int
}

// SSEParser parses Server-Sent Events from a reader
type SSEParser struct {
	scanner *bufio.Scanner
}

// NewSSEParser creates a new SSE parser
func NewSSEParser(r io.Reader) *SSEParser {
	scanner := bufio.NewScanner(r)
	// Increase buffer size for large SSE events
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	return &SSEParser{scanner: scanner}
}

// Next reads the next SSE event
func (p *SSEParser) Next() (*SSEEvent, error) {
	event := &SSEEvent{}
	var dataLines []string

	for p.scanner.Scan() {
		line := p.scanner.Text()

		// Empty line signals end of event
		if line == "" {
			if len(dataLines) > 0 || event.Event != "" || event.ID != "" {
				event.Data = strings.Join(dataLines, "\n")
				return event, nil
			}
			continue
		}

		// Skip comments
		if strings.HasPrefix(line, ":") {
			continue
		}

		// Parse field
		colonIdx := strings.Index(line, ":")
		if colonIdx == -1 {
			continue
		}

		field := line[:colonIdx]
		value := line[colonIdx+1:]
		if strings.HasPrefix(value, " ") {
			value = value[1:]
		}

		switch field {
		case "event":
			event.Event = value
		case "data":
			dataLines = append(dataLines, value)
		case "id":
			event.ID = value
		case "retry":
			fmt.Sscanf(value, "%d", &event.Retry)
		}
	}

	if err := p.scanner.Err(); err != nil {
		return nil, err
	}

	// EOF
	if len(dataLines) > 0 || event.Event != "" || event.ID != "" {
		event.Data = strings.Join(dataLines, "\n")
		return event, nil
	}

	return nil, io.EOF
}

// NDJSONParser parses newline-delimited JSON from a reader
type NDJSONParser struct {
	scanner *bufio.Scanner
}

// NewNDJSONParser creates a new NDJSON parser
func NewNDJSONParser(r io.Reader) *NDJSONParser {
	scanner := bufio.NewScanner(r)
	// Increase buffer size for large JSON lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	return &NDJSONParser{scanner: scanner}
}

// Next reads and decodes the next JSON object
func (p *NDJSONParser) Next(result interface{}) error {
	if !p.scanner.Scan() {
		if err := p.scanner.Err(); err != nil {
			return err
		}
		return io.EOF
	}

	line := bytes.TrimSpace(p.scanner.Bytes())
	if len(line) == 0 {
		return p.Next(result) // Skip empty lines
	}

	if err := json.Unmarshal(line, result); err != nil {
		return fmt.Errorf("json decode failed: %w", err)
	}

	return nil
}

// ErrorClassification represents error handling metadata
type ErrorClassification struct {
	Retryable      bool
	Backoff        bool
	CooldownMs     int
	RefreshAuth    bool
	RateLimited    bool
	AuthInvalid    bool
	ModelLocked    bool
	QuotaExhausted bool
}

// ParseHTTPError classifies an HTTP error response
func ParseHTTPError(statusCode int, body []byte) ErrorClassification {
	classification := ErrorClassification{}

	switch statusCode {
	case http.StatusTooManyRequests: // 429
		classification.Retryable = true
		classification.RateLimited = true
		classification.Backoff = true
		classification.CooldownMs = 60000 // Default 1 minute

	case http.StatusServiceUnavailable: // 503
		classification.Retryable = true
		classification.Backoff = true

	case http.StatusBadGateway, http.StatusGatewayTimeout: // 502, 504
		classification.Retryable = true
		classification.Backoff = true

	case http.StatusUnauthorized: // 401
		classification.RefreshAuth = true
		classification.Retryable = true

	case http.StatusForbidden: // 403
		classification.AuthInvalid = true

	case http.StatusPaymentRequired: // 402
		classification.QuotaExhausted = true

	case http.StatusInternalServerError: // 500
		classification.Retryable = true
		classification.Backoff = true
	}

	// Try to extract more specific error info from body
	if len(body) > 0 {
		bodyLower := strings.ToLower(string(body))
		
		if strings.Contains(bodyLower, "rate limit") || strings.Contains(bodyLower, "too many requests") {
			classification.RateLimited = true
			classification.Retryable = true
		}
		
		if strings.Contains(bodyLower, "quota") || strings.Contains(bodyLower, "insufficient") {
			classification.QuotaExhausted = true
		}
		
		if strings.Contains(bodyLower, "model") && strings.Contains(bodyLower, "locked") {
			classification.ModelLocked = true
		}
	}

	return classification
}
