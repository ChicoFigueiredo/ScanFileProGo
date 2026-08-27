package ai

// ProviderType represents the type of LLM integration.
type ProviderType string

const (
	ProviderOllama      ProviderType = "ollama"
	ProviderOpenRouter  ProviderType = "openrouter"
	ProviderDirectLocal ProviderType = "direct"
)

// Message represents a single chat turn in the conversation.
type Message struct {
	Role       string     `json:"role"` // "system", "user", "assistant", "tool"
	Content    string     `json:"content"`
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
	Provider       ProviderType `json:"provider"`                 // "ollama", "openrouter", "direct"
	Model          string       `json:"model"`                    // e.g. "qwen2.5:1.5b", "claude-3.7-sonnet"
	Prompt         string       `json:"prompt"`                   // User input
	History        []Message    `json:"history"`                  // Conversation turns
	SelectedFolder string       `json:"selectedFolder,omitempty"` // Target root/folder context
	DryRunDefault  bool         `json:"dryRunDefault"`            // Default dry-run mode for proposals
}

// ActionProposal represents a structured proposal formulated by the AI for user approval.
type ActionProposal struct {
	ProposalID  string   `json:"proposalId"`  // Unique ID for approval execution
	ActionType  string   `json:"actionType"`  // "RECYCLE", "MOVE", "TAG", "DELETE", "MARK_REVIEW"
	Description string   `json:"description"` // Human-readable summary of what will happen
	Files       []string `json:"files"`       // List of targeted absolute filepaths
	FileCount   int      `json:"fileCount"`
	TotalBytes  int64    `json:"totalBytes"`
	TotalSize   string   `json:"totalSize"`
	Category    string   `json:"category,omitempty"` // Tag or classification applied
	DryRun      bool     `json:"dryRun"`
	Executed    bool     `json:"executed"`
	CreatedAt   string   `json:"createdAt"`
}

// StreamEvent is sent over Server-Sent Events (SSE) or WebSocket to the UI during chat.
type StreamEvent struct {
	Type     string          `json:"type"` // "token", "thought", "tool_start", "tool_end", "proposal", "error", "done"
	Content  string          `json:"content,omitempty"`
	ToolName string          `json:"toolName,omitempty"`
	ToolArgs string          `json:"toolArgs,omitempty"`
	Proposal *ActionProposal `json:"proposal,omitempty"`
}

// ActionExecuteRequest is sent when the user clicks to approve and execute a proposal.
type ActionExecuteRequest struct {
	ProposalID string `json:"proposalId"`
	ActionType string `json:"actionType"`
	Files      []string `json:"files"`
}

// ActionExecuteResult contains the final execution result.
type ActionExecuteResult struct {
	Success     bool     `json:"success"`
	ActionType  string   `json:"actionType"`
	Affected    int      `json:"affected"`
	FreedBytes  int64    `json:"freedBytes"`
	FreedSize   string   `json:"freedSize"`
	Errors      []string `json:"errors,omitempty"`
	Message     string   `json:"message"`
}
