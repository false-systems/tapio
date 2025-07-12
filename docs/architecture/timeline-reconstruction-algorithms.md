# Timeline Reconstruction Algorithms

## Overview

Timeline reconstruction is critical for understanding the sequence and causality of events across distributed systems. This document describes algorithms for building accurate, performant timelines from multi-source events with clock skew, missing data, and high event volumes.

## Core Challenges

1. **Clock Skew**: Different sources have unsynchronized clocks
2. **Event Ordering**: Determining true order with concurrent events  
3. **Missing Events**: Gaps in data collection
4. **Scale**: Processing millions of events efficiently
5. **Correlation**: Identifying related events across sources

## Timeline Data Structure

### Hybrid Index Structure

```go
type Timeline struct {
    // Primary time-ordered index
    timeIndex    *btree.BTree      // O(log n) operations
    
    // Secondary indexes for fast lookup
    entityIndex  map[string]*roaring.Bitmap  // Entity -> Event positions
    typeIndex    map[EventType]*roaring.Bitmap // Type -> Event positions
    
    // Interval tree for range queries
    intervalTree *interval.Tree
    
    // Events storage
    events       []Event          // Append-only event storage
    
    // Metadata
    window       TimeWindow
    stats        TimelineStats
    mu           sync.RWMutex
}

// TimelineStats tracks timeline metrics
type TimelineStats struct {
    EventCount     int64
    TimeRange      TimeRange
    EntityCount    int
    AvgEventSize   int
    IndexSize      int64
    CompressionRatio float64
}
```

### Event Storage Format

```go
type Event struct {
    // Fixed-size header (64 bytes)
    ID           [16]byte     // UUID
    Timestamp    int64        // Unix nano
    Source       uint8        // Source type enum
    Type         uint16       // Event type
    Severity     uint8        // Severity level
    EntityHash   uint64       // Fast entity lookup
    DataOffset   uint32       // Offset to variable data
    DataSize     uint16       // Size of variable data
    Flags        uint16       // Compression, encryption flags
    
    // Variable-size data (stored separately)
    Entity       EntityRef    `msgpack:"e"`
    Attributes   map[string]interface{} `msgpack:"a"`
    Correlation  []string     `msgpack:"c,omitempty"`
}
```

## Clock Synchronization Algorithms

### Hybrid Logical Clock (HLC)

```go
type HybridLogicalClock struct {
    physicalTime int64
    logical      int64
    nodeID       uint16
    mu           sync.Mutex
}

func (hlc *HybridLogicalClock) Now() Timestamp {
    hlc.mu.Lock()
    defer hlc.mu.Unlock()
    
    physical := time.Now().UnixNano()
    
    if physical > hlc.physicalTime {
        hlc.physicalTime = physical
        hlc.logical = 0
    } else {
        hlc.logical++
    }
    
    return Timestamp{
        Physical: hlc.physicalTime,
        Logical:  hlc.logical,
        NodeID:   hlc.nodeID,
    }
}

func (hlc *HybridLogicalClock) Update(remote Timestamp) Timestamp {
    hlc.mu.Lock()
    defer hlc.mu.Unlock()
    
    physical := time.Now().UnixNano()
    
    if remote.Physical > hlc.physicalTime && remote.Physical > physical {
        hlc.physicalTime = remote.Physical
        hlc.logical = remote.Logical + 1
    } else if remote.Physical == hlc.physicalTime {
        if remote.Logical > hlc.logical {
            hlc.logical = remote.Logical + 1
        } else {
            hlc.logical++
        }
    } else if physical > hlc.physicalTime {
        hlc.physicalTime = physical
        hlc.logical = 0
    } else {
        hlc.logical++
    }
    
    return Timestamp{
        Physical: hlc.physicalTime,
        Logical:  hlc.logical,
        NodeID:   hlc.nodeID,
    }
}
```

### Clock Skew Detection and Correction

```go
type ClockSkewDetector struct {
    sources      map[SourceType]*SourceClock
    corrections  map[SourceType]time.Duration
    window       *SlidingWindow
}

type SourceClock struct {
    LastSeen     time.Time
    Samples      []TimeSample
    EstimatedSkew time.Duration
    Confidence   float64
}

type TimeSample struct {
    SourceTime time.Time
    LocalTime  time.Time
    EventType  string
}

func (csd *ClockSkewDetector) EstimateSkew(source SourceType) time.Duration {
    clock := csd.sources[source]
    if len(clock.Samples) < 10 {
        return 0 // Not enough samples
    }
    
    // Use linear regression to estimate clock skew
    var sumX, sumY, sumXY, sumX2 float64
    n := float64(len(clock.Samples))
    
    for _, sample := range clock.Samples {
        x := float64(sample.LocalTime.UnixNano())
        y := float64(sample.SourceTime.UnixNano())
        
        sumX += x
        sumY += y
        sumXY += x * y
        sumX2 += x * x
    }
    
    // Calculate slope (clock rate difference)
    slope := (n*sumXY - sumX*sumY) / (n*sumX2 - sumX*sumX)
    
    // Calculate intercept (clock offset)
    intercept := (sumY - slope*sumX) / n
    
    // Estimate current skew
    currentLocal := float64(time.Now().UnixNano())
    estimatedSource := slope*currentLocal + intercept
    skew := time.Duration(estimatedSource - currentLocal)
    
    // Update confidence based on sample variance
    clock.EstimatedSkew = skew
    clock.Confidence = csd.calculateConfidence(clock.Samples, slope, intercept)
    
    return skew
}

func (csd *ClockSkewDetector) CorrectTimestamp(event Event) time.Time {
    skew := csd.corrections[event.Source]
    return event.Timestamp.Add(-skew)
}
```

## Event Ordering Algorithms

### Lamport Timestamps

```go
type LamportClock struct {
    counter uint64
    mu      sync.Mutex
}

func (lc *LamportClock) Increment() uint64 {
    lc.mu.Lock()
    defer lc.mu.Unlock()
    
    lc.counter++
    return lc.counter
}

func (lc *LamportClock) Update(remote uint64) uint64 {
    lc.mu.Lock()
    defer lc.mu.Unlock()
    
    if remote > lc.counter {
        lc.counter = remote
    }
    lc.counter++
    return lc.counter
}
```

### Vector Clocks

```go
type VectorClock struct {
    clocks map[string]uint64
    nodeID string
    mu     sync.RWMutex
}

func (vc *VectorClock) Increment() map[string]uint64 {
    vc.mu.Lock()
    defer vc.mu.Unlock()
    
    vc.clocks[vc.nodeID]++
    return vc.copy()
}

func (vc *VectorClock) Update(remote map[string]uint64) map[string]uint64 {
    vc.mu.Lock()
    defer vc.mu.Unlock()
    
    // Merge remote clock
    for node, timestamp := range remote {
        if timestamp > vc.clocks[node] {
            vc.clocks[node] = timestamp
        }
    }
    
    // Increment local clock
    vc.clocks[vc.nodeID]++
    return vc.copy()
}

func (vc *VectorClock) HappensBefore(a, b map[string]uint64) bool {
    // a happens-before b if all a[i] <= b[i] and at least one a[i] < b[i]
    hasLess := false
    
    for node, aTime := range a {
        bTime := b[node]
        if aTime > bTime {
            return false
        }
        if aTime < bTime {
            hasLess = true
        }
    }
    
    return hasLess
}

func (vc *VectorClock) Concurrent(a, b map[string]uint64) bool {
    return !vc.HappensBefore(a, b) && !vc.HappensBefore(b, a)
}
```

### Causal Ordering Algorithm

```go
type CausalOrderer struct {
    graph    *dag.Graph
    events   map[string]*Event
    pending  map[string][]*Event
}

func (co *CausalOrderer) AddEvent(event *Event) {
    co.events[event.ID] = event
    
    // Add node to graph
    co.graph.AddNode(event.ID)
    
    // Process causal dependencies
    for _, dep := range event.Dependencies {
        if _, exists := co.events[dep]; exists {
            // Dependency already processed
            co.graph.AddEdge(dep, event.ID)
        } else {
            // Queue for later processing
            co.pending[dep] = append(co.pending[dep], event)
        }
    }
    
    // Process any events waiting on this one
    if waiting := co.pending[event.ID]; len(waiting) > 0 {
        for _, waitingEvent := range waiting {
            co.graph.AddEdge(event.ID, waitingEvent.ID)
        }
        delete(co.pending, event.ID)
    }
}

func (co *CausalOrderer) GetOrdered() []*Event {
    // Topological sort of the causal graph
    ordered := co.graph.TopologicalSort()
    
    result := make([]*Event, 0, len(ordered))
    for _, id := range ordered {
        if event, exists := co.events[id]; exists {
            result = append(result, event)
        }
    }
    
    return result
}
```

## Gap Detection and Interpolation

### Missing Event Detection

```go
type GapDetector struct {
    sources       map[SourceType]*SourceTracker
    threshold     time.Duration
    interpolator  Interpolator
}

type SourceTracker struct {
    LastSeen      time.Time
    ExpectedRate  float64
    RateWindow    *RateEstimator
    Gaps          []TimeGap
}

type TimeGap struct {
    Start    time.Time
    End      time.Time
    Expected int
    Severity GapSeverity
}

func (gd *GapDetector) DetectGaps(timeline *Timeline) []TimeGap {
    var allGaps []TimeGap
    
    for source, tracker := range gd.sources {
        events := timeline.GetEventsBySource(source)
        gaps := gd.detectSourceGaps(events, tracker)
        allGaps = append(allGaps, gaps...)
    }
    
    return allGaps
}

func (gd *GapDetector) detectSourceGaps(events []Event, tracker *SourceTracker) []TimeGap {
    var gaps []TimeGap
    
    for i := 1; i < len(events); i++ {
        timeDiff := events[i].Timestamp.Sub(events[i-1].Timestamp)
        expectedEvents := tracker.ExpectedRate * timeDiff.Seconds()
        
        if timeDiff > gd.threshold && expectedEvents > 1.5 {
            gap := TimeGap{
                Start:    events[i-1].Timestamp,
                End:      events[i].Timestamp,
                Expected: int(expectedEvents),
            }
            
            // Classify gap severity
            if expectedEvents > 100 {
                gap.Severity = GapSeverityCritical
            } else if expectedEvents > 10 {
                gap.Severity = GapSeverityHigh
            } else {
                gap.Severity = GapSeverityLow
            }
            
            gaps = append(gaps, gap)
        }
    }
    
    return gaps
}
```

### Event Interpolation

```go
type Interpolator interface {
    Interpolate(gap TimeGap, before, after []Event) []Event
}

type StatisticalInterpolator struct {
    patterns *PatternLibrary
    ml       *MLPredictor
}

func (si *StatisticalInterpolator) Interpolate(gap TimeGap, before, after []Event) []Event {
    // Analyze patterns in surrounding events
    beforePattern := si.patterns.AnalyzeSequence(before)
    afterPattern := si.patterns.AnalyzeSequence(after)
    
    // Use ML model to predict likely events
    features := si.extractFeatures(gap, beforePattern, afterPattern)
    predictions := si.ml.Predict(features)
    
    // Generate interpolated events
    var interpolated []Event
    
    for _, pred := range predictions {
        if pred.Confidence > 0.7 {
            event := Event{
                Timestamp:   pred.Timestamp,
                Type:        pred.Type,
                Source:      pred.Source,
                Interpolated: true,
                Confidence:  pred.Confidence,
            }
            interpolated = append(interpolated, event)
        }
    }
    
    return interpolated
}
```

## Correlation Detection

### Sliding Window Correlation

```go
type WindowCorrelator struct {
    window   time.Duration
    overlap  time.Duration
    matchers []CorrelationMatcher
}

func (wc *WindowCorrelator) FindCorrelations(timeline *Timeline) []Correlation {
    var correlations []Correlation
    
    // Slide window across timeline
    start := timeline.StartTime()
    end := start.Add(wc.window)
    
    for end.Before(timeline.EndTime()) {
        // Get events in current window
        windowEvents := timeline.GetEventsInRange(start, end)
        
        // Check each correlation matcher
        for _, matcher := range wc.matchers {
            if matches := matcher.Match(windowEvents); len(matches) > 0 {
                correlations = append(correlations, matches...)
            }
        }
        
        // Slide window
        start = start.Add(wc.window - wc.overlap)
        end = start.Add(wc.window)
    }
    
    return correlations
}
```

### Pattern-Based Correlation

```go
type PatternMatcher struct {
    patterns []Pattern
    index    *PatternIndex
}

type Pattern struct {
    Name      string
    Sequence  []EventMatcher
    Window    time.Duration
    MinEvents int
}

type EventMatcher interface {
    Matches(event Event) bool
}

func (pm *PatternMatcher) Match(events []Event) []PatternMatch {
    var matches []PatternMatch
    
    // Build suffix array for efficient pattern matching
    sa := NewSuffixArray(events)
    
    for _, pattern := range pm.patterns {
        // Use Boyer-Moore for pattern search
        positions := sa.FindPattern(pattern.Sequence)
        
        for _, pos := range positions {
            // Verify time constraints
            matchEvents := events[pos : pos+len(pattern.Sequence)]
            
            if pm.verifyTimeConstraints(matchEvents, pattern.Window) {
                match := PatternMatch{
                    Pattern:    pattern.Name,
                    Events:     matchEvents,
                    Confidence: pm.calculateConfidence(matchEvents, pattern),
                }
                matches = append(matches, match)
            }
        }
    }
    
    return matches
}
```

### Graph-Based Correlation

```go
type GraphCorrelator struct {
    builder GraphBuilder
    analyzer GraphAnalyzer
}

func (gc *GraphCorrelator) BuildCorrelationGraph(timeline *Timeline) *CorrelationGraph {
    graph := NewCorrelationGraph()
    
    // Add nodes for each event
    for _, event := range timeline.GetEvents() {
        graph.AddNode(event)
    }
    
    // Add edges based on relationships
    gc.addTemporalEdges(graph, timeline)
    gc.addCausalEdges(graph, timeline)
    gc.addEntityEdges(graph, timeline)
    
    return graph
}

func (gc *GraphCorrelator) FindCorrelationClusters(graph *CorrelationGraph) []Cluster {
    // Use community detection algorithm
    communities := gc.analyzer.DetectCommunities(graph)
    
    var clusters []Cluster
    for _, community := range communities {
        if gc.isSignificantCluster(community) {
            cluster := Cluster{
                Events:     community.Nodes,
                Cohesion:   gc.calculateCohesion(community),
                Type:       gc.classifyCluster(community),
            }
            clusters = append(clusters, cluster)
        }
    }
    
    return clusters
}
```

## Performance Optimizations

### Parallel Timeline Construction

```go
type ParallelTimelineBuilder struct {
    workers   int
    chunkSize int
}

func (ptb *ParallelTimelineBuilder) Build(events []Event) *Timeline {
    // Partition events by time chunks
    chunks := ptb.partitionEvents(events)
    
    // Process chunks in parallel
    subTimelines := make([]*Timeline, len(chunks))
    var wg sync.WaitGroup
    
    for i, chunk := range chunks {
        wg.Add(1)
        go func(idx int, events []Event) {
            defer wg.Done()
            subTimelines[idx] = ptb.buildSubTimeline(events)
        }(i, chunk)
    }
    
    wg.Wait()
    
    // Merge sub-timelines
    return ptb.mergeTimelines(subTimelines)
}

func (ptb *ParallelTimelineBuilder) mergeTimelines(timelines []*Timeline) *Timeline {
    // Use k-way merge with heap
    merged := NewTimeline()
    heap := NewEventHeap()
    
    // Initialize heap with first event from each timeline
    for i, tl := range timelines {
        if event := tl.Next(); event != nil {
            heap.Push(&HeapItem{
                Event:    event,
                Source:   i,
                Timeline: tl,
            })
        }
    }
    
    // Merge events
    for heap.Len() > 0 {
        item := heap.Pop().(*HeapItem)
        merged.AddEvent(item.Event)
        
        // Add next event from same timeline
        if next := item.Timeline.Next(); next != nil {
            heap.Push(&HeapItem{
                Event:    next,
                Source:   item.Source,
                Timeline: item.Timeline,
            })
        }
    }
    
    return merged
}
```

### Incremental Timeline Updates

```go
type IncrementalTimeline struct {
    base     *Timeline
    delta    *DeltaLog
    merger   *DeltaMerger
}

type DeltaLog struct {
    events    []Event
    deletions []string
    mu        sync.RWMutex
}

func (it *IncrementalTimeline) AddEvent(event Event) {
    it.delta.mu.Lock()
    defer it.delta.mu.Unlock()
    
    it.delta.events = append(it.delta.events, event)
    
    // Trigger merge if delta gets too large
    if len(it.delta.events) > 10000 {
        go it.mergeDelta()
    }
}

func (it *IncrementalTimeline) Query(start, end time.Time) []Event {
    // Query both base and delta
    baseEvents := it.base.GetEventsInRange(start, end)
    
    it.delta.mu.RLock()
    deltaEvents := it.filterDeltaEvents(start, end)
    it.delta.mu.RUnlock()
    
    // Merge results
    return it.merger.MergeResults(baseEvents, deltaEvents)
}

func (it *IncrementalTimeline) mergeDelta() {
    it.delta.mu.Lock()
    events := it.delta.events
    it.delta.events = nil
    it.delta.mu.Unlock()
    
    // Merge into base timeline
    it.base.AddBatch(events)
}
```

### Memory-Efficient Storage

```go
type CompressedTimeline struct {
    chunks    []*CompressedChunk
    index     *ChunkIndex
    allocator *MemoryAllocator
}

type CompressedChunk struct {
    StartTime time.Time
    EndTime   time.Time
    Data      []byte // Compressed events
    Index     []uint32 // Offsets for binary search
}

func (ct *CompressedTimeline) AddEvent(event Event) {
    // Serialize event
    data := ct.serializeEvent(event)
    
    // Find or create chunk
    chunk := ct.getCurrentChunk()
    
    // Compress and append
    compressed := ct.compress(data)
    offset := uint32(len(chunk.Data))
    chunk.Data = append(chunk.Data, compressed...)
    chunk.Index = append(chunk.Index, offset)
    
    // Update chunk metadata
    if event.Timestamp.After(chunk.EndTime) {
        chunk.EndTime = event.Timestamp
    }
}

func (ct *CompressedTimeline) GetEvents(start, end time.Time) []Event {
    // Find relevant chunks
    chunks := ct.index.FindChunks(start, end)
    
    var events []Event
    for _, chunk := range chunks {
        // Decompress chunk
        decompressed := ct.decompress(chunk.Data)
        
        // Extract events in range
        chunkEvents := ct.extractEvents(decompressed, chunk.Index, start, end)
        events = append(events, chunkEvents...)
    }
    
    return events
}
```

## Advanced Algorithms

### Machine Learning for Anomaly Detection

```go
type AnomalyDetector struct {
    model    *IsolationForest
    features *FeatureExtractor
}

func (ad *AnomalyDetector) DetectAnomalies(timeline *Timeline) []Anomaly {
    // Extract features from timeline segments
    segments := timeline.Segment(5 * time.Minute)
    
    var anomalies []Anomaly
    for _, segment := range segments {
        features := ad.features.Extract(segment)
        score := ad.model.AnomalyScore(features)
        
        if score > 0.8 {
            anomaly := Anomaly{
                TimeRange:  segment.TimeRange(),
                Score:      score,
                Events:     segment.Events,
                Indicators: ad.explainAnomaly(features, score),
            }
            anomalies = append(anomalies, anomaly)
        }
    }
    
    return anomalies
}
```

### Predictive Timeline Analysis

```go
type TimelinePredictor struct {
    lstm     *LSTMModel
    patterns *PatternLibrary
}

func (tp *TimelinePredictor) PredictNext(timeline *Timeline, horizon time.Duration) []PredictedEvent {
    // Extract sequential features
    sequence := tp.extractSequence(timeline)
    
    // Run LSTM prediction
    predictions := tp.lstm.Predict(sequence, horizon)
    
    // Convert to predicted events
    var events []PredictedEvent
    for _, pred := range predictions {
        event := PredictedEvent{
            Timestamp:   pred.Time,
            Type:        tp.patterns.MapPrediction(pred),
            Probability: pred.Confidence,
            Explanation: tp.explainPrediction(pred),
        }
        events = append(events, event)
    }
    
    return events
}
```