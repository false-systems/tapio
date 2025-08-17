package domain

// PredictionContext provides an interface for prediction context data
// This avoids the need for weakly-typed maps while maintaining type safety
type PredictionContext interface {
	// GetEventType returns the event type
	GetEventType() string
	// GetSource returns the event source
	GetSource() string
	// GetEventID returns the event ID
	GetEventID() string
	// ToMap converts the context to a map for serialization compatibility
	ToMap() map[string]string
}

// SimplePredictionContext provides a basic implementation
type SimplePredictionContext struct {
	EventType string            `json:"event_type"`
	Source    string            `json:"source"`
	EventID   string            `json:"event_id,omitempty"`
	Data      map[string]string `json:"data,omitempty"`
}

// GetEventType returns the event type
func (c *SimplePredictionContext) GetEventType() string {
	return c.EventType
}

// GetSource returns the event source
func (c *SimplePredictionContext) GetSource() string {
	return c.Source
}

// GetEventID returns the event ID
func (c *SimplePredictionContext) GetEventID() string {
	return c.EventID
}

// ToMap converts the context to a map
func (c *SimplePredictionContext) ToMap() map[string]string {
	result := map[string]string{
		"event_type": c.EventType,
		"source":     c.Source,
	}
	if c.EventID != "" {
		result["event_id"] = c.EventID
	}
	for k, v := range c.Data {
		result[k] = v
	}
	return result
}

// NewSimplePredictionContext creates a new SimplePredictionContext
func NewSimplePredictionContext(eventType, source, eventID string) *SimplePredictionContext {
	return &SimplePredictionContext{
		EventType: eventType,
		Source:    source,
		EventID:   eventID,
		Data:      make(map[string]string),
	}
}

// AddData adds a key-value pair to the context data
func (c *SimplePredictionContext) AddData(key, value string) {
	if c.Data == nil {
		c.Data = make(map[string]string)
	}
	c.Data[key] = value
}
