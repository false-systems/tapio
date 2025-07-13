package correlation

// Re-export foundation interfaces for backward compatibility
// This maintains the existing API while using the clean foundation interfaces

import (
	"github.com/yairfalse/tapio/pkg/correlation/foundation"
)

// ============================================================================
// INTERFACE ALIASES FOR BACKWARD COMPATIBILITY
// ============================================================================

// Core engine interfaces
type Engine = foundation.Engine
type Rule = foundation.Rule

// Data source interfaces
type EventStore = foundation.EventStore
type DataSource = foundation.DataSource
type DataHandler = foundation.DataHandler

// Pattern detection interfaces
type PatternDetector = foundation.PatternDetector
type PatternRegistry = foundation.PatternRegistry

// AutoFix interfaces
type AutoFixEngine = foundation.AutoFixEngine

// Result processing interfaces
type ResultHandler = foundation.ResultHandler
type AlertManager = foundation.AlertManager

// Monitoring interfaces
type MetricsCollector = foundation.MetricsCollector
type HealthChecker = foundation.HealthChecker

// Builder interfaces
type RuleBuilder = foundation.RuleBuilder
type EngineFactory = foundation.EngineFactory

// Configuration interfaces
type ConfigurationManager = foundation.ConfigurationManager
type ConfigChangeHandler = foundation.ConfigChangeHandler

// Validation interfaces
type RuleValidator = foundation.RuleValidator
type PatternValidator = foundation.PatternValidator

// Utility interfaces
type Serializer = foundation.Serializer
type Cache = foundation.Cache

// ============================================================================
// FUNCTION TYPE ALIASES
// ============================================================================

// RuleFunction defines the signature for correlation rule evaluation functions
type RuleFunction = foundation.RuleFunction

// ============================================================================
// ENUM ALIASES
// ============================================================================

// Engine types
type EngineType = foundation.EngineType

const (
	EngineTypeBasic             = foundation.EngineTypeBasic
	EngineTypeEnhanced          = foundation.EngineTypeEnhanced
	EngineTypePerfect           = foundation.EngineTypePerfect
	EngineTypePatternIntegrated = foundation.EngineTypePatternIntegrated
)

// Export formats
type ExportFormat = foundation.ExportFormat

const (
	ExportFormatPrometheus = foundation.ExportFormatPrometheus
	ExportFormatOTEL       = foundation.ExportFormatOTEL
	ExportFormatJSON       = foundation.ExportFormatJSON
	ExportFormatCSV        = foundation.ExportFormatCSV
)

// Serialization formats
type SerializationFormat = foundation.SerializationFormat

const (
	SerializationFormatJSON     = foundation.SerializationFormatJSON
	SerializationFormatProtobuf = foundation.SerializationFormatProtobuf
	SerializationFormatMsgPack  = foundation.SerializationFormatMsgPack
)

// Safety levels
type SafetyLevel = foundation.SafetyLevel

const (
	SafetyLevelSafe      = foundation.SafetyLevelSafe
	SafetyLevelModerate  = foundation.SafetyLevelModerate
	SafetyLevelRisky     = foundation.SafetyLevelRisky
	SafetyLevelDangerous = foundation.SafetyLevelDangerous
)