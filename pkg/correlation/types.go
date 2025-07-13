package correlation

// Re-export foundation types for backward compatibility
// This allows existing code to continue using correlation.Event, etc.
// while internally using the foundation package types

import (
	"github.com/yairfalse/tapio/pkg/correlation/foundation"
)

// ============================================================================
// TYPE ALIASES FOR BACKWARD COMPATIBILITY
// ============================================================================

// Core data types
type Event = foundation.Event
type Entity = foundation.Entity
type TimeWindow = foundation.TimeWindow
type Filter = foundation.Filter

// Severity and categorization
type Severity = foundation.Severity
type Category = foundation.Category
type SourceType = foundation.SourceType

// Re-export constants
const (
	SeverityInfo     = foundation.SeverityInfo
	SeverityWarning  = foundation.SeverityWarning
	SeverityError    = foundation.SeverityError
	SeverityCritical = foundation.SeverityCritical
)

const (
	CategoryPerformance = foundation.CategoryPerformance
	CategorySecurity    = foundation.CategorySecurity
	CategoryReliability = foundation.CategoryReliability
	CategoryCost        = foundation.CategoryCost
	CategoryCapacity    = foundation.CategoryCapacity
	CategoryNetwork     = foundation.CategoryNetwork
)

const (
	SourceEBPF       = foundation.SourceEBPF
	SourceKubernetes = foundation.SourceKubernetes
	SourceSystemd    = foundation.SourceSystemd
	SourceJournald   = foundation.SourceJournald
	SourceMetrics    = foundation.SourceMetrics
	SourcePrometheus = foundation.SourcePrometheus
	SourceOTEL       = foundation.SourceOTEL
)

// Result and finding types
type Result = foundation.Result
type Finding = foundation.Finding
type Evidence = foundation.Evidence
type Prediction = foundation.Prediction

// Metrics and time series
type MetricPoint = foundation.MetricPoint
type MetricSeries = foundation.MetricSeries

// Resource references
type ResourceReference = foundation.ResourceReference
type ResourceInfo = foundation.ResourceInfo

// Confidence and validation
type ConfidenceLevel = foundation.ConfidenceLevel

const (
	ConfidenceLow      = foundation.ConfidenceLow
	ConfidenceMedium   = foundation.ConfidenceMedium
	ConfidenceHigh     = foundation.ConfidenceHigh
	ConfidenceVeryHigh = foundation.ConfidenceVeryHigh
)

// Context and execution types
type DataCollection = foundation.DataCollection
type RuleContext = foundation.RuleContext
type RuleExecution = foundation.RuleExecution
type RulePerformance = foundation.RulePerformance

// Statistics types
type Stats = foundation.Stats
type EventStoreStats = foundation.EventStoreStats

// Health and status types
type HealthStatus = foundation.HealthStatus
type ComponentStatus = foundation.ComponentStatus
type HealthReport = foundation.HealthReport
type ResourceUsage = foundation.ResourceUsage

// ============================================================================
// UTILITY FUNCTIONS
// ============================================================================

// NewTimeWindow creates a time window from start and duration
var NewTimeWindow = foundation.NewTimeWindow

// NewTimeWindowFromRange creates a time window from start and end times
var NewTimeWindowFromRange = foundation.NewTimeWindowFromRange

// EventsByTimestamp sorts events by timestamp
type EventsByTimestamp = foundation.EventsByTimestamp