package correlation
// CausalLink represents a causal relationship between events
type CausalLink struct {
	SourceEventID string  `json:"source_event_id"`
	TargetEventID string  `json:"target_event_id"`
	Strength      float64 `json:"strength"`
	Direction     string  `json:"direction"`
	Type          string  `json:"type"`
}