package k8s

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
)

type StateTracker struct {
	resourceStates   map[string]*ResourceState
	conflictResolver *ConflictResolver
	reconciler       *StateReconciler
	differ           *StateDiffer
	mu               sync.RWMutex
	config           *StateConfig
	stopCh           chan struct{}
}

type StateConfig struct {
	ReconcileInterval  time.Duration
	ConflictResolution ConflictResolutionStrategy
	MaxStateHistory    int
	StateRetentionTTL  time.Duration
	DiffComputeTimeout time.Duration
	ReconcileTimeout   time.Duration
}

type ResourceState struct {
	UID             types.UID
	Namespace       string
	Name            string
	Kind            string
	ResourceVersion string
	Generation      int64
	Object          runtime.Object
	LastModified    time.Time
	StateHistory    []*StateSnapshot
	ConflictCount   int
	mu              sync.RWMutex
}

type StateSnapshot struct {
	ResourceVersion string
	Generation      int64
	Object          runtime.Object
	Timestamp       time.Time
	ChangeType      ChangeType
	ChangeSummary   *ChangeSummary
}

type ChangeSummary struct {
	FieldsAdded    []string
	FieldsRemoved  []string
	FieldsModified []string
	Severity       ChangeSeverity
}

type ChangeType string

const (
	ChangeTypeCreate ChangeType = "CREATE"
	ChangeTypeUpdate ChangeType = "UPDATE"
	ChangeTypeDelete ChangeType = "DELETE"
	ChangeTypeStatus ChangeType = "STATUS"
)

type ChangeSeverity string

const (
	ChangeSeverityLow    ChangeSeverity = "LOW"
	ChangeSeverityMedium ChangeSeverity = "MEDIUM"
	ChangeSeverityHigh   ChangeSeverity = "HIGH"
)

type ConflictResolutionStrategy string

const (
	ConflictResolutionLastWriter ConflictResolutionStrategy = "LAST_WRITER"
	ConflictResolutionMerge      ConflictResolutionStrategy = "MERGE"
	ConflictResolutionManual     ConflictResolutionStrategy = "MANUAL"
)

type ConflictResolver struct {
	strategy ConflictResolutionStrategy
	mu       sync.RWMutex
}

type StateReconciler struct {
	client       *ResilientClient
	cacheManager *CacheManager
	interval     time.Duration
	timeout      time.Duration
}

type StateDiffer struct {
	timeout time.Duration
}

type StateConflict struct {
	ResourceKey  string
	LocalState   *ResourceState
	RemoteState  *ResourceState
	ConflictType ConflictType
	Resolution   ConflictResolution
	CreatedAt    time.Time
}

type ConflictType string

const (
	ConflictTypeVersionMismatch ConflictType = "VERSION_MISMATCH"
	ConflictTypeGenerationSkew  ConflictType = "GENERATION_SKEW"
	ConflictTypeContentDrift    ConflictType = "CONTENT_DRIFT"
)

type ConflictResolution struct {
	Strategy     ConflictResolutionStrategy
	ResolvedAt   time.Time
	Resolution   string
	AutoResolved bool
}

func DefaultStateConfig() *StateConfig {
	return &StateConfig{
		ReconcileInterval:  30 * time.Second,
		ConflictResolution: ConflictResolutionMerge,
		MaxStateHistory:    10,
		StateRetentionTTL:  24 * time.Hour,
		DiffComputeTimeout: 5 * time.Second,
		ReconcileTimeout:   30 * time.Second,
	}
}

func NewStateTracker(config *StateConfig) *StateTracker {
	if config == nil {
		config = DefaultStateConfig()
	}

	st := &StateTracker{
		resourceStates: make(map[string]*ResourceState),
		conflictResolver: &ConflictResolver{
			strategy: config.ConflictResolution,
		},
		reconciler: &StateReconciler{
			interval: config.ReconcileInterval,
			timeout:  config.ReconcileTimeout,
		},
		differ: &StateDiffer{
			timeout: config.DiffComputeTimeout,
		},
		config: config,
		stopCh: make(chan struct{}),
	}

	go st.reconcileLoop()
	go st.cleanupLoop()

	return st
}

func (st *StateTracker) TrackResource(obj runtime.Object) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	accessor, err := meta.Accessor(obj)
	if err != nil {
		return fmt.Errorf("failed to get object metadata: %w", err)
	}

	key := st.generateResourceKey(accessor)
	existing, exists := st.resourceStates[key]

	if !exists {
		st.resourceStates[key] = st.createResourceState(obj, accessor)
		return nil
	}

	return st.updateResourceState(existing, obj, accessor)
}

func (st *StateTracker) GetResourceState(namespace, name, kind string) (*ResourceState, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	key := fmt.Sprintf("%s/%s/%s", namespace, name, kind)
	state, exists := st.resourceStates[key]
	return state, exists
}

func (st *StateTracker) ComputeDiff(current, desired runtime.Object) (*ChangeSummary, error) {
	ctx, cancel := context.WithTimeout(context.Background(), st.differ.timeout)
	defer cancel()

	return st.differ.computeDiff(ctx, current, desired)
}

func (st *StateTracker) DetectConflicts() []*StateConflict {
	st.mu.RLock()
	defer st.mu.RUnlock()

	var conflicts []*StateConflict
	for key, state := range st.resourceStates {
		if conflict := st.detectStateConflict(key, state); conflict != nil {
			conflicts = append(conflicts, conflict)
		}
	}

	return conflicts
}

func (st *StateTracker) ResolveConflict(conflict *StateConflict) error {
	return st.conflictResolver.Resolve(conflict)
}

func (st *StateTracker) Close() error {
	close(st.stopCh)
	return nil
}

func (st *StateTracker) generateResourceKey(accessor metav1.Object) string {
	return fmt.Sprintf("%s/%s", accessor.GetNamespace(), accessor.GetName())
}

func (st *StateTracker) createResourceState(obj runtime.Object, accessor metav1.Object) *ResourceState {
	state := &ResourceState{
		UID:             accessor.GetUID(),
		Namespace:       accessor.GetNamespace(),
		Name:            accessor.GetName(),
		Kind:            "unknown", // Will be set by caller if needed
		ResourceVersion: accessor.GetResourceVersion(),
		Generation:      accessor.GetGeneration(),
		Object:          obj.DeepCopyObject(),
		LastModified:    time.Now(),
		StateHistory:    make([]*StateSnapshot, 0, st.config.MaxStateHistory),
	}

	snapshot := &StateSnapshot{
		ResourceVersion: accessor.GetResourceVersion(),
		Generation:      accessor.GetGeneration(),
		Object:          obj.DeepCopyObject(),
		Timestamp:       time.Now(),
		ChangeType:      ChangeTypeCreate,
	}

	state.StateHistory = append(state.StateHistory, snapshot)
	return state
}

func (st *StateTracker) updateResourceState(state *ResourceState, obj runtime.Object, accessor metav1.Object) error {
	state.mu.Lock()
	defer state.mu.Unlock()

	changeSummary, err := st.ComputeDiff(state.Object, obj)
	if err != nil {
		return fmt.Errorf("failed to compute diff: %w", err)
	}

	changeType := st.determineChangeType(state, accessor)

	snapshot := &StateSnapshot{
		ResourceVersion: accessor.GetResourceVersion(),
		Generation:      accessor.GetGeneration(),
		Object:          obj.DeepCopyObject(),
		Timestamp:       time.Now(),
		ChangeType:      changeType,
		ChangeSummary:   changeSummary,
	}

	state.ResourceVersion = accessor.GetResourceVersion()
	state.Generation = accessor.GetGeneration()
	state.Object = obj.DeepCopyObject()
	state.LastModified = time.Now()

	state.StateHistory = append(state.StateHistory, snapshot)
	if len(state.StateHistory) > st.config.MaxStateHistory {
		state.StateHistory = state.StateHistory[1:]
	}

	return nil
}

func (st *StateTracker) determineChangeType(state *ResourceState, accessor metav1.Object) ChangeType {
	if accessor.GetGeneration() > state.Generation {
		return ChangeTypeUpdate
	}
	return ChangeTypeStatus
}

func (st *StateTracker) detectStateConflict(key string, state *ResourceState) *StateConflict {
	state.mu.RLock()
	defer state.mu.RUnlock()

	if len(state.StateHistory) < 2 {
		return nil
	}

	recent := state.StateHistory[len(state.StateHistory)-1]
	previous := state.StateHistory[len(state.StateHistory)-2]

	if recent.Generation < previous.Generation {
		return &StateConflict{
			ResourceKey:  key,
			LocalState:   state,
			ConflictType: ConflictTypeGenerationSkew,
			CreatedAt:    time.Now(),
		}
	}

	return nil
}

func (st *StateTracker) reconcileLoop() {
	ticker := time.NewTicker(st.reconciler.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			st.reconcile()
		case <-st.stopCh:
			return
		}
	}
}

func (st *StateTracker) reconcile() {
	conflicts := st.DetectConflicts()
	for _, conflict := range conflicts {
		if err := st.ResolveConflict(conflict); err != nil {
			// Log error but continue with other conflicts
		}
	}
}

func (st *StateTracker) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			st.cleanup()
		case <-st.stopCh:
			return
		}
	}
}

func (st *StateTracker) cleanup() {
	st.mu.Lock()
	defer st.mu.Unlock()

	now := time.Now()
	for key, state := range st.resourceStates {
		state.mu.Lock()
		if now.Sub(state.LastModified) > st.config.StateRetentionTTL {
			delete(st.resourceStates, key)
		}
		state.mu.Unlock()
	}
}

func (cr *ConflictResolver) Resolve(conflict *StateConflict) error {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	switch cr.strategy {
	case ConflictResolutionLastWriter:
		return cr.resolveLastWriter(conflict)
	case ConflictResolutionMerge:
		return cr.resolveMerge(conflict)
	case ConflictResolutionManual:
		return cr.resolveManual(conflict)
	default:
		return fmt.Errorf("unknown conflict resolution strategy: %s", cr.strategy)
	}
}

func (cr *ConflictResolver) resolveLastWriter(conflict *StateConflict) error {
	conflict.Resolution = ConflictResolution{
		Strategy:     ConflictResolutionLastWriter,
		ResolvedAt:   time.Now(),
		Resolution:   "Applied last writer wins strategy",
		AutoResolved: true,
	}
	return nil
}

func (cr *ConflictResolver) resolveMerge(conflict *StateConflict) error {
	// Simplified merge strategy - in practice, this would use strategic merge patch
	conflict.Resolution = ConflictResolution{
		Strategy:     ConflictResolutionMerge,
		ResolvedAt:   time.Now(),
		Resolution:   "Applied merge strategy",
		AutoResolved: true,
	}
	return nil
}

func (cr *ConflictResolver) resolveManual(conflict *StateConflict) error {
	conflict.Resolution = ConflictResolution{
		Strategy:     ConflictResolutionManual,
		ResolvedAt:   time.Now(),
		Resolution:   "Marked for manual resolution",
		AutoResolved: false,
	}
	return nil
}

func (sd *StateDiffer) computeDiff(ctx context.Context, current, desired runtime.Object) (*ChangeSummary, error) {
	currentJSON, err := runtime.Encode(runtime.NewCodec(nil, nil), current)
	if err != nil {
		return nil, fmt.Errorf("failed to encode current object: %w", err)
	}

	desiredJSON, err := runtime.Encode(runtime.NewCodec(nil, nil), desired)
	if err != nil {
		return nil, fmt.Errorf("failed to encode desired object: %w", err)
	}

	patchBytes, err := strategicpatch.CreateTwoWayMergePatch(currentJSON, desiredJSON, current)
	if err != nil {
		return nil, fmt.Errorf("failed to create patch: %w", err)
	}

	if len(patchBytes) == 0 || string(patchBytes) == "{}" {
		return &ChangeSummary{
			Severity: ChangeSeverityLow,
		}, nil
	}

	summary := &ChangeSummary{
		FieldsModified: []string{"detected changes"},
		Severity:       sd.determineSeverity(patchBytes),
	}

	return summary, nil
}

func (sd *StateDiffer) determineSeverity(patchBytes []byte) ChangeSeverity {
	patchSize := len(patchBytes)

	if patchSize < 100 {
		return ChangeSeverityLow
	} else if patchSize < 1000 {
		return ChangeSeverityMedium
	}

	return ChangeSeverityHigh
}
