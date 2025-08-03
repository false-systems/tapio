package template

// K8sMetadata contains Kubernetes object metadata
// ALL collectors must extract this information when available
type K8sMetadata struct {
	Namespace string
	Name      string
	Kind      string
	UID       string
	Labels    string // comma-separated: app=nginx,version=1.2
	OwnerRefs string // Type/Name format: ReplicaSet/nginx-7c4ff8b6d5
}

// EventData represents collector-specific event data
// Each collector defines their own event types here
type EventData struct {
	// Template example fields
	Message string                 `json:"message"`
	Value   int                    `json:"value"`
	Details map[string]interface{} `json:"details,omitempty"`
}
