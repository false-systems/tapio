package base

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// FilterCompiler compiles filter rules into filter functions
type FilterCompiler struct {
	logger *zap.Logger
}

// NewFilterCompiler creates a new filter compiler
func NewFilterCompiler(logger *zap.Logger) *FilterCompiler {
	return &FilterCompiler{logger: logger}
}

// CompileRule compiles a FilterRule into a FilterFunc
func (fc *FilterCompiler) CompileRule(rule *FilterRule) (FilterFunc, error) {
	if rule == nil {
		return nil, fmt.Errorf("cannot compile nil rule")
	}

	// Compile based on filter type
	switch rule.Type {
	case "severity":
		return fc.compileSeverityFilter(rule)
	case "event_type":
		return fc.compileEventTypeFilter(rule)
	case "network":
		return fc.compileNetworkFilter(rule)
	case "dns":
		return fc.compileDNSFilter(rule)
	case "http":
		return fc.compileHTTPFilter(rule)
	case "regex":
		return fc.compileRegexFilter(rule)
	case "time_based":
		return fc.compileTimeBasedFilter(rule)
	default:
		return nil, fmt.Errorf("unknown filter type: %s", rule.Type)
	}
}

// compileSeverityFilter creates a filter for severity levels
func (fc *FilterCompiler) compileSeverityFilter(rule *FilterRule) (FilterFunc, error) {
	severityOrder := map[string]int{
		"DEBUG":    0,
		"INFO":     1,
		"WARNING":  2,
		"ERROR":    3,
		"CRITICAL": 4,
	}

	minLevel := rule.Conditions.MinSeverity
	maxLevel := rule.Conditions.MaxSeverity

	if minLevel == "" && maxLevel == "" {
		return nil, fmt.Errorf("severity filter requires min_severity or max_severity")
	}

	return func(event *domain.CollectorEvent) bool {
		eventLevel, exists := severityOrder[string(event.Severity)]
		if !exists {
			return false
		}

		if minLevel != "" {
			if minOrder, ok := severityOrder[minLevel]; ok && eventLevel < minOrder {
				return false
			}
		}
		if maxLevel != "" {
			if maxOrder, ok := severityOrder[maxLevel]; ok && eventLevel > maxOrder {
				return false
			}
		}
		return true
	}, nil
}

// compileEventTypeFilter creates a filter for event types
func (fc *FilterCompiler) compileEventTypeFilter(rule *FilterRule) (FilterFunc, error) {
	if len(rule.Conditions.Types) == 0 {
		return nil, fmt.Errorf("event_type filter requires 'types' condition")
	}

	// Build allowed types map
	allowedTypes := make(map[string]bool)
	for _, t := range rule.Conditions.Types {
		allowedTypes[t] = true
	}

	return func(event *domain.CollectorEvent) bool {
		return allowedTypes[string(event.Type)]
	}, nil
}

// compileNetworkFilter creates a filter for network events
func (fc *FilterCompiler) compileNetworkFilter(rule *FilterRule) (FilterFunc, error) {
	return func(event *domain.CollectorEvent) bool {
		// Check if it's a network event
		if string(event.Type) != "network" {
			return false
		}

		// Check if network data exists
		if event.EventData.Network == nil {
			return false
		}

		// Check source port filter
		if rule.Conditions.SourcePort > 0 {
			if event.EventData.Network.SrcPort != int32(rule.Conditions.SourcePort) {
				return false
			}
		}

		// Check destination port filter
		if rule.Conditions.DestPort > 0 {
			if event.EventData.Network.DstPort != int32(rule.Conditions.DestPort) {
				return false
			}
		}

		// Check protocol filter
		if rule.Conditions.Protocol != "" {
			if !strings.EqualFold(event.EventData.Network.Protocol, rule.Conditions.Protocol) {
				return false
			}
		}

		return true
	}, nil
}

// compileDNSFilter creates a filter for DNS events
func (fc *FilterCompiler) compileDNSFilter(rule *FilterRule) (FilterFunc, error) {
	var domainRegex *regexp.Regexp
	if rule.Conditions.DomainPattern != "" {
		regex, err := regexp.Compile(rule.Conditions.DomainPattern)
		if err != nil {
			return nil, fmt.Errorf("invalid domain pattern: %w", err)
		}
		domainRegex = regex
	}

	return func(event *domain.CollectorEvent) bool {
		// Check if it's a DNS event
		if string(event.Type) != "dns" {
			return false
		}

		// Check if DNS data exists
		if event.EventData.DNS == nil {
			return false
		}

		// Check domain pattern
		if domainRegex != nil {
			if event.EventData.DNS.QueryName != "" {
				return domainRegex.MatchString(event.EventData.DNS.QueryName)
			}
			return false
		}

		return true
	}, nil
}

// compileHTTPFilter creates a filter for HTTP events
func (fc *FilterCompiler) compileHTTPFilter(rule *FilterRule) (FilterFunc, error) {
	return func(event *domain.CollectorEvent) bool {
		// Check if it's an HTTP event
		if string(event.Type) != "http" {
			return false
		}

		// Check if HTTP data exists
		if event.EventData.HTTP == nil {
			return false
		}

		// Check status code filter
		if rule.Conditions.StatusCode > 0 {
			if event.EventData.HTTP.StatusCode != int32(rule.Conditions.StatusCode) {
				return false
			}
		}

		// Check method filter
		if rule.Conditions.Method != "" {
			if !strings.EqualFold(event.EventData.HTTP.Method, rule.Conditions.Method) {
				return false
			}
		}

		// Check URL pattern filter
		if rule.Conditions.URLPattern != "" {
			regex, err := regexp.Compile(rule.Conditions.URLPattern)
			if err != nil {
				if fc.logger != nil {
					fc.logger.Debug("Invalid URL pattern", zap.Error(err))
				}
				return false
			}
			return regex.MatchString(event.EventData.HTTP.URL)
		}

		return true
	}, nil
}

// compileRegexFilter creates a generic regex-based filter
func (fc *FilterCompiler) compileRegexFilter(rule *FilterRule) (FilterFunc, error) {
	if rule.Conditions.Field == "" || rule.Conditions.Pattern == "" {
		return nil, fmt.Errorf("regex filter requires 'field' and 'pattern' conditions")
	}

	regex, err := regexp.Compile(rule.Conditions.Pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}

	return func(event *domain.CollectorEvent) bool {
		// Check standard fields
		var fieldValue string
		switch rule.Conditions.Field {
		case "type":
			fieldValue = string(event.Type)
		case "severity":
			fieldValue = string(event.Severity)
		case "source":
			fieldValue = event.Source
		default:
			// For custom fields, check RawData if present
			if event.EventData.RawData != nil {
				fieldValue = string(event.EventData.RawData.Data)
			}
		}

		return regex.MatchString(fieldValue)
	}, nil
}

// compileTimeBasedFilter creates a time-based filter
func (fc *FilterCompiler) compileTimeBasedFilter(rule *FilterRule) (FilterFunc, error) {
	if rule.Conditions.StartTime == "" && rule.Conditions.EndTime == "" {
		return nil, fmt.Errorf("time_based filter requires start_time or end_time")
	}

	var start, end time.Time
	var err error

	if rule.Conditions.StartTime != "" {
		start, err = time.Parse(time.RFC3339, rule.Conditions.StartTime)
		if err != nil {
			return nil, fmt.Errorf("invalid start_time: %w", err)
		}
	}

	if rule.Conditions.EndTime != "" {
		end, err = time.Parse(time.RFC3339, rule.Conditions.EndTime)
		if err != nil {
			return nil, fmt.Errorf("invalid end_time: %w", err)
		}
	}

	return func(event *domain.CollectorEvent) bool {
		if rule.Conditions.StartTime != "" && event.Timestamp.Before(start) {
			return false
		}
		if rule.Conditions.EndTime != "" && event.Timestamp.After(end) {
			return false
		}
		return true
	}, nil
}
