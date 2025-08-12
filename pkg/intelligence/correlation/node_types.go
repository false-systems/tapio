package correlation

import (
	"fmt"
	"time"
)

// NodeType represents the type of a graph node
type NodeType string

const (
	NodeTypePod         NodeType = "Pod"
	NodeTypeService     NodeType = "Service"
	NodeTypeDeployment  NodeType = "Deployment"
	NodeTypeReplicaSet  NodeType = "ReplicaSet"
	NodeTypeStatefulSet NodeType = "StatefulSet"
	NodeTypeDaemonSet   NodeType = "DaemonSet"
	NodeTypeConfigMap   NodeType = "ConfigMap"
	NodeTypeSecret      NodeType = "Secret"
	NodeTypePVC         NodeType = "PersistentVolumeClaim"
	NodeTypePV          NodeType = "PersistentVolume"
	NodeTypeNode        NodeType = "Node"
	NodeTypeNamespace   NodeType = "Namespace"
	NodeTypeEvent       NodeType = "Event"
	NodeTypeContainer   NodeType = "Container"
	NodeTypeUnknown     NodeType = "Unknown"
)

// GraphNode represents a node in the graph database
type GraphNode struct {
	ID         int64          `json:"id"`
	UID        string         `json:"uid"`
	Type       NodeType       `json:"type"`
	Labels     []string       `json:"labels"`
	Properties NodeProperties `json:"properties"`
}

// NodeProperties represents the properties of a graph node
type NodeProperties struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Cluster   string            `json:"cluster"`
	Ready     bool              `json:"ready"`
	Phase     string            `json:"phase"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
	Metadata  map[string]string `json:"metadata"`
}

// ServiceNode represents a Kubernetes Service in the graph
type ServiceNode struct {
	GraphNode
	Selector    map[string]string `json:"selector"`
	ClusterIP   string            `json:"cluster_ip"`
	Ports       []ServicePort     `json:"ports"`
	Type        string            `json:"service_type"`
	ExternalIPs []string          `json:"external_ips"`
}

// ServicePort represents a port configuration for a service
type ServicePort struct {
	Name       string `json:"name"`
	Port       int32  `json:"port"`
	TargetPort int32  `json:"target_port"`
	Protocol   string `json:"protocol"`
}

// PodNode represents a Kubernetes Pod in the graph
type PodNode struct {
	GraphNode
	NodeName     string            `json:"node_name"`
	HostIP       string            `json:"host_ip"`
	PodIP        string            `json:"pod_ip"`
	Containers   []ContainerInfo   `json:"containers"`
	Volumes      []VolumeInfo      `json:"volumes"`
	Conditions   []PodCondition    `json:"conditions"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	RestartCount int32             `json:"restart_count"`
}

// ContainerInfo represents container information in a pod
type ContainerInfo struct {
	Name         string   `json:"name"`
	Image        string   `json:"image"`
	Ready        bool     `json:"ready"`
	RestartCount int32    `json:"restart_count"`
	State        string   `json:"state"`
	Ports        []int32  `json:"ports"`
	Command      []string `json:"command"`
	Args         []string `json:"args"`
}

// VolumeInfo represents volume information in a pod
type VolumeInfo struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Source    string `json:"source"`
	MountPath string `json:"mount_path"`
	ReadOnly  bool   `json:"read_only"`
}

// PodCondition represents a condition of a pod
type PodCondition struct {
	Type               string    `json:"type"`
	Status             string    `json:"status"`
	Reason             string    `json:"reason"`
	Message            string    `json:"message"`
	LastTransitionTime time.Time `json:"last_transition_time"`
}

// DeploymentNode represents a Kubernetes Deployment in the graph
type DeploymentNode struct {
	GraphNode
	Replicas          int32             `json:"replicas"`
	ReadyReplicas     int32             `json:"ready_replicas"`
	UpdatedReplicas   int32             `json:"updated_replicas"`
	AvailableReplicas int32             `json:"available_replicas"`
	Strategy          string            `json:"strategy"`
	Labels            map[string]string `json:"labels"`
	Selector          map[string]string `json:"selector"`
}

// ConfigMapNode represents a Kubernetes ConfigMap in the graph
type ConfigMapNode struct {
	GraphNode
	Data       map[string]string `json:"data"`
	BinaryData map[string][]byte `json:"binary_data"`
}

// SecretNode represents a Kubernetes Secret in the graph
type SecretNode struct {
	GraphNode
	Type string            `json:"secret_type"`
	Data map[string][]byte `json:"data"`
}

// PVCNode represents a Kubernetes PersistentVolumeClaim in the graph
type PVCNode struct {
	GraphNode
	StorageClass string   `json:"storage_class"`
	AccessModes  []string `json:"access_modes"`
	Capacity     string   `json:"capacity"`
	Phase        string   `json:"phase"`
	VolumeName   string   `json:"volume_name"`
}

// EventNode represents a Kubernetes Event in the graph
type EventNode struct {
	GraphNode
	Reason         string          `json:"reason"`
	Message        string          `json:"message"`
	Source         string          `json:"source"`
	FirstTimestamp time.Time       `json:"first_timestamp"`
	LastTimestamp  time.Time       `json:"last_timestamp"`
	Count          int32           `json:"count"`
	Type           string          `json:"event_type"`
	InvolvedObject ObjectReference `json:"involved_object"`
}

// ObjectReference represents a reference to another Kubernetes object
type ObjectReference struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	UID       string `json:"uid"`
	Namespace string `json:"namespace"`
}

// RelationshipType represents the type of relationship between nodes
type RelationshipType string

const (
	RelTypeOwns        RelationshipType = "OWNS"
	RelTypeSelects     RelationshipType = "SELECTS"
	RelTypeMounts      RelationshipType = "MOUNTS"
	RelTypeUsesSecret  RelationshipType = "USES_SECRET"
	RelTypeClaims      RelationshipType = "CLAIMS"
	RelTypeExposes     RelationshipType = "EXPOSES"
	RelTypeConnectsTo  RelationshipType = "CONNECTS_TO"
	RelTypeScheduledOn RelationshipType = "SCHEDULED_ON"
	RelTypeManages     RelationshipType = "MANAGES"
	RelTypeReferences  RelationshipType = "REFERENCES"
	RelTypeTriggered   RelationshipType = "TRIGGERED"
	RelTypeImpacted    RelationshipType = "IMPACTED"
)

// RelationshipProperties represents the properties of a graph relationship
type RelationshipProperties struct {
	Type     string            `json:"type"`
	Weight   float64           `json:"weight,omitempty"`
	Metadata map[string]string `json:"metadata"`
}

// GraphRelationship represents a relationship between two nodes
type GraphRelationship struct {
	ID         int64                  `json:"id"`
	Type       RelationshipType       `json:"type"`
	StartNode  int64                  `json:"start_node"`
	EndNode    int64                  `json:"end_node"`
	Properties RelationshipProperties `json:"properties"`
	CreatedAt  time.Time              `json:"created_at"`
}

// GraphPath represents a path through the graph
type GraphPath struct {
	Nodes         []GraphNode         `json:"nodes"`
	Relationships []GraphRelationship `json:"relationships"`
	Length        int                 `json:"length"`
}

// QueryResult represents a typed result from a graph query
type QueryResult struct {
	Nodes         []GraphNode            `json:"nodes,omitempty"`
	Relationships []GraphRelationship    `json:"relationships,omitempty"`
	Paths         []GraphPath            `json:"paths,omitempty"`
	Scalars       map[string]interface{} `json:"scalars,omitempty"`
}

// ParseNodeFromRecord parses a graph node from a record
func ParseNodeFromRecord(record map[string]interface{}, key string) (*GraphNode, error) {
	nodeData, ok := record[key]
	if !ok {
		return nil, fmt.Errorf("key %s not found in record", key)
	}

	nodeMap, ok := nodeData.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid node data type for key %s", key)
	}

	node := &GraphNode{}

	// Parse ID
	if id, ok := nodeMap["id"].(int64); ok {
		node.ID = id
	}

	// Parse labels
	if labels, ok := nodeMap["labels"].([]interface{}); ok {
		for _, label := range labels {
			if labelStr, ok := label.(string); ok {
				node.Labels = append(node.Labels, labelStr)
				// Set node type based on label
				if node.Type == "" {
					node.Type = NodeType(labelStr)
				}
			}
		}
	}

	// Parse properties
	if props, ok := nodeMap["properties"].(map[string]interface{}); ok {
		node.Properties = parseNodeProperties(props)

		// Extract UID if present
		if uid, ok := props["uid"].(string); ok {
			node.UID = uid
		}
	}

	return node, nil
}

// parseNodeProperties parses node properties from a map
func parseNodeProperties(props map[string]interface{}) NodeProperties {
	np := NodeProperties{
		Metadata: make(map[string]string),
	}

	if name, ok := props["name"].(string); ok {
		np.Name = name
	}
	if namespace, ok := props["namespace"].(string); ok {
		np.Namespace = namespace
	}
	if cluster, ok := props["cluster"].(string); ok {
		np.Cluster = cluster
	}
	if ready, ok := props["ready"].(bool); ok {
		np.Ready = ready
	}
	if phase, ok := props["phase"].(string); ok {
		np.Phase = phase
	}

	// Parse timestamps
	if created, ok := props["created_at"].(int64); ok {
		np.CreatedAt = time.Unix(created, 0)
	}
	if updated, ok := props["updated_at"].(int64); ok {
		np.UpdatedAt = time.Unix(updated, 0)
	}

	// Collect remaining properties as metadata
	for k, v := range props {
		switch k {
		case "name", "namespace", "cluster", "ready", "phase", "created_at", "updated_at", "uid":
			// Already processed
		default:
			if str, ok := v.(string); ok {
				np.Metadata[k] = str
			}
		}
	}

	return np
}

// parseRelationshipProperties converts map[string]interface{} to RelationshipProperties
func parseRelationshipProperties(props map[string]interface{}) RelationshipProperties {
	rp := RelationshipProperties{
		Metadata: make(map[string]string),
	}

	if typeStr, ok := props["type"].(string); ok {
		rp.Type = typeStr
	}
	if weight, ok := props["weight"].(float64); ok {
		rp.Weight = weight
	}

	// Collect remaining properties as metadata
	for k, v := range props {
		switch k {
		case "type", "weight":
			// Already processed
		default:
			if str, ok := v.(string); ok {
				rp.Metadata[k] = str
			} else {
				rp.Metadata[k] = fmt.Sprintf("%v", v)
			}
		}
	}

	return rp
}

// ParsePodFromNode converts a GraphNode to a PodNode
func ParsePodFromNode(node *GraphNode) (*PodNode, error) {
	if node.Type != NodeTypePod {
		return nil, fmt.Errorf("node is not a Pod type: %s", node.Type)
	}

	pod := &PodNode{
		GraphNode:   *node,
		Labels:      make(map[string]string),
		Annotations: make(map[string]string),
	}

	// Extract pod-specific properties from metadata
	for k, v := range node.Properties.Metadata {
		switch k {
		case "node_name":
			pod.NodeName = v
		case "host_ip":
			pod.HostIP = v
		case "pod_ip":
			pod.PodIP = v
		case "restart_count":
			// Parse restart count if needed
		default:
			// Check if it's a label or annotation
			if len(k) > 6 && k[:6] == "label_" {
				pod.Labels[k[6:]] = v
			} else if len(k) > 11 && k[:11] == "annotation_" {
				pod.Annotations[k[11:]] = v
			}
		}
	}

	return pod, nil
}

// ParseServiceFromNode converts a GraphNode to a ServiceNode
func ParseServiceFromNode(node *GraphNode) (*ServiceNode, error) {
	if node.Type != NodeTypeService {
		return nil, fmt.Errorf("node is not a Service type: %s", node.Type)
	}

	svc := &ServiceNode{
		GraphNode:   *node,
		Selector:    make(map[string]string),
		ExternalIPs: []string{},
	}

	// Extract service-specific properties from metadata
	for k, v := range node.Properties.Metadata {
		switch k {
		case "cluster_ip":
			svc.ClusterIP = v
		case "service_type":
			svc.Type = v
		default:
			// Check if it's a selector
			if len(k) > 9 && k[:9] == "selector_" {
				svc.Selector[k[9:]] = v
			}
		}
	}

	return svc, nil
}
