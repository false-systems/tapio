package types

// Evidence represents evidence for a finding or correlation
type Evidence struct {
	EventID     string                 `json:"event_id"`
	Type        string                 `json:"type"`
	Description string                 `json:"description"`
	Timestamp   string                 `json:"timestamp"`
	Source      string                 `json:"source"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}