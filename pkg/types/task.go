package types

import "time"

type Task struct {
	ID                string
	Title             string
	Prompt            string
	Tenant            string
	User              string
	Repo              string
	BaseBranch        string
	WorkspaceMode     string
	Status            string
	Priority          string
	RequestedModel    string
	RequestedProvider string
	BudgetMicrosUSD   int64
	LatestRunID       string
	LastError         string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	StartedAt         time.Time
	FinishedAt        time.Time
	RootTraceID       string
	LatestTraceID     string
	LatestRequestID   string
}

type TaskRun struct {
	ID                 string
	TaskID             string
	Number             int
	Status             string
	Orchestrator       string
	Model              string
	Provider           string
	ProviderKind       string
	WorkspaceID        string
	WorkspacePath      string
	StepCount          int
	ApprovalCount      int
	ArtifactCount      int
	TotalCostMicrosUSD int64
	LastError          string
	StartedAt          time.Time
	FinishedAt         time.Time
	RequestID          string
	TraceID            string
	RootSpanID         string
	OtelStatusCode     string
	OtelStatusMessage  string
}

type TaskStep struct {
	ID            string
	TaskID        string
	RunID         string
	ParentStepID  string
	Index         int
	Kind          string
	Title         string
	Status        string
	Phase         string
	Result        string
	ToolName      string
	Input         map[string]any
	OutputSummary map[string]any
	ExitCode      int
	Error         string
	ErrorKind     string
	ApprovalID    string
	StartedAt     time.Time
	FinishedAt    time.Time
	RequestID     string
	TraceID       string
	SpanID        string
	ParentSpanID  string
}

type TaskApproval struct {
	ID             string
	TaskID         string
	RunID          string
	StepID         string
	Kind           string
	Status         string
	Reason         string
	RequestedBy    string
	ResolvedBy     string
	ResolutionNote string
	CreatedAt      time.Time
	ResolvedAt     time.Time
	RequestID      string
	TraceID        string
	SpanID         string
}

type TaskArtifact struct {
	ID          string
	TaskID      string
	RunID       string
	StepID      string
	Kind        string
	Name        string
	Description string
	MimeType    string
	StorageKind string
	Path        string
	ContentText string
	ObjectRef   string
	SizeBytes   int64
	SHA256      string
	Status      string
	CreatedAt   time.Time
	RequestID   string
	TraceID     string
	SpanID      string
}
