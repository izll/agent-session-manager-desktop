package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// JSONRPCRequest represents a JSON-RPC 2.0 request
type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCMessage represents any JSON-RPC 2.0 message (request, response, or notification)
type JSONRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`      // nil for notifications
	Method  string          `json:"method,omitempty"`  // empty for responses
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *JSONRPCError) Error() string {
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

// Tool represents an MCP tool definition
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// ToolCallResult represents the result of a tool call
type ToolCallResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// ContentBlock represents a content block in a tool result
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// Client represents an MCP client that communicates with a server via stdio
type Client struct {
	cmd           *exec.Cmd
	stdin         io.WriteCloser
	stdout        io.ReadCloser
	stderr        io.ReadCloser
	scanner       *bufio.Scanner
	requestID     int64
	tools         []Tool
	stateMu       sync.Mutex // protects running state
	stdinMu       sync.Mutex // protects stdin writes
	responseChan  map[int64]chan *JSONRPCResponse
	responseMu    sync.RWMutex
	running       bool
	projectRoot   string
	serverReady   chan struct{}
}

// NewClient creates a new MCP client
// Uses Claude Code provider by default (no API key required)
func NewClient(projectRoot string) *Client {
	return &Client{
		projectRoot:  projectRoot,
		responseChan: make(map[int64]chan *JSONRPCResponse),
		serverReady:  make(chan struct{}),
	}
}

// Start starts the MCP server process
func (c *Client) Start() error {
	c.stateMu.Lock()
	if c.running {
		c.stateMu.Unlock()
		return nil
	}

	// Start the task-master-ai MCP server via npx
	// Uses Claude Code provider by default - no API key required
	c.cmd = exec.Command("npx", "-y", "task-master-ai")
	c.cmd.Env = append(os.Environ(),
		"TASK_MASTER_TOOLS=all",
		"TASK_MASTER_AI_PROVIDER=claude-code",
		"TASK_MASTER_AI_MODEL=sonnet",
	)

	if c.projectRoot != "" {
		c.cmd.Dir = c.projectRoot
	}

	var err error
	c.stdin, err = c.cmd.StdinPipe()
	if err != nil {
		c.stateMu.Unlock()
		return fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	c.stdout, err = c.cmd.StdoutPipe()
	if err != nil {
		c.stateMu.Unlock()
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	c.stderr, err = c.cmd.StderrPipe()
	if err != nil {
		c.stateMu.Unlock()
		return fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	if err := c.cmd.Start(); err != nil {
		c.stateMu.Unlock()
		return fmt.Errorf("failed to start MCP server: %w", err)
	}

	c.scanner = bufio.NewScanner(c.stdout)
	c.running = true
	c.stateMu.Unlock() // Release lock before starting goroutines

	// Start response reader goroutine - must start BEFORE waiting
	// This allows us to respond to server probes (ping, roots/list)
	go c.readResponses()

	// Start stderr reader goroutine (for debugging)
	go c.readStderr()

	// Wait for the server to start up (or timeout after 30 seconds)
	// During this time, readResponses() handles any server probes
	fmt.Println("MCP: waiting for server to start...")
	select {
	case <-c.serverReady:
		fmt.Println("MCP: server is ready, initializing...")
	case <-time.After(30 * time.Second):
		fmt.Println("MCP: timeout waiting for server, trying to initialize anyway...")
	}

	// Initialize connection and list tools
	if err := c.initialize(); err != nil {
		c.Stop()
		return fmt.Errorf("failed to initialize MCP connection: %w", err)
	}

	return nil
}

// Stop stops the MCP server process
func (c *Client) Stop() error {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()

	if !c.running {
		return nil
	}

	c.running = false

	if c.stdin != nil {
		c.stdin.Close()
	}
	if c.stdout != nil {
		c.stdout.Close()
	}
	if c.stderr != nil {
		c.stderr.Close()
	}

	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill()
		c.cmd.Wait()
	}

	return nil
}

// IsRunning returns true if the MCP server is running
func (c *Client) IsRunning() bool {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c.running
}

// readResponses reads JSON-RPC messages from the server (responses and requests)
func (c *Client) readResponses() {
	for c.scanner.Scan() {
		line := c.scanner.Text()
		if line == "" {
			continue
		}

		// Skip non-JSON lines (warnings, logs, etc.)
		trimmed := line
		for len(trimmed) > 0 && trimmed[0] == ' ' {
			trimmed = trimmed[1:]
		}
		if len(trimmed) == 0 || trimmed[0] != '{' {
			fmt.Printf("MCP stdout (non-JSON): %s\n", line)
			continue
		}

		fmt.Printf("MCP <<< received: %s\n", line)

		var msg JSONRPCMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			fmt.Printf("MCP: failed to parse message: %v\n", err)
			continue
		}

		// Check if this is a server request (has method AND id)
		if msg.Method != "" && msg.ID != nil {
			c.handleServerRequest(msg)
			continue
		}

		// Check if this is a notification (has method but no id)
		if msg.Method != "" && msg.ID == nil {
			fmt.Printf("MCP notification: %s\n", msg.Method)
			continue
		}

		// Otherwise it's a response to our request
		if msg.ID != nil {
			response := &JSONRPCResponse{
				JSONRPC: msg.JSONRPC,
				ID:      *msg.ID,
				Result:  msg.Result,
				Error:   msg.Error,
			}

			c.responseMu.RLock()
			ch, ok := c.responseChan[response.ID]
			c.responseMu.RUnlock()

			if ok {
				ch <- response
			} else {
				fmt.Printf("MCP: received response for unknown request ID %d\n", response.ID)
			}
		}
	}
}

// handleServerRequest handles requests from the server to the client
func (c *Client) handleServerRequest(msg JSONRPCMessage) {
	fmt.Printf("MCP server request: %s (id=%d)\n", msg.Method, *msg.ID)

	var result interface{}

	switch msg.Method {
	case "ping":
		// Respond to ping
		result = map[string]interface{}{}

	case "roots/list":
		// Return list of filesystem roots the client has access to
		result = map[string]interface{}{
			"roots": []map[string]interface{}{
				{
					"uri":  "file://" + c.projectRoot,
					"name": "Project Root",
				},
			},
		}

	case "sampling/createMessage":
		// We don't support sampling, return error
		c.sendErrorResponse(*msg.ID, -32601, "Method not supported: sampling/createMessage")
		return

	default:
		fmt.Printf("MCP: unknown server request method: %s\n", msg.Method)
		c.sendErrorResponse(*msg.ID, -32601, "Method not found: "+msg.Method)
		return
	}

	c.sendSuccessResponse(*msg.ID, result)
}

// sendSuccessResponse sends a successful response to a server request
func (c *Client) sendSuccessResponse(id int64, result interface{}) {
	response := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}

	data, err := json.Marshal(response)
	if err != nil {
		fmt.Printf("MCP: failed to marshal response: %v\n", err)
		return
	}

	fmt.Printf("MCP >>> responding: %s\n", data)
	c.stdinMu.Lock()
	fmt.Fprintf(c.stdin, "%s\n", data)
	c.stdinMu.Unlock()
}

// sendErrorResponse sends an error response to a server request
func (c *Client) sendErrorResponse(id int64, code int, message string) {
	response := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]interface{}{
			"code":    code,
			"message": message,
		},
	}

	data, err := json.Marshal(response)
	if err != nil {
		fmt.Printf("MCP: failed to marshal error response: %v\n", err)
		return
	}

	fmt.Printf("MCP >>> responding error: %s\n", data)
	c.stdinMu.Lock()
	fmt.Fprintf(c.stdin, "%s\n", data)
	c.stdinMu.Unlock()
}

// readStderr reads and logs stderr from the server
func (c *Client) readStderr() {
	scanner := bufio.NewScanner(c.stderr)
	serverReadySignaled := false
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Printf("MCP stderr: %s\n", line)

		// Signal when server is ready (look for "Registered X tools successfully")
		if !serverReadySignaled && (strings.Contains(line, "tools successfully") || strings.Contains(line, "Registered") && strings.Contains(line, "tools")) {
			serverReadySignaled = true
			close(c.serverReady)
			fmt.Println("MCP: server ready signal received")
		}
	}
}

// sendRequest sends a JSON-RPC request and waits for the response
func (c *Client) sendRequest(method string, params interface{}) (*JSONRPCResponse, error) {
	return c.sendRequestWithTimeout(method, params, 60*time.Second)
}

// sendRequestWithTimeout sends a JSON-RPC request with a custom timeout
func (c *Client) sendRequestWithTimeout(method string, params interface{}, timeout time.Duration) (*JSONRPCResponse, error) {
	id := atomic.AddInt64(&c.requestID, 1)

	request := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create response channel
	responseCh := make(chan *JSONRPCResponse, 1)
	c.responseMu.Lock()
	c.responseChan[id] = responseCh
	c.responseMu.Unlock()

	defer func() {
		c.responseMu.Lock()
		delete(c.responseChan, id)
		c.responseMu.Unlock()
	}()

	// Send request
	fmt.Printf("MCP >>> sending request: %s\n", string(data))
	c.stdinMu.Lock()
	_, err = fmt.Fprintf(c.stdin, "%s\n", data)
	c.stdinMu.Unlock()

	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// Wait for response with timeout
	select {
	case response := <-responseCh:
		if response.Error != nil {
			return nil, response.Error
		}
		return response, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("request timeout after %v for method %s", timeout, method)
	}
}

// initialize initializes the MCP connection
func (c *Client) initialize() error {
	// Send initialize request with sampling and roots capabilities
	initParams := map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]interface{}{
			"sampling": map[string]interface{}{},
			"roots": map[string]interface{}{
				"listChanged": true,
			},
		},
		"clientInfo": map[string]string{
			"name":    "asmgr-desktop",
			"version": "0.1.0",
		},
	}

	_, err := c.sendRequest("initialize", initParams)
	if err != nil {
		return fmt.Errorf("initialize failed: %w", err)
	}

	// Send initialized notification
	notif := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	data, _ := json.Marshal(notif)
	fmt.Printf("MCP >>> sending notification: %s\n", data)
	c.stdinMu.Lock()
	fmt.Fprintf(c.stdin, "%s\n", data)
	c.stdinMu.Unlock()

	// List available tools
	return c.listTools()
}

// listTools retrieves the list of available tools from the server
func (c *Client) listTools() error {
	response, err := c.sendRequest("tools/list", map[string]interface{}{})
	if err != nil {
		return fmt.Errorf("tools/list failed: %w", err)
	}

	var result struct {
		Tools []Tool `json:"tools"`
	}

	if err := json.Unmarshal(response.Result, &result); err != nil {
		return fmt.Errorf("failed to parse tools list: %w", err)
	}

	c.tools = result.Tools
	fmt.Printf("MCP: loaded %d tools\n", len(c.tools))

	return nil
}

// GetTools returns the list of available tools
func (c *Client) GetTools() []Tool {
	return c.tools
}

// CallTool calls an MCP tool with the given arguments
func (c *Client) CallTool(name string, args map[string]interface{}) (*ToolCallResult, error) {
	return c.CallToolWithTimeout(name, args, 60*time.Second)
}

// CallToolWithTimeout calls an MCP tool with a custom timeout
func (c *Client) CallToolWithTimeout(name string, args map[string]interface{}, timeout time.Duration) (*ToolCallResult, error) {
	if !c.IsRunning() {
		return nil, fmt.Errorf("MCP client not running")
	}

	// Add projectRoot to args if not present
	if c.projectRoot != "" {
		if _, ok := args["projectRoot"]; !ok {
			args["projectRoot"] = c.projectRoot
		}
	}

	params := map[string]interface{}{
		"name":      name,
		"arguments": args,
	}

	response, err := c.sendRequestWithTimeout("tools/call", params, timeout)
	if err != nil {
		return nil, fmt.Errorf("tool call failed: %w", err)
	}

	var result ToolCallResult
	if err := json.Unmarshal(response.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse tool result: %w", err)
	}

	return &result, nil
}

// GetToolResultText extracts text from a tool call result
func GetToolResultText(result *ToolCallResult) string {
	if result == nil || len(result.Content) == 0 {
		return ""
	}

	text := ""
	for _, block := range result.Content {
		if block.Type == "text" {
			text += block.Text
		}
	}
	return text
}
