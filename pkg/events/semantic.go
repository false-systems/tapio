package events

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/events/opinionated"
)

// SemanticEnricher provides OPINIONATED semantic enrichment at collection time.
// This is where we transform raw events into AI-ready intelligence.
type SemanticEnricher struct {
	// Ontology for domain-specific tagging
	ontology *EventOntology

	// Embedding generator for semantic vectors
	embedder *SemanticEmbedder

	// Intent classifier
	intentClassifier *IntentClassifier

	// Feature extractors
	extractors map[string]FeatureExtractor

	// Cache for performance
	cache *SemanticCache

	// Metrics
	metrics *EnrichmentMetrics
}

// EventOntology defines our OPINIONATED taxonomy of events
type EventOntology struct {
	mu sync.RWMutex

	// Hierarchical event taxonomy
	taxonomy map[string]*TaxonomyNode

	// Domain concepts
	concepts map[string]*Concept

	// Relationships between concepts
	relations map[string][]Relation
}

// TaxonomyNode represents a node in our event taxonomy
type TaxonomyNode struct {
	Name        string
	Path        string
	Level       int
	Parent      *TaxonomyNode
	Children    []*TaxonomyNode
	Tags        []string
	Importance  float32
	AIRelevance float32
}

// Concept represents a domain concept
type Concept struct {
	ID          string
	Name        string
	Description string
	Embedding   []float32
	Aliases     []string
	Properties  map[string]interface{}
}

// Relation between concepts
type Relation struct {
	From       string
	To         string
	Type       string
	Strength   float32
	Bidirectional bool
}

// SemanticEmbedder generates semantic embeddings
type SemanticEmbedder struct {
	// Model for generating embeddings
	model EmbeddingModel

	// Dimension of embeddings
	dimension int

	// Cache for common embeddings
	cache *sync.Map
}

// EmbeddingModel interface for different embedding generators
type EmbeddingModel interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
}

// IntentClassifier determines the intent behind events
type IntentClassifier struct {
	// Classification model
	model ClassificationModel

	// Intent categories
	categories []IntentCategory

	// Confidence threshold
	threshold float32
}

// IntentCategory represents a possible intent
type IntentCategory struct {
	Name        string
	Description string
	Indicators  []string
	Priority    int
}

// ClassificationModel interface for intent classification
type ClassificationModel interface {
	Classify(ctx context.Context, features map[string]float32) (string, float32, error)
}

// FeatureExtractor extracts semantic features from events
type FeatureExtractor interface {
	Extract(ctx context.Context, event RawEvent) (map[string]float32, error)
	Name() string
	Version() string
}

// SemanticCache for performance optimization
type SemanticCache struct {
	embeddings *sync.Map
	intents    *sync.Map
	features   *sync.Map
	ttl        time.Duration
}

// EnrichmentMetrics tracks enrichment performance
type EnrichmentMetrics struct {
	mu sync.RWMutex

	EventsEnriched      uint64
	EnrichmentDuration  time.Duration
	CacheHits          uint64
	CacheMisses        uint64
	EmbeddingTime      time.Duration
	ClassificationTime time.Duration
	ExtractionTime     time.Duration
}

// RawEvent represents an unenriched event
type RawEvent struct {
	Type       string
	Source     string
	Entity     string
	Data       map[string]interface{}
	Timestamp  time.Time
}

// NewSemanticEnricher creates an OPINIONATED semantic enricher
func NewSemanticEnricher() *SemanticEnricher {
	return &SemanticEnricher{
		ontology:         buildOpinionatedOntology(),
		embedder:        newSemanticEmbedder(),
		intentClassifier: newIntentClassifier(),
		extractors:      buildFeatureExtractors(),
		cache:           newSemanticCache(),
		metrics:         &EnrichmentMetrics{},
	}
}

// Enrich performs OPINIONATED semantic enrichment at collection time
func (se *SemanticEnricher) Enrich(ctx context.Context, raw RawEvent) (*opinionated.SemanticContext, error) {
	start := time.Now()
	defer func() {
		se.metrics.mu.Lock()
		se.metrics.EventsEnriched++
		se.metrics.EnrichmentDuration += time.Since(start)
		se.metrics.mu.Unlock()
	}()

	// Build semantic context
	semantic := &opinionated.SemanticContext{}

	// 1. Classify event type using our opinionated taxonomy
	eventType := se.classifyEventType(raw)
	semantic.EventType = eventType

	// 2. Generate semantic embedding
	embedding, err := se.generateEmbedding(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("embedding generation failed: %w", err)
	}
	semantic.Embedding = embedding

	// 3. Extract ontology tags
	tags := se.extractOntologyTags(raw, eventType)
	semantic.OntologyTags = tags

	// 4. Generate natural language description
	description := se.generateDescription(raw, eventType)
	semantic.Description = description

	// 5. Extract semantic features
	features, err := se.extractSemanticFeatures(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("feature extraction failed: %w", err)
	}
	semantic.SemanticFeatures = features

	// 6. Classify intent
	intent, confidence, err := se.classifyIntent(ctx, raw, features)
	if err != nil {
		return nil, fmt.Errorf("intent classification failed: %w", err)
	}
	semantic.Intent = intent
	semantic.IntentConfidence = confidence

	return semantic, nil
}

// classifyEventType uses our opinionated taxonomy
func (se *SemanticEnricher) classifyEventType(raw RawEvent) string {
	se.ontology.mu.RLock()
	defer se.ontology.mu.RUnlock()

	// Start with raw type
	path := []string{}

	// Map to our taxonomy
	if node, exists := se.ontology.taxonomy[raw.Type]; exists {
		current := node
		for current != nil {
			path = append([]string{current.Name}, path...)
			current = current.Parent
		}
	} else {
		// Default classification based on patterns
		path = se.inferTaxonomyPath(raw)
	}

	return strings.Join(path, ".")
}

// inferTaxonomyPath infers taxonomy path from event data
func (se *SemanticEnricher) inferTaxonomyPath(raw RawEvent) []string {
	// OPINIONATED inference rules
	switch {
	case strings.Contains(raw.Type, "memory"):
		return []string{"resource", "exhaustion", "memory"}
	case strings.Contains(raw.Type, "cpu"):
		return []string{"resource", "exhaustion", "cpu"}
	case strings.Contains(raw.Type, "network"):
		return []string{"connectivity", "network", raw.Type}
	case strings.Contains(raw.Type, "pod"):
		return []string{"lifecycle", "kubernetes", "pod", raw.Type}
	case strings.Contains(raw.Type, "error"):
		return []string{"failure", "application", raw.Type}
	default:
		return []string{"unknown", raw.Type}
	}
}

// generateEmbedding creates semantic vector representation
func (se *SemanticEnricher) generateEmbedding(ctx context.Context, raw RawEvent) ([]float32, error) {
	// Check cache first
	cacheKey := fmt.Sprintf("%s:%s:%s", raw.Type, raw.Source, raw.Entity)
	if cached, ok := se.cache.embeddings.Load(cacheKey); ok {
		se.metrics.mu.Lock()
		se.metrics.CacheHits++
		se.metrics.mu.Unlock()
		return cached.([]float32), nil
	}

	se.metrics.mu.Lock()
	se.metrics.CacheMisses++
	se.metrics.mu.Unlock()

	// Generate embedding
	start := time.Now()
	text := se.buildEmbeddingText(raw)
	embedding, err := se.embedder.model.Embed(ctx, text)
	if err != nil {
		return nil, err
	}

	se.metrics.mu.Lock()
	se.metrics.EmbeddingTime += time.Since(start)
	se.metrics.mu.Unlock()

	// Cache for future use
	se.cache.embeddings.Store(cacheKey, embedding)

	return embedding, nil
}

// buildEmbeddingText creates text representation for embedding
func (se *SemanticEnricher) buildEmbeddingText(raw RawEvent) string {
	parts := []string{
		raw.Type,
		raw.Source,
		raw.Entity,
	}

	// Add key data fields
	for k, v := range raw.Data {
		parts = append(parts, fmt.Sprintf("%s:%v", k, v))
	}

	return strings.Join(parts, " ")
}

// extractOntologyTags extracts domain-specific tags
func (se *SemanticEnricher) extractOntologyTags(raw RawEvent, eventType string) []string {
	tags := make(map[string]bool)

	// Add taxonomy tags
	se.ontology.mu.RLock()
	if node, exists := se.ontology.taxonomy[raw.Type]; exists {
		for _, tag := range node.Tags {
			tags[tag] = true
		}
	}

	// Add concept tags based on data
	for conceptID, concept := range se.ontology.concepts {
		if se.matchesConcept(raw, concept) {
			tags[conceptID] = true
			// Add related concepts
			if relations, exists := se.ontology.relations[conceptID]; exists {
				for _, rel := range relations {
					if rel.Strength > 0.7 {
						tags[rel.To] = true
					}
				}
			}
		}
	}
	se.ontology.mu.RUnlock()

	// Convert to slice
	result := make([]string, 0, len(tags))
	for tag := range tags {
		result = append(result, tag)
	}

	return result
}

// matchesConcept checks if event matches a concept
func (se *SemanticEnricher) matchesConcept(raw RawEvent, concept *Concept) bool {
	// Check aliases
	eventText := strings.ToLower(raw.Type + " " + raw.Source)
	for _, alias := range concept.Aliases {
		if strings.Contains(eventText, strings.ToLower(alias)) {
			return true
		}
	}

	// Check properties
	for key, expectedValue := range concept.Properties {
		if actualValue, exists := raw.Data[key]; exists {
			if fmt.Sprintf("%v", actualValue) == fmt.Sprintf("%v", expectedValue) {
				return true
			}
		}
	}

	return false
}

// generateDescription creates natural language description
func (se *SemanticEnricher) generateDescription(raw RawEvent, eventType string) string {
	// OPINIONATED templates for different event types
	templates := map[string]string{
		"resource.exhaustion.memory": "%s is experiencing memory pressure (%v%% used)",
		"resource.exhaustion.cpu":    "%s has high CPU utilization (%v%%)",
		"lifecycle.kubernetes.pod":   "Pod %s %s in namespace %s",
		"connectivity.network":       "Network %s from %s to %s",
		"failure.application":        "Application error in %s: %s",
	}

	// Find matching template
	template, exists := templates[eventType]
	if !exists {
		template = "Event %s occurred on %s"
	}

	// Fill template with data
	args := se.extractTemplateArgs(raw, eventType)
	return fmt.Sprintf(template, args...)
}

// extractTemplateArgs extracts arguments for description template
func (se *SemanticEnricher) extractTemplateArgs(raw RawEvent, eventType string) []interface{} {
	switch eventType {
	case "resource.exhaustion.memory":
		usage := raw.Data["memory_usage_percent"]
		return []interface{}{raw.Entity, usage}
	case "resource.exhaustion.cpu":
		usage := raw.Data["cpu_usage_percent"]
		return []interface{}{raw.Entity, usage}
	case "lifecycle.kubernetes.pod":
		action := raw.Data["action"]
		namespace := raw.Data["namespace"]
		return []interface{}{raw.Entity, action, namespace}
	case "connectivity.network":
		action := raw.Data["action"]
		src := raw.Data["source"]
		dst := raw.Data["destination"]
		return []interface{}{action, src, dst}
	case "failure.application":
		error := raw.Data["error"]
		return []interface{}{raw.Entity, error}
	default:
		return []interface{}{raw.Type, raw.Entity}
	}
}

// extractSemanticFeatures extracts AI-ready features
func (se *SemanticEnricher) extractSemanticFeatures(ctx context.Context, raw RawEvent) (map[string]float32, error) {
	features := make(map[string]float32)

	// Run all extractors in parallel
	var wg sync.WaitGroup
	var mu sync.Mutex
	errors := make(chan error, len(se.extractors))

	start := time.Now()
	for name, extractor := range se.extractors {
		wg.Add(1)
		go func(n string, e FeatureExtractor) {
			defer wg.Done()
			
			extracted, err := e.Extract(ctx, raw)
			if err != nil {
				errors <- fmt.Errorf("%s extractor: %w", n, err)
				return
			}

			mu.Lock()
			for k, v := range extracted {
				features[n+"."+k] = v
			}
			mu.Unlock()
		}(name, extractor)
	}

	wg.Wait()
	close(errors)

	se.metrics.mu.Lock()
	se.metrics.ExtractionTime += time.Since(start)
	se.metrics.mu.Unlock()

	// Check for errors
	for err := range errors {
		return nil, err
	}

	return features, nil
}

// classifyIntent determines event intent
func (se *SemanticEnricher) classifyIntent(ctx context.Context, raw RawEvent, features map[string]float32) (string, float32, error) {
	// Check cache
	cacheKey := fmt.Sprintf("%s:%s", raw.Type, raw.Entity)
	if cached, ok := se.cache.intents.Load(cacheKey); ok {
		result := cached.(intentResult)
		return result.intent, result.confidence, nil
	}

	start := time.Now()
	intent, confidence, err := se.intentClassifier.model.Classify(ctx, features)
	if err != nil {
		return "", 0, err
	}

	se.metrics.mu.Lock()
	se.metrics.ClassificationTime += time.Since(start)
	se.metrics.mu.Unlock()

	// Cache result
	se.cache.intents.Store(cacheKey, intentResult{intent, confidence})

	return intent, confidence, nil
}

// intentResult for caching
type intentResult struct {
	intent     string
	confidence float32
}

// buildOpinionatedOntology creates our opinionated event ontology
func buildOpinionatedOntology() *EventOntology {
	ontology := &EventOntology{
		taxonomy:  make(map[string]*TaxonomyNode),
		concepts:  make(map[string]*Concept),
		relations: make(map[string][]Relation),
	}

	// Build taxonomy tree
	root := &TaxonomyNode{Name: "root", Level: 0}
	
	// Resource events
	resource := &TaxonomyNode{Name: "resource", Parent: root, Level: 1}
	exhaustion := &TaxonomyNode{Name: "exhaustion", Parent: resource, Level: 2}
	memory := &TaxonomyNode{Name: "memory", Parent: exhaustion, Level: 3, Tags: []string{"critical", "performance"}}
	cpu := &TaxonomyNode{Name: "cpu", Parent: exhaustion, Level: 3, Tags: []string{"critical", "performance"}}

	// Lifecycle events
	lifecycle := &TaxonomyNode{Name: "lifecycle", Parent: root, Level: 1}
	kubernetes := &TaxonomyNode{Name: "kubernetes", Parent: lifecycle, Level: 2}
	pod := &TaxonomyNode{Name: "pod", Parent: kubernetes, Level: 3, Tags: []string{"infrastructure"}}

	// Add to taxonomy
	ontology.taxonomy["memory"] = memory
	ontology.taxonomy["cpu"] = cpu
	ontology.taxonomy["pod"] = pod

	// Add concepts
	ontology.concepts["oom"] = &Concept{
		ID:          "oom",
		Name:        "Out of Memory",
		Description: "Memory exhaustion leading to process termination",
		Aliases:     []string{"oom", "out of memory", "memory exhausted"},
		Properties:  map[string]interface{}{"severity": "critical"},
	}

	ontology.concepts["cpu_throttle"] = &Concept{
		ID:          "cpu_throttle",
		Name:        "CPU Throttling",
		Description: "CPU usage limited by cgroup constraints",
		Aliases:     []string{"throttle", "cpu limit", "cgroup limit"},
		Properties:  map[string]interface{}{"impact": "performance"},
	}

	// Add relations
	ontology.relations["oom"] = []Relation{
		{From: "oom", To: "pod_restart", Type: "causes", Strength: 0.9},
		{From: "oom", To: "service_degradation", Type: "causes", Strength: 0.8},
	}

	return ontology
}

// newSemanticEmbedder creates the embedding generator
func newSemanticEmbedder() *SemanticEmbedder {
	return &SemanticEmbedder{
		model:     &SimpleEmbeddingModel{dimension: 128}, // Simplified for now
		dimension: 128,
		cache:     &sync.Map{},
	}
}

// SimpleEmbeddingModel is a placeholder embedding model
type SimpleEmbeddingModel struct {
	dimension int
}

func (m *SimpleEmbeddingModel) Embed(ctx context.Context, text string) ([]float32, error) {
	// Simple hash-based embedding for demonstration
	embedding := make([]float32, m.dimension)
	hash := 0
	for _, r := range text {
		hash = hash*31 + int(r)
	}
	
	for i := 0; i < m.dimension; i++ {
		// Generate deterministic values based on hash
		embedding[i] = float32(math.Sin(float64(hash+i))) * 0.5 + 0.5
	}
	
	return embedding, nil
}

func (m *SimpleEmbeddingModel) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	embeddings := make([][]float32, len(texts))
	for i, text := range texts {
		emb, err := m.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		embeddings[i] = emb
	}
	return embeddings, nil
}

// newIntentClassifier creates the intent classifier
func newIntentClassifier() *IntentClassifier {
	return &IntentClassifier{
		model: &SimpleClassificationModel{},
		categories: []IntentCategory{
			{Name: "normal_operation", Description: "Regular system behavior"},
			{Name: "debugging", Description: "Diagnostic or debugging activity"},
			{Name: "maintenance", Description: "Planned maintenance activity"},
			{Name: "incident", Description: "Unexpected incident or failure"},
			{Name: "attack", Description: "Potential security threat"},
		},
		threshold: 0.7,
	}
}

// SimpleClassificationModel is a placeholder classifier
type SimpleClassificationModel struct{}

func (m *SimpleClassificationModel) Classify(ctx context.Context, features map[string]float32) (string, float32, error) {
	// Rule-based classification for demonstration
	if features["error.count"] > 0 {
		return "incident", 0.9, nil
	}
	if features["security.suspicious"] > 0.5 {
		return "attack", 0.8, nil
	}
	if features["debug.enabled"] > 0 {
		return "debugging", 0.95, nil
	}
	if features["maintenance.window"] > 0 {
		return "maintenance", 0.99, nil
	}
	return "normal_operation", 0.85, nil
}

// buildFeatureExtractors creates semantic feature extractors
func buildFeatureExtractors() map[string]FeatureExtractor {
	return map[string]FeatureExtractor{
		"temporal":   &TemporalFeatureExtractor{},
		"entity":     &EntityFeatureExtractor{},
		"behavioral": &BehavioralFeatureExtractor{},
		"contextual": &ContextualFeatureExtractor{},
	}
}

// TemporalFeatureExtractor extracts time-based features
type TemporalFeatureExtractor struct{}

func (e *TemporalFeatureExtractor) Extract(ctx context.Context, event RawEvent) (map[string]float32, error) {
	features := make(map[string]float32)
	
	// Time of day features
	hour := float32(event.Timestamp.Hour())
	features["hour_of_day"] = hour
	features["is_business_hours"] = 0
	if hour >= 9 && hour <= 17 {
		features["is_business_hours"] = 1
	}
	
	// Day of week features
	dow := float32(event.Timestamp.Weekday())
	features["day_of_week"] = dow
	features["is_weekend"] = 0
	if dow == 0 || dow == 6 {
		features["is_weekend"] = 1
	}
	
	return features, nil
}

func (e *TemporalFeatureExtractor) Name() string    { return "temporal" }
func (e *TemporalFeatureExtractor) Version() string { return "1.0" }

// EntityFeatureExtractor extracts entity-based features
type EntityFeatureExtractor struct{}

func (e *EntityFeatureExtractor) Extract(ctx context.Context, event RawEvent) (map[string]float32, error) {
	features := make(map[string]float32)
	
	// Entity type features
	if strings.Contains(event.Entity, "pod") {
		features["is_pod"] = 1
	}
	if strings.Contains(event.Entity, "node") {
		features["is_node"] = 1
	}
	if strings.Contains(event.Entity, "service") {
		features["is_service"] = 1
	}
	
	// Entity hierarchy depth
	parts := strings.Split(event.Entity, "/")
	features["hierarchy_depth"] = float32(len(parts))
	
	return features, nil
}

func (e *EntityFeatureExtractor) Name() string    { return "entity" }
func (e *EntityFeatureExtractor) Version() string { return "1.0" }

// BehavioralFeatureExtractor extracts behavior-based features
type BehavioralFeatureExtractor struct{}

func (e *BehavioralFeatureExtractor) Extract(ctx context.Context, event RawEvent) (map[string]float32, error) {
	features := make(map[string]float32)
	
	// Extract behavioral indicators
	if val, ok := event.Data["retry_count"].(float64); ok {
		features["retry_behavior"] = float32(val)
	}
	if val, ok := event.Data["error_rate"].(float64); ok {
		features["error_behavior"] = float32(val)
	}
	if val, ok := event.Data["request_rate"].(float64); ok {
		features["activity_level"] = float32(val)
	}
	
	return features, nil
}

func (e *BehavioralFeatureExtractor) Name() string    { return "behavioral" }
func (e *BehavioralFeatureExtractor) Version() string { return "1.0" }

// ContextualFeatureExtractor extracts context-based features
type ContextualFeatureExtractor struct{}

func (e *ContextualFeatureExtractor) Extract(ctx context.Context, event RawEvent) (map[string]float32, error) {
	features := make(map[string]float32)
	
	// Extract contextual features
	if val, ok := event.Data["namespace"].(string); ok {
		if val == "kube-system" {
			features["is_system_namespace"] = 1
		}
	}
	if val, ok := event.Data["priority"].(string); ok {
		switch val {
		case "critical":
			features["priority_level"] = 1.0
		case "high":
			features["priority_level"] = 0.75
		case "medium":
			features["priority_level"] = 0.5
		case "low":
			features["priority_level"] = 0.25
		}
	}
	
	return features, nil
}

func (e *ContextualFeatureExtractor) Name() string    { return "contextual" }
func (e *ContextualFeatureExtractor) Version() string { return "1.0" }

// newSemanticCache creates a cache for enrichment results
func newSemanticCache() *SemanticCache {
	return &SemanticCache{
		embeddings: &sync.Map{},
		intents:    &sync.Map{},
		features:   &sync.Map{},
		ttl:        5 * time.Minute,
	}
}

// Metrics returns current enrichment metrics
func (se *SemanticEnricher) Metrics() EnrichmentMetrics {
	se.metrics.mu.RLock()
	defer se.metrics.mu.RUnlock()
	return *se.metrics
}