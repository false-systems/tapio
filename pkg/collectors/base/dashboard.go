package base

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DashboardExtension allows collectors to add custom panels to the base dashboard
type DashboardExtension struct {
	// Title for the row containing custom panels
	RowTitle string

	// Custom panels specific to this collector
	Panels []Panel

	// Additional variables needed for this collector
	Variables []Variable

	// Tags to add for dashboard discovery
	Tags []string
}

// Panel represents a Grafana panel configuration
type Panel struct {
	Title       string                 `json:"title"`
	Type        string                 `json:"type"`
	Description string                 `json:"description,omitempty"`
	Targets     []Target               `json:"targets"`
	GridPos     GridPosition           `json:"gridPos"`
	Options     map[string]interface{} `json:"options,omitempty"`
	FieldConfig map[string]interface{} `json:"fieldConfig,omitempty"`
}

// Target represents a Prometheus query target
type Target struct {
	Expr         string `json:"expr"`
	LegendFormat string `json:"legendFormat,omitempty"`
	RefID        string `json:"refId"`
}

// GridPosition defines panel placement
type GridPosition struct {
	H int `json:"h"` // Height
	W int `json:"w"` // Width
	X int `json:"x"` // X position
	Y int `json:"y"` // Y position
}

// Variable represents a dashboard template variable
type Variable struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Type        string `json:"type"`
	Query       string `json:"query,omitempty"`
	Regex       string `json:"regex,omitempty"`
	Description string `json:"description,omitempty"`
}

// DashboardMetadata provides information about available dashboards
type DashboardMetadata struct {
	CollectorName     string   `json:"collector_name"`
	BaseDashboard     string   `json:"base_dashboard_uid"`
	ExtendedDashboard string   `json:"extended_dashboard_uid,omitempty"`
	Description       string   `json:"description"`
	Tags              []string `json:"tags"`
}

// GetDashboardMetadata returns metadata for dashboard discovery
func (bc *BaseCollector) GetDashboardMetadata() DashboardMetadata {
	return DashboardMetadata{
		CollectorName: bc.name,
		BaseDashboard: "tapio-collector-base",
		Description:   fmt.Sprintf("Base metrics for %s collector", bc.name),
		Tags:          []string{"tapio", bc.name, "collector"},
	}
}

// GenerateDashboardConfig creates dashboard configuration for this collector
// This can be used to generate collector-specific extended dashboards
func GenerateDashboardConfig(collectorName string, extension *DashboardExtension) ([]byte, error) {
	if extension == nil {
		// No extension, just return metadata pointing to base dashboard
		metadata := DashboardMetadata{
			CollectorName: collectorName,
			BaseDashboard: "tapio-collector-base",
			Description:   fmt.Sprintf("Use base dashboard with collector_name=%s", collectorName),
			Tags:          []string{"tapio", collectorName},
		}
		return json.MarshalIndent(metadata, "", "  ")
	}

	// Generate extended dashboard JSON
	dashboard := map[string]interface{}{
		"uid":          fmt.Sprintf("tapio-%s-extended", strings.ReplaceAll(collectorName, "_", "-")),
		"title":        fmt.Sprintf("Tapio %s Collector - Extended Metrics", formatCollectorName(collectorName)),
		"description":  fmt.Sprintf("Extended metrics and visualizations for %s collector", collectorName),
		"tags":         append([]string{"tapio", "tapio-extended", collectorName}, extension.Tags...),
		"style":        "dark",
		"timezone":     "browser",
		"editable":     true,
		"graphTooltip": 1,
		"panels":       generatePanelJSON(extension.Panels),
		"templating": map[string]interface{}{
			"list": generateVariableJSON(extension.Variables),
		},
		"links": []map[string]interface{}{
			{
				"title":       "Base Metrics",
				"type":        "link",
				"url":         fmt.Sprintf("/d/tapio-collector-base/tapio-collector-base-metrics?var-collector_name=%s", collectorName),
				"tooltip":     "View base collector metrics",
				"icon":        "external link",
				"targetBlank": false,
				"keepTime":    true,
			},
		},
		"time": map[string]string{
			"from": "now-30m",
			"to":   "now",
		},
		"refresh": "10s",
	}

	return json.MarshalIndent(dashboard, "", "  ")
}

// Helper function to format collector names for display
func formatCollectorName(name string) string {
	// Convert snake_case to Title Case
	parts := strings.Split(name, "_")
	for i, part := range parts {
		if part == "ebpf" {
			parts[i] = "eBPF"
		} else if part == "api" {
			parts[i] = "API"
		} else if part == "io" {
			parts[i] = "IO"
		} else {
			parts[i] = strings.Title(part)
		}
	}
	return strings.Join(parts, " ")
}

// generatePanelJSON converts Panel structs to Grafana JSON format
func generatePanelJSON(panels []Panel) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(panels)+1)

	// Add a row header first
	result = append(result, map[string]interface{}{
		"collapsed": false,
		"gridPos": map[string]int{
			"h": 1,
			"w": 24,
			"x": 0,
			"y": 0,
		},
		"id":    200,
		"title": "Extended Metrics",
		"type":  "row",
	})

	// Add custom panels
	for i, panel := range panels {
		panelJSON := map[string]interface{}{
			"id":          201 + i,
			"title":       panel.Title,
			"type":        panel.Type,
			"description": panel.Description,
			"gridPos":     panel.GridPos,
			"targets":     panel.Targets,
		}

		if panel.Options != nil {
			panelJSON["options"] = panel.Options
		}

		if panel.FieldConfig != nil {
			panelJSON["fieldConfig"] = panel.FieldConfig
		}

		result = append(result, panelJSON)
	}

	return result
}

// generateVariableJSON converts Variable structs to Grafana JSON format
func generateVariableJSON(variables []Variable) []map[string]interface{} {
	// Always include datasource variable
	result := []map[string]interface{}{
		{
			"name":  "datasource",
			"type":  "datasource",
			"query": "prometheus",
			"label": "Datasource",
			"hide":  0,
		},
	}

	// Add custom variables
	for _, v := range variables {
		varJSON := map[string]interface{}{
			"name":        v.Name,
			"label":       v.Label,
			"type":        v.Type,
			"description": v.Description,
			"hide":        0,
		}

		if v.Query != "" {
			varJSON["query"] = v.Query
		}

		if v.Regex != "" {
			varJSON["regex"] = v.Regex
		}

		result = append(result, varJSON)
	}

	return result
}

// ExampleNetworkExtension shows how a collector would define its extended dashboard
func ExampleNetworkExtension() *DashboardExtension {
	return &DashboardExtension{
		RowTitle: "Network Protocol Breakdown",
		Panels: []Panel{
			{
				Title:       "Protocol Distribution",
				Type:        "piechart",
				Description: "Breakdown of network traffic by protocol",
				GridPos:     GridPosition{H: 8, W: 12, X: 0, Y: 1},
				Targets: []Target{
					{
						Expr:         "sum by (protocol) (rate(network_events_by_protocol_total[5m]))",
						LegendFormat: "{{protocol}}",
						RefID:        "A",
					},
				},
			},
			{
				Title:       "L7 Parsing Success Rate",
				Type:        "gauge",
				Description: "Percentage of successfully parsed L7 protocols",
				GridPos:     GridPosition{H: 8, W: 12, X: 12, Y: 1},
				Targets: []Target{
					{
						Expr:         "(sum(rate(network_l7_parsed_total[5m])) / sum(rate(network_events_processed_total[5m]))) * 100",
						LegendFormat: "L7 Parse Rate",
						RefID:        "A",
					},
				},
			},
		},
		Variables: []Variable{
			{
				Name:        "protocol",
				Label:       "Protocol",
				Type:        "query",
				Query:       "label_values(network_events_by_protocol_total, protocol)",
				Description: "Filter by network protocol",
			},
		},
		Tags: []string{"network", "l7", "protocols"},
	}
}
