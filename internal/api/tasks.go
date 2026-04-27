package api

type CreateTaskRequest struct {
	Title  string `json:"title"`
	Prompt string `json:"prompt"`
	// SystemPrompt is the per-task system prompt for agent_loop runs.
	// It's the narrowest layer in the four-level composition (global
	// → tenant → workspace CLAUDE.md/AGENTS.md → this).
	SystemPrompt       string `json:"system_prompt,omitempty"`
	ExecutionProfile   string `json:"execution_profile"`
	Repo               string `json:"repo"`
	BaseBranch         string `json:"base_branch"`
	WorkspaceMode      string `json:"workspace_mode"`
	ExecutionKind      string `json:"execution_kind"`
	ShellCommand       string `json:"shell_command"`
	GitCommand         string `json:"git_command"`
	WorkingDirectory   string `json:"working_directory"`
	FileOperation      string `json:"file_operation"`
	FilePath           string `json:"file_path"`
	FileContent        string `json:"file_content"`
	SandboxAllowedRoot string `json:"sandbox_allowed_root"`
	SandboxReadOnly    bool   `json:"sandbox_read_only"`
	SandboxNetwork     bool   `json:"sandbox_network"`
	TimeoutMS          int    `json:"timeout_ms"`
	Priority           string `json:"priority"`
	RequestedModel     string `json:"requested_model"`
	RequestedProvider  string `json:"requested_provider"`
	BudgetMicrosUSD    int64  `json:"budget_micros_usd"`
}

type TaskLifecycleRequest struct {
	ID string `json:"id"`
}

type ResolveTaskApprovalRequest struct {
	Decision string `json:"decision"`
	Note     string `json:"note"`
}

type RetryTaskRunRequest struct {
	Reason string `json:"reason"`
}

type ResumeTaskRunRequest struct {
	Reason string `json:"reason"`
}

type AppendTaskRunEventRequest struct {
	EventType string         `json:"event_type"`
	StepID    string         `json:"step_id"`
	Status    string         `json:"status"`
	Note      string         `json:"note"`
	Data      map[string]any `json:"data"`
}

type TaskResponse struct {
	Object string   `json:"object"`
	Data   TaskItem `json:"data"`
}

type TasksResponse struct {
	Object string     `json:"object"`
	Data   []TaskItem `json:"data"`
}

type TaskRunResponse struct {
	Object string      `json:"object"`
	Data   TaskRunItem `json:"data"`
}

type TaskRunStreamEventResponse struct {
	Object string                 `json:"object"`
	Data   TaskRunStreamEventData `json:"data"`
}

type TaskRunsResponse struct {
	Object string        `json:"object"`
	Data   []TaskRunItem `json:"data"`
}

type TaskStepResponse struct {
	Object string       `json:"object"`
	Data   TaskStepItem `json:"data"`
}

type TaskStepsResponse struct {
	Object string         `json:"object"`
	Data   []TaskStepItem `json:"data"`
}

type TaskApprovalResponse struct {
	Object string           `json:"object"`
	Data   TaskApprovalItem `json:"data"`
}

type TaskApprovalsResponse struct {
	Object string             `json:"object"`
	Data   []TaskApprovalItem `json:"data"`
}

type TaskArtifactResponse struct {
	Object string           `json:"object"`
	Data   TaskArtifactItem `json:"data"`
}

type TaskArtifactsResponse struct {
	Object string             `json:"object"`
	Data   []TaskArtifactItem `json:"data"`
}

type TaskRunEventsResponse struct {
	Object string             `json:"object"`
	Data   []TaskRunEventItem `json:"data"`
}

type TaskItem struct {
	ID                   string `json:"id"`
	Title                string `json:"title"`
	Prompt               string `json:"prompt"`
	SystemPrompt         string `json:"system_prompt,omitempty"`
	Tenant               string `json:"tenant,omitempty"`
	User                 string `json:"user,omitempty"`
	Repo                 string `json:"repo,omitempty"`
	BaseBranch           string `json:"base_branch,omitempty"`
	WorkspaceMode        string `json:"workspace_mode,omitempty"`
	ExecutionKind        string `json:"execution_kind,omitempty"`
	ShellCommand         string `json:"shell_command,omitempty"`
	GitCommand           string `json:"git_command,omitempty"`
	WorkingDirectory     string `json:"working_directory,omitempty"`
	FileOperation        string `json:"file_operation,omitempty"`
	FilePath             string `json:"file_path,omitempty"`
	FileContent          string `json:"file_content,omitempty"`
	SandboxAllowedRoot   string `json:"sandbox_allowed_root,omitempty"`
	SandboxReadOnly      bool   `json:"sandbox_read_only,omitempty"`
	SandboxNetwork       bool   `json:"sandbox_network,omitempty"`
	TimeoutMS            int    `json:"timeout_ms,omitempty"`
	Status               string `json:"status"`
	Priority             string `json:"priority,omitempty"`
	RequestedModel       string `json:"requested_model,omitempty"`
	RequestedProvider    string `json:"requested_provider,omitempty"`
	BudgetMicrosUSD      int64  `json:"budget_micros_usd,omitempty"`
	LatestRunID          string `json:"latest_run_id,omitempty"`
	PendingApprovalCount int    `json:"pending_approval_count,omitempty"`
	StepCount            int    `json:"step_count,omitempty"`
	ArtifactCount        int    `json:"artifact_count,omitempty"`
	LastError            string `json:"last_error,omitempty"`
	CreatedAt            string `json:"created_at,omitempty"`
	UpdatedAt            string `json:"updated_at,omitempty"`
	StartedAt            string `json:"started_at,omitempty"`
	FinishedAt           string `json:"finished_at,omitempty"`
	RootTraceID          string `json:"root_trace_id,omitempty"`
	LatestTraceID        string `json:"latest_trace_id,omitempty"`
	LatestRequestID      string `json:"latest_request_id,omitempty"`
}

type TaskRunItem struct {
	ID                 string `json:"id"`
	TaskID             string `json:"task_id"`
	Number             int    `json:"number"`
	Status             string `json:"status"`
	Orchestrator       string `json:"orchestrator,omitempty"`
	Model              string `json:"model,omitempty"`
	Provider           string `json:"provider,omitempty"`
	ProviderKind       string `json:"provider_kind,omitempty"`
	WorkspaceID        string `json:"workspace_id,omitempty"`
	WorkspacePath      string `json:"workspace_path,omitempty"`
	StepCount          int    `json:"step_count,omitempty"`
	ApprovalCount      int    `json:"approval_count,omitempty"`
	ArtifactCount      int    `json:"artifact_count,omitempty"`
	TotalCostMicrosUSD int64  `json:"total_cost_micros_usd,omitempty"`
	LastError          string `json:"last_error,omitempty"`
	StartedAt          string `json:"started_at,omitempty"`
	FinishedAt         string `json:"finished_at,omitempty"`
	RequestID          string `json:"request_id,omitempty"`
	TraceID            string `json:"trace_id,omitempty"`
	RootSpanID         string `json:"root_span_id,omitempty"`
	OtelStatusCode     string `json:"otel_status_code,omitempty"`
	OtelStatusMessage  string `json:"otel_status_message,omitempty"`
}

type TaskRunStreamEventData struct {
	Sequence  int                `json:"sequence"`
	Terminal  bool               `json:"terminal,omitempty"`
	Run       TaskRunItem        `json:"run"`
	Steps     []TaskStepItem     `json:"steps,omitempty"`
	Artifacts []TaskArtifactItem `json:"artifacts,omitempty"`
	EventType string             `json:"event_type,omitempty"`
}

type TaskRunEventItem struct {
	ID        string         `json:"id"`
	TaskID    string         `json:"task_id"`
	RunID     string         `json:"run_id"`
	Sequence  int64          `json:"sequence"`
	EventType string         `json:"event_type"`
	Data      map[string]any `json:"data,omitempty"`
	CreatedAt string         `json:"created_at,omitempty"`
	RequestID string         `json:"request_id,omitempty"`
	TraceID   string         `json:"trace_id,omitempty"`
}

type TaskStepItem struct {
	ID            string         `json:"id"`
	TaskID        string         `json:"task_id"`
	RunID         string         `json:"run_id"`
	ParentStepID  string         `json:"parent_step_id,omitempty"`
	Index         int            `json:"index"`
	Kind          string         `json:"kind"`
	Title         string         `json:"title,omitempty"`
	Status        string         `json:"status"`
	Phase         string         `json:"phase,omitempty"`
	Result        string         `json:"result,omitempty"`
	ToolName      string         `json:"tool_name,omitempty"`
	Input         map[string]any `json:"input,omitempty"`
	OutputSummary map[string]any `json:"output_summary,omitempty"`
	ExitCode      int            `json:"exit_code,omitempty"`
	Error         string         `json:"error,omitempty"`
	ErrorKind     string         `json:"error_kind,omitempty"`
	ApprovalID    string         `json:"approval_id,omitempty"`
	StartedAt     string         `json:"started_at,omitempty"`
	FinishedAt    string         `json:"finished_at,omitempty"`
	RequestID     string         `json:"request_id,omitempty"`
	TraceID       string         `json:"trace_id,omitempty"`
	SpanID        string         `json:"span_id,omitempty"`
	ParentSpanID  string         `json:"parent_span_id,omitempty"`
}

type TaskApprovalItem struct {
	ID             string `json:"id"`
	TaskID         string `json:"task_id"`
	RunID          string `json:"run_id"`
	StepID         string `json:"step_id,omitempty"`
	Kind           string `json:"kind"`
	Status         string `json:"status"`
	Reason         string `json:"reason,omitempty"`
	RequestedBy    string `json:"requested_by,omitempty"`
	ResolvedBy     string `json:"resolved_by,omitempty"`
	ResolutionNote string `json:"resolution_note,omitempty"`
	CreatedAt      string `json:"created_at,omitempty"`
	ResolvedAt     string `json:"resolved_at,omitempty"`
	RequestID      string `json:"request_id,omitempty"`
	TraceID        string `json:"trace_id,omitempty"`
	SpanID         string `json:"span_id,omitempty"`
}

type TaskArtifactItem struct {
	ID          string `json:"id"`
	TaskID      string `json:"task_id"`
	RunID       string `json:"run_id"`
	StepID      string `json:"step_id,omitempty"`
	Kind        string `json:"kind"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mime_type,omitempty"`
	StorageKind string `json:"storage_kind,omitempty"`
	Path        string `json:"path,omitempty"`
	ContentText string `json:"content_text,omitempty"`
	ObjectRef   string `json:"object_ref,omitempty"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	Status      string `json:"status,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
	RequestID   string `json:"request_id,omitempty"`
	TraceID     string `json:"trace_id,omitempty"`
	SpanID      string `json:"span_id,omitempty"`
}
