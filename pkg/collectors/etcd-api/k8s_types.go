package etcdapi

// K8sObject represents a typed Kubernetes object from etcd
// This replaces map[string]interface{} for K8s resource parsing
type K8sObject struct {
	APIVersion string      `json:"apiVersion,omitempty"`
	Kind       string      `json:"kind,omitempty"`
	Metadata   K8sMetadata `json:"metadata"`
	Spec       K8sSpec     `json:"spec,omitempty"`
	Status     K8sStatus   `json:"status,omitempty"`
}

// K8sMetadata represents Kubernetes object metadata
type K8sMetadata struct {
	Name              string              `json:"name"`
	Namespace         string              `json:"namespace,omitempty"`
	UID               string              `json:"uid,omitempty"`
	ResourceVersion   string              `json:"resourceVersion,omitempty"`
	Generation        int64               `json:"generation,omitempty"`
	CreationTimestamp string              `json:"creationTimestamp,omitempty"`
	Labels            map[string]string   `json:"labels,omitempty"`
	Annotations       map[string]string   `json:"annotations,omitempty"`
	OwnerReferences   []K8sOwnerReference `json:"ownerReferences,omitempty"`
}

// K8sOwnerReference represents an owner reference in Kubernetes
type K8sOwnerReference struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	UID        string `json:"uid"`
	Controller *bool  `json:"controller,omitempty"`
}

// K8sSpec represents the spec section of a Kubernetes object
type K8sSpec struct {
	// Common fields
	Replicas *int32       `json:"replicas,omitempty"`
	NodeName string       `json:"nodeName,omitempty"`
	Selector *K8sSelector `json:"selector,omitempty"`

	// Pod-specific
	Containers []K8sContainer `json:"containers,omitempty"`

	// Service-specific
	Type      string    `json:"type,omitempty"`
	ClusterIP string    `json:"clusterIP,omitempty"`
	Ports     []K8sPort `json:"ports,omitempty"`

	// ConfigMap/Secret data
	Data map[string]string `json:"data,omitempty"`
}

// K8sSelector represents a label selector
type K8sSelector struct {
	MatchLabels map[string]string `json:"matchLabels,omitempty"`
}

// K8sContainer represents a container specification
type K8sContainer struct {
	Name    string   `json:"name"`
	Image   string   `json:"image"`
	Command []string `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
}

// K8sPort represents a port specification
type K8sPort struct {
	Name       string `json:"name,omitempty"`
	Port       int32  `json:"port"`
	TargetPort int32  `json:"targetPort,omitempty"`
	Protocol   string `json:"protocol,omitempty"`
}

// K8sStatus represents the status section of a Kubernetes object
type K8sStatus struct {
	Phase              string         `json:"phase,omitempty"`
	Conditions         []K8sCondition `json:"conditions,omitempty"`
	ReadyReplicas      int32          `json:"readyReplicas,omitempty"`
	AvailableReplicas  int32          `json:"availableReplicas,omitempty"`
	ObservedGeneration int64          `json:"observedGeneration,omitempty"`

	// Pod-specific
	PodIP     string `json:"podIP,omitempty"`
	HostIP    string `json:"hostIP,omitempty"`
	StartTime string `json:"startTime,omitempty"`
}

// K8sCondition represents a condition in status
type K8sCondition struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	LastTransitionTime string `json:"lastTransitionTime,omitempty"`
	Reason             string `json:"reason,omitempty"`
	Message            string `json:"message,omitempty"`
}
