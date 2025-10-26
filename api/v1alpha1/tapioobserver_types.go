package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,categories={tapio,observability},shortName=to
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=`.status.daemonSetStatus.ready`
// +kubebuilder:printcolumn:name="Desired",type=integer,JSONPath=`.status.daemonSetStatus.desired`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// TapioObserver is the Schema for the tapioobservers API
type TapioObserver struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TapioObserverSpec   `json:"spec,omitempty"`
	Status TapioObserverStatus `json:"status,omitempty"`
}

// TapioObserverSpec defines the desired state of TapioObserver
type TapioObserverSpec struct {
	// NetworkObserver configuration
	// +optional
	NetworkObserver *NetworkObserverConfig `json:"networkObserver,omitempty"`

	// SchedulerObserver configuration
	// +optional
	SchedulerObserver *SchedulerObserverConfig `json:"schedulerObserver,omitempty"`

	// Container image for Tapio observers
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^[a-z0-9.\-/]+:[a-z0-9.\-]+$`
	Image string `json:"image"`

	// Image pull policy
	// +kubebuilder:validation:Enum=Always;Never;IfNotPresent
	// +kubebuilder:default=IfNotPresent
	// +optional
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// Resource requirements for observer pods
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// OTLP endpoint for exporting observability data
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	OTLPEndpoint string `json:"otlpEndpoint"`

	// Use insecure connection to OTLP endpoint
	// +kubebuilder:default=false
	// +optional
	OTLPInsecure bool `json:"otlpInsecure,omitempty"`
}

// NetworkObserverConfig defines configuration for the network observer
type NetworkObserverConfig struct {
	// Enable network observer
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`

	// Interface name pattern to observe (e.g., "eth0,ens*")
	// +kubebuilder:validation:Pattern="^[a-zA-Z0-9*,\\-_]+$"
	// +kubebuilder:default="eth0"
	// +optional
	InterfaceFilter string `json:"interfaceFilter,omitempty"`

	// Buffer size for packet capture in bytes
	// +kubebuilder:validation:Minimum=1024
	// +kubebuilder:validation:Maximum=65536
	// +kubebuilder:default=4096
	// +optional
	BufferSize int32 `json:"bufferSize,omitempty"`

	// eBPF map size (number of flow entries to track)
	// +kubebuilder:validation:Minimum=1024
	// +kubebuilder:validation:Maximum=1048576
	// +kubebuilder:default=10000
	// +optional
	MapSize int32 `json:"mapSize,omitempty"`
}

// SchedulerObserverConfig defines configuration for the scheduler observer
type SchedulerObserverConfig struct {
	// Enable scheduler observer
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`

	// Watch Kubernetes Events API for scheduling failures
	// +kubebuilder:default=true
	// +optional
	EventsAPI bool `json:"eventsAPI,omitempty"`
}

// TapioObserverStatus defines the observed state of TapioObserver
type TapioObserverStatus struct {
	// Phase represents the current deployment phase
	// +kubebuilder:validation:Enum=Pending;Running;Failed
	// +optional
	Phase string `json:"phase,omitempty"`

	// ObservedGeneration reflects the generation most recently observed
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// DaemonSet status
	// +optional
	DaemonSetStatus *DaemonSetStatus `json:"daemonSetStatus,omitempty"`
}

// DaemonSetStatus contains DaemonSet rollout status
type DaemonSetStatus struct {
	// Number of ready pods
	Ready int32 `json:"ready"`

	// Number of desired pods
	Desired int32 `json:"desired"`

	// Number of updated pods
	Updated int32 `json:"updated"`
}

// +kubebuilder:object:root=true

// TapioObserverList contains a list of TapioObserver
type TapioObserverList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TapioObserver `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TapioObserver{}, &TapioObserverList{})
}
