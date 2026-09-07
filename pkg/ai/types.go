package ai

// Message represents a single chat turn in the conversation.
type Message struct {
	Role       string     `json:"role"` // "system", "user", "assistant", "tool"
	Content    string     `json:"content"`
	Images     []string   `json:"images,omitempty"` // Base64-encoded images for multimodal vision models like Qwen-VL
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"` // For tool responses
}

// ToolCall represents an instruction by the LLM to call a specific tool.
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"` // "function"
	Function FunctionCallInfo `json:"function"`
}

// FunctionCallInfo contains the name and serialized arguments of a function call.
type FunctionCallInfo struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

// ToolDefinition defines an invokable tool exposed to the LLM (OpenAI / Ollama / MCP format).
type ToolDefinition struct {
	Type     string             `json:"type"` // "function"
	Function FunctionDefinition `json:"function"`
}

// FunctionDefinition holds JSON schema metadata for a function.
type FunctionDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// ChatRequest represents the payload sent to the agent/chat endpoint.
type ChatRequest struct {
	Provider       ProviderType `json:"provider"`                 // "ollama", "openrouter" or "quick" ("direct" is a legacy alias)
	Model          string       `json:"model"`                    // e.g. "qwen3-vl:8b"
	Prompt         string       `json:"prompt"`                   // User input
	History        []Message    `json:"history"`                  // Conversation turns
	SelectedFolder string       `json:"selectedFolder,omitempty"` // Target root/folder context
	DryRunDefault  bool         `json:"dryRunDefault"`            // Deprecated: a Proposta is always pending
}

// ActionProposal is the Proposta as the interface sees it: always pending until the
// user approves it explicitly.
type ActionProposal struct {
	ProposalID  string   `json:"proposalId"`  // Unique ID for approval execution
	ActionType  string   `json:"actionType"`  // "RECYCLE", "MOVE", "TAG", "MARK_REVIEW"
	Description string   `json:"description"` // Human-readable summary of what will happen
	Files       []string `json:"files"`       // List of targeted absolute filepaths
	FileCount   int      `json:"fileCount"`
	TotalBytes  int64    `json:"totalBytes"`
	TotalSize   string   `json:"totalSize"`
	Destination string   `json:"destination,omitempty"` // Target folder for MOVE
	Category    string   `json:"category,omitempty"`    // Tag or classification applied
	DryRun      bool     `json:"dryRun"`                // Always true while pending
	Executed    bool     `json:"executed"`
	CreatedAt   string   `json:"createdAt"`
	ExpiresAt   string   `json:"expiresAt,omitempty"`
}

// StreamEvent is sent over Server-Sent Events (SSE) or WebSocket to the UI during chat.
type StreamEvent struct {
	Type     string          `json:"type"` // "token", "thought", "tool_start", "tool_end", "proposal", "error", "done"
	Content  string          `json:"content,omitempty"`
	ToolName string          `json:"toolName,omitempty"`
	ToolArgs string          `json:"toolArgs,omitempty"`
	Proposal *ActionProposal `json:"proposal,omitempty"`
}

// ActionExecuteRequest is sent when the user approves a Proposta in the interface.
// Confirm must be true: without it the server answers 400 and nothing is executed.
type ActionExecuteRequest struct {
	ProposalID string `json:"proposalId"`
	Confirm    bool   `json:"confirm"`
	// Deprecated: the action and the files come from the stored Proposta, never
	// from the request body.
	ActionType string   `json:"actionType,omitempty"`
	Files      []string `json:"files,omitempty"`
}

// ActionExecuteResult contains the final execution result.
type ActionExecuteResult struct {
	Success    bool     `json:"success"`
	ActionType string   `json:"actionType"`
	Affected   int      `json:"affected"`
	FreedBytes int64    `json:"freedBytes"`
	FreedSize  string   `json:"freedSize"`
	Errors     []string `json:"errors,omitempty"`
	Message    string   `json:"message"`
}
