package domain

import (
	"context"
	"time"
)

// K8sClient provides an abstraction for Kubernetes operations
// This interface allows the intelligence layer to work with K8s
// without directly depending on k8s.io packages
type K8sClient interface {
	// GetPod retrieves a pod by namespace and name
	GetPod(ctx context.Context, namespace, name string) (*K8sPod, error)

	// GetService retrieves a service by namespace and name
	GetService(ctx context.Context, namespace, name string) (*K8sService, error)

	// GetDeployment retrieves a deployment by namespace and name
	GetDeployment(ctx context.Context, namespace, name string) (*K8sDeployment, error)

	// GetReplicaSet retrieves a replicaset by namespace and name
	GetReplicaSet(ctx context.Context, namespace, name string) (*K8sReplicaSet, error)

	// GetStatefulSet retrieves a statefulset by namespace and name
	GetStatefulSet(ctx context.Context, namespace, name string) (*K8sStatefulSet, error)

	// GetDaemonSet retrieves a daemonset by namespace and name
	GetDaemonSet(ctx context.Context, namespace, name string) (*K8sDaemonSet, error)

	// ListPods lists pods in a namespace with optional label selector
	ListPods(ctx context.Context, namespace string, selector map[string]string) ([]*K8sPod, error)

	// ListServices lists services in a namespace with optional label selector
	ListServices(ctx context.Context, namespace string, selector map[string]string) ([]*K8sService, error)

	// WatchPods watches for pod changes
	WatchPods(ctx context.Context, namespace string) (<-chan K8sWatchEvent, error)

	// WatchServices watches for service changes
	WatchServices(ctx context.Context, namespace string) (<-chan K8sWatchEvent, error)

	// GetOwnerReferences gets owner references for an object
	GetOwnerReferences(ctx context.Context, kind, namespace, name string) ([]K8sOwnerReference, error)

	// GetEvents gets events for an object
	GetEvents(ctx context.Context, involvedObjectKind, namespace, name string) ([]*K8sEvent, error)
}

// K8sWatchEvent represents a watch event
type K8sWatchEvent struct {
	Type   K8sWatchEventType
	Object K8sObject
}

// K8sWatchEventType represents the type of watch event
type K8sWatchEventType string

const (
	K8sWatchAdded    K8sWatchEventType = "ADDED"
	K8sWatchModified K8sWatchEventType = "MODIFIED"
	K8sWatchDeleted  K8sWatchEventType = "DELETED"
	K8sWatchError    K8sWatchEventType = "ERROR"
)

// K8sObject is the base interface for all K8s objects
type K8sObject interface {
	GetName() string
	GetNamespace() string
	GetUID() string
	GetResourceVersion() string
	GetLabels() map[string]string
	GetAnnotations() map[string]string
	GetCreationTimestamp() time.Time
	GetOwnerReferences() []K8sOwnerReference
}

// K8sPod represents a Kubernetes pod
type K8sPod struct {
	Name              string
	Namespace         string
	UID               string
	ResourceVersion   string
	Labels            map[string]string
	Annotations       map[string]string
	CreationTimestamp time.Time
	OwnerReferences   []K8sOwnerReference

	// Pod-specific fields
	Phase          K8sPodPhase
	Conditions     []K8sPodCondition
	NodeName       string
	HostIP         string
	PodIP          string
	StartTime      *time.Time
	Containers     []K8sContainer
	InitContainers []K8sContainer
	RestartCount   int32
	Ready          bool
}

// Implement K8sObject interface for K8sPod
func (p *K8sPod) GetName() string                         { return p.Name }
func (p *K8sPod) GetNamespace() string                    { return p.Namespace }
func (p *K8sPod) GetUID() string                          { return p.UID }
func (p *K8sPod) GetResourceVersion() string              { return p.ResourceVersion }
func (p *K8sPod) GetLabels() map[string]string            { return p.Labels }
func (p *K8sPod) GetAnnotations() map[string]string       { return p.Annotations }
func (p *K8sPod) GetCreationTimestamp() time.Time         { return p.CreationTimestamp }
func (p *K8sPod) GetOwnerReferences() []K8sOwnerReference { return p.OwnerReferences }

// K8sPodPhase represents the phase of a pod
type K8sPodPhase string

const (
	PodPending   K8sPodPhase = "Pending"
	PodRunning   K8sPodPhase = "Running"
	PodSucceeded K8sPodPhase = "Succeeded"
	PodFailed    K8sPodPhase = "Failed"
	PodUnknown   K8sPodPhase = "Unknown"
)

// K8sPodCondition represents a pod condition
type K8sPodCondition struct {
	Type               K8sPodConditionType
	Status             bool
	LastProbeTime      time.Time
	LastTransitionTime time.Time
	Reason             string
	Message            string
}

// K8sPodConditionType represents the type of pod condition
type K8sPodConditionType string

const (
	PodScheduled        K8sPodConditionType = "PodScheduled"
	PodReady            K8sPodConditionType = "Ready"
	PodInitialized      K8sPodConditionType = "Initialized"
	PodContainersReady  K8sPodConditionType = "ContainersReady"
	PodDisruptionTarget K8sPodConditionType = "DisruptionTarget"
)

// K8sContainer represents a container in a pod
type K8sContainer struct {
	Name         string
	Image        string
	Ready        bool
	RestartCount int32
	State        K8sContainerState
	LastState    K8sContainerState
}

// K8sContainerState represents the state of a container
type K8sContainerState struct {
	Running    *K8sContainerStateRunning
	Waiting    *K8sContainerStateWaiting
	Terminated *K8sContainerStateTerminated
}

// K8sContainerStateRunning represents a running container
type K8sContainerStateRunning struct {
	StartedAt time.Time
}

// K8sContainerStateWaiting represents a waiting container
type K8sContainerStateWaiting struct {
	Reason  string
	Message string
}

// K8sContainerStateTerminated represents a terminated container
type K8sContainerStateTerminated struct {
	ExitCode    int32
	Signal      int32
	Reason      string
	Message     string
	StartedAt   time.Time
	FinishedAt  time.Time
	ContainerID string
}

// K8sService represents a Kubernetes service
type K8sService struct {
	Name              string
	Namespace         string
	UID               string
	ResourceVersion   string
	Labels            map[string]string
	Annotations       map[string]string
	CreationTimestamp time.Time
	OwnerReferences   []K8sOwnerReference

	// Service-specific fields
	Type            K8sServiceType
	ClusterIP       string
	ExternalIPs     []string
	Ports           []K8sServicePort
	Selector        map[string]string
	SessionAffinity K8sSessionAffinity
}

// Implement K8sObject interface for K8sService
func (s *K8sService) GetName() string                         { return s.Name }
func (s *K8sService) GetNamespace() string                    { return s.Namespace }
func (s *K8sService) GetUID() string                          { return s.UID }
func (s *K8sService) GetResourceVersion() string              { return s.ResourceVersion }
func (s *K8sService) GetLabels() map[string]string            { return s.Labels }
func (s *K8sService) GetAnnotations() map[string]string       { return s.Annotations }
func (s *K8sService) GetCreationTimestamp() time.Time         { return s.CreationTimestamp }
func (s *K8sService) GetOwnerReferences() []K8sOwnerReference { return s.OwnerReferences }

// K8sServiceType represents the type of service
type K8sServiceType string

const (
	ServiceTypeClusterIP    K8sServiceType = "ClusterIP"
	ServiceTypeNodePort     K8sServiceType = "NodePort"
	ServiceTypeLoadBalancer K8sServiceType = "LoadBalancer"
	ServiceTypeExternalName K8sServiceType = "ExternalName"
)

// K8sServicePort represents a service port
type K8sServicePort struct {
	Name       string
	Protocol   string
	Port       int32
	TargetPort int32
	NodePort   int32
}

// K8sSessionAffinity represents session affinity
type K8sSessionAffinity string

const (
	SessionAffinityNone     K8sSessionAffinity = "None"
	SessionAffinityClientIP K8sSessionAffinity = "ClientIP"
)

// K8sDeployment represents a Kubernetes deployment
type K8sDeployment struct {
	Name              string
	Namespace         string
	UID               string
	ResourceVersion   string
	Labels            map[string]string
	Annotations       map[string]string
	CreationTimestamp time.Time
	OwnerReferences   []K8sOwnerReference

	// Deployment-specific fields
	Replicas          int32
	UpdatedReplicas   int32
	ReadyReplicas     int32
	AvailableReplicas int32
	Conditions        []K8sDeploymentCondition
	Selector          map[string]string
}

// K8sDeploymentCondition represents a deployment condition
type K8sDeploymentCondition struct {
	Type               K8sDeploymentConditionType
	Status             bool
	LastUpdateTime     time.Time
	LastTransitionTime time.Time
	Reason             string
	Message            string
}

// K8sDeploymentConditionType represents the type of deployment condition
type K8sDeploymentConditionType string

const (
	DeploymentAvailable   K8sDeploymentConditionType = "Available"
	DeploymentProgressing K8sDeploymentConditionType = "Progressing"
	DeploymentFailure     K8sDeploymentConditionType = "ReplicaFailure"
)

// K8sReplicaSet represents a Kubernetes replicaset
type K8sReplicaSet struct {
	Name              string
	Namespace         string
	UID               string
	ResourceVersion   string
	Labels            map[string]string
	Annotations       map[string]string
	CreationTimestamp time.Time
	OwnerReferences   []K8sOwnerReference

	// ReplicaSet-specific fields
	Replicas             int32
	FullyLabeledReplicas int32
	ReadyReplicas        int32
	AvailableReplicas    int32
	Selector             map[string]string
}

// K8sStatefulSet represents a Kubernetes statefulset
type K8sStatefulSet struct {
	Name              string
	Namespace         string
	UID               string
	ResourceVersion   string
	Labels            map[string]string
	Annotations       map[string]string
	CreationTimestamp time.Time
	OwnerReferences   []K8sOwnerReference

	// StatefulSet-specific fields
	Replicas        int32
	ReadyReplicas   int32
	CurrentReplicas int32
	UpdatedReplicas int32
	CurrentRevision string
	UpdateRevision  string
	Selector        map[string]string
}

// K8sDaemonSet represents a Kubernetes daemonset
type K8sDaemonSet struct {
	Name              string
	Namespace         string
	UID               string
	ResourceVersion   string
	Labels            map[string]string
	Annotations       map[string]string
	CreationTimestamp time.Time
	OwnerReferences   []K8sOwnerReference

	// DaemonSet-specific fields
	DesiredNumberScheduled int32
	CurrentNumberScheduled int32
	NumberMisscheduled     int32
	NumberReady            int32
	UpdatedNumberScheduled int32
	NumberAvailable        int32
	Selector               map[string]string
}

// K8sEvent represents a Kubernetes event
type K8sEvent struct {
	Name              string
	Namespace         string
	UID               string
	ResourceVersion   string
	CreationTimestamp time.Time

	// Event-specific fields
	InvolvedObject      K8sObjectReference
	Reason              string
	Message             string
	Source              K8sEventSource
	FirstTimestamp      time.Time
	LastTimestamp       time.Time
	Count               int32
	Type                K8sEventType
	EventTime           time.Time
	Action              string
	ReportingController string
	ReportingInstance   string
}

// K8sEventType represents the type of event
type K8sEventType string

const (
	EventTypeNormal  K8sEventType = "Normal"
	EventTypeWarning K8sEventType = "Warning"
	EventTypeError   K8sEventType = "Error"
)

// K8sEventSource represents the source of an event
type K8sEventSource struct {
	Component string
	Host      string
}

// K8sOwnerReference represents an owner reference
type K8sOwnerReference struct {
	APIVersion         string
	Kind               string
	Name               string
	UID                string
	Controller         *bool
	BlockOwnerDeletion *bool
}

// K8sObjectReference references another K8s object
type K8sObjectReference struct {
	Kind            string
	Namespace       string
	Name            string
	UID             string
	APIVersion      string
	ResourceVersion string
}
