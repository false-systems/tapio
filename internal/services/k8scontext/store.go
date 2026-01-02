package k8scontext

import "sync"

// Store is a multi-index in-memory store for K8s metadata.
// All lookups are O(1). Uses RWMutex for simplicity (KISS).
type Store struct {
	mu sync.RWMutex

	// Primary stores (by UID)
	pods     map[string]*PodMeta
	services map[string]*ServiceMeta

	// Secondary indexes (point to UID)
	podByIP   map[string]string // "10.0.1.5" -> UID
	podByCID  map[string]string // "abc123" -> UID
	podByName map[string]string // "default/nginx" -> UID
	svcByIP   map[string]string // ClusterIP -> UID
	svcByName map[string]string // "default/nginx-svc" -> UID
}

// NewStore creates an empty store.
func NewStore() *Store {
	return &Store{
		pods:      make(map[string]*PodMeta),
		services:  make(map[string]*ServiceMeta),
		podByIP:   make(map[string]string),
		podByCID:  make(map[string]string),
		podByName: make(map[string]string),
		svcByIP:   make(map[string]string),
		svcByName: make(map[string]string),
	}
}

// AddPod adds or updates a pod in the store.
func (s *Store) AddPod(pod *PodMeta) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove old indexes if updating
	if old, ok := s.pods[pod.UID]; ok {
		s.removePodIndexes(old)
	}

	// Add to primary store
	s.pods[pod.UID] = pod

	// Add secondary indexes
	if pod.PodIP != "" {
		s.podByIP[pod.PodIP] = pod.UID
	}
	for _, c := range pod.Containers {
		if c.ContainerID != "" {
			s.podByCID[c.ContainerID] = pod.UID
		}
	}
	s.podByName[pod.NamespacedName()] = pod.UID
}

// DeletePod removes a pod from the store.
func (s *Store) DeletePod(pod *PodMeta) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.removePodIndexes(pod)
	delete(s.pods, pod.UID)
}

func (s *Store) removePodIndexes(pod *PodMeta) {
	delete(s.podByIP, pod.PodIP)
	for _, c := range pod.Containers {
		delete(s.podByCID, c.ContainerID)
	}
	delete(s.podByName, pod.NamespacedName())
}

// PodByIP looks up a pod by its IP address.
func (s *Store) PodByIP(ip string) (*PodMeta, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	uid, ok := s.podByIP[ip]
	if !ok {
		return nil, false
	}
	return s.pods[uid], true
}

// PodByContainerID looks up a pod by container ID.
func (s *Store) PodByContainerID(cid string) (*PodMeta, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	uid, ok := s.podByCID[cid]
	if !ok {
		return nil, false
	}
	return s.pods[uid], true
}

// PodByName looks up a pod by namespace and name.
func (s *Store) PodByName(namespace, name string) (*PodMeta, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := namespace + "/" + name
	uid, ok := s.podByName[key]
	if !ok {
		return nil, false
	}
	return s.pods[uid], true
}

// AddService adds or updates a service in the store.
func (s *Store) AddService(svc *ServiceMeta) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove old indexes if updating
	if old, ok := s.services[svc.UID]; ok {
		s.removeServiceIndexes(old)
	}

	s.services[svc.UID] = svc

	if svc.ClusterIP != "" && svc.ClusterIP != "None" {
		s.svcByIP[svc.ClusterIP] = svc.UID
	}
	s.svcByName[svc.NamespacedName()] = svc.UID
}

// DeleteService removes a service from the store.
func (s *Store) DeleteService(svc *ServiceMeta) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.removeServiceIndexes(svc)
	delete(s.services, svc.UID)
}

func (s *Store) removeServiceIndexes(svc *ServiceMeta) {
	delete(s.svcByIP, svc.ClusterIP)
	delete(s.svcByName, svc.NamespacedName())
}

// ServiceByClusterIP looks up a service by its ClusterIP.
func (s *Store) ServiceByClusterIP(ip string) (*ServiceMeta, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	uid, ok := s.svcByIP[ip]
	if !ok {
		return nil, false
	}
	return s.services[uid], true
}

// ServiceByName looks up a service by namespace and name.
func (s *Store) ServiceByName(namespace, name string) (*ServiceMeta, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := namespace + "/" + name
	uid, ok := s.svcByName[key]
	if !ok {
		return nil, false
	}
	return s.services[uid], true
}

// PodCount returns the number of pods in the store.
func (s *Store) PodCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.pods)
}

// ServiceCount returns the number of services in the store.
func (s *Store) ServiceCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.services)
}
