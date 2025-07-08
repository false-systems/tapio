package types

import "time"

// ExplainRequest represents a request for explanation
type ExplainRequest struct {
	Resource  *ResourceRef `json:"resource"`
	Namespace string       `json:"namespace"`
	Verbose   bool         `json:"verbose"`
}

// Explanation contains the detailed analysis and explanation
type Explanation struct {
	Resource    *ResourceRef        `json:"resource"`
	Summary     string              `json:"summary"`
	Problems    []Problem           `json:"problems"`
	Analysis    *Analysis           `json:"analysis"`
	RootCauses  []RootCause         `json:"root_causes"`
	Solutions   []Solution          `json:"solutions"`
	Prediction  *PredictionSummary  `json:"prediction,omitempty"`
	Learning    *Learning           `json:"learning,omitempty"`
	Timestamp   time.Time           `json:"timestamp"`
}

// Analysis contains the technical details
type Analysis struct {
	KubernetesView *KubernetesView `json:"kubernetes_view"`
	RealityCheck   *RealityCheck   `json:"reality_check"`
	Correlation    *Correlation    `json:"correlation"`
	KernelInsights *KernelInsights `json:"kernel_insights,omitempty"`
}

// KubernetesView shows what Kubernetes API reports
type KubernetesView struct {
	Status      string            `json:"status"`
	Phase       string            `json:"phase"`
	Conditions  []string          `json:"conditions"`
	Resources   map[string]string `json:"resources"`
	Events      []string          `json:"recent_events"`
}

// RealityCheck shows actual system state with eBPF data
type RealityCheck struct {
	ActualMemory    string        `json:"actual_memory,omitempty"`
	RestartPattern  string        `json:"restart_pattern,omitempty"`
	ErrorPatterns   []string      `json:"error_patterns,omitempty"`
	NetworkIssues   []string      `json:"network_issues,omitempty"`
	EBPFInsights    *EBPFInsights `json:"ebpf_insights,omitempty"`
}

// Correlation shows the differences and patterns
type Correlation struct {
	Discrepancies []string `json:"discrepancies"`
	Patterns      []string `json:"patterns"`
	Trends        []string `json:"trends"`
}

// RootCause represents an identified root cause
type RootCause struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Evidence    []string `json:"evidence"`
	Confidence  float64  `json:"confidence"`
}

// Solution represents a recommended fix
type Solution struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Commands    []string `json:"commands"`
	Urgency     Severity `json:"urgency"`
	Difficulty  string   `json:"difficulty"` // "easy", "medium", "hard"
	Risk        string   `json:"risk"`       // "low", "medium", "high"
}

// Learning contains educational information
type Learning struct {
	ConceptExplanation string   `json:"concept_explanation"`
	WhyItMatters       string   `json:"why_it_matters"`
	CommonMistakes     []string `json:"common_mistakes"`
	BestPractices      []string `json:"best_practices"`
}

// KernelInsights contains kernel-level insights from eBPF
type KernelInsights struct {
	MemoryPressure     string `json:"memory_pressure,omitempty"`
	HeapAnalysis       string `json:"heap_analysis,omitempty"`
	NetworkCorrelation string `json:"network_correlation,omitempty"`
	DiskIO             string `json:"disk_io,omitempty"`
	CPUOverhead        string `json:"cpu_overhead,omitempty"`
}

// EBPFInsights contains eBPF-collected data
type EBPFInsights struct {
	TotalMemory      uint64           `json:"total_memory"`
	MemoryGrowthRate float64          `json:"memory_growth_rate"`
	SyscallPattern   string           `json:"syscall_pattern,omitempty"`
	Processes        []ProcessInsight `json:"processes"`
}

// ProcessInsight contains per-process eBPF data
type ProcessInsight struct {
	PID                 uint32  `json:"pid"`
	Command             string  `json:"command"`
	MemoryUsage         uint64  `json:"memory_usage"`
	AllocationRate      float64 `json:"allocation_rate"`
	MemoryLeakSignature string  `json:"memory_leak_signature,omitempty"`
}

// PredictionSummary contains prediction data
type PredictionSummary struct {
	Type        string        `json:"type"`
	TimeToEvent time.Duration `json:"time_to_event"`
	Confidence  float64       `json:"confidence"`
	Impact      []string      `json:"impact"`
}