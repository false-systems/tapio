package exports

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"sync"
)

// Router implements the ExportRouter interface
type Router struct {
	routes      map[string]*ExportRoute
	routeOrder  []string // Maintains priority order
	mutex       sync.RWMutex
	matchers    map[string]*compiledMatcher
}

// compiledMatcher holds compiled patterns for efficient matching
type compiledMatcher struct {
	route       *ExportRoute
	tagRegexps  map[string]*regexp.Regexp
	metaRegexps map[string]*regexp.Regexp
	expression  *regexp.Regexp
}

// NewRouter creates a new export router
func NewRouter() *Router {
	return &Router{
		routes:   make(map[string]*ExportRoute),
		matchers: make(map[string]*compiledMatcher),
	}
}

// AddRoute adds a new route to the router
func (r *Router) AddRoute(route *ExportRoute) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if route.ID == "" {
		return fmt.Errorf("route ID cannot be empty")
	}

	if _, exists := r.routes[route.ID]; exists {
		return fmt.Errorf("route %s already exists", route.ID)
	}

	// Compile patterns
	matcher, err := r.compileMatcher(route)
	if err != nil {
		return fmt.Errorf("failed to compile matcher for route %s: %w", route.ID, err)
	}

	r.routes[route.ID] = route
	r.matchers[route.ID] = matcher

	// Update route order based on priority
	r.updateRouteOrder()

	return nil
}

// RemoveRoute removes a route from the router
func (r *Router) RemoveRoute(routeID string) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if _, exists := r.routes[routeID]; !exists {
		return fmt.Errorf("route %s not found", routeID)
	}

	delete(r.routes, routeID)
	delete(r.matchers, routeID)

	// Update route order
	r.updateRouteOrder()

	return nil
}

// GetRoute retrieves a route by ID
func (r *Router) GetRoute(routeID string) (*ExportRoute, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	route, exists := r.routes[routeID]
	if !exists {
		return nil, fmt.Errorf("route %s not found", routeID)
	}

	return route, nil
}

// ListRoutes returns all routes in priority order
func (r *Router) ListRoutes() []*ExportRoute {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	routes := make([]*ExportRoute, 0, len(r.routeOrder))
	for _, id := range r.routeOrder {
		if route, exists := r.routes[id]; exists {
			routes = append(routes, route)
		}
	}

	return routes
}

// RouteData routes data to appropriate plugins based on rules
func (r *Router) RouteData(ctx context.Context, data ExportData) ([]*RouteDecision, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	decisions := make([]*RouteDecision, 0)

	// Check each route in priority order
	for _, routeID := range r.routeOrder {
		route, exists := r.routes[routeID]
		if !exists || !route.Enabled {
			continue
		}

		matcher, exists := r.matchers[routeID]
		if !exists {
			continue
		}

		// Test if route matches
		if r.matchRoute(matcher, data) {
			decision := &RouteDecision{
				RouteID:    route.ID,
				PluginName: route.PluginName,
				Priority:   route.Priority,
				Matched:    true,
			}
			decisions = append(decisions, decision)
		}
	}

	return decisions, nil
}

// TestRoute tests if a route would match given data
func (r *Router) TestRoute(route *ExportRoute, data ExportData) (bool, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	// Compile matcher for testing
	matcher, err := r.compileMatcher(route)
	if err != nil {
		return false, err
	}

	return r.matchRoute(matcher, data), nil
}

// compileMatcher compiles a route's patterns for efficient matching
func (r *Router) compileMatcher(route *ExportRoute) (*compiledMatcher, error) {
	matcher := &compiledMatcher{
		route:       route,
		tagRegexps:  make(map[string]*regexp.Regexp),
		metaRegexps: make(map[string]*regexp.Regexp),
	}

	if route.Pattern == nil {
		return matcher, nil
	}

	// Compile tag patterns
	for key, pattern := range route.Pattern.Tags {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid tag pattern %s: %w", key, err)
		}
		matcher.tagRegexps[key] = re
	}

	// Compile metadata patterns
	for key, pattern := range route.Pattern.Metadata {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid metadata pattern %s: %w", key, err)
		}
		matcher.metaRegexps[key] = re
	}

	// Compile expression pattern
	if route.Pattern.Expression != "" {
		re, err := regexp.Compile(route.Pattern.Expression)
		if err != nil {
			return nil, fmt.Errorf("invalid expression pattern: %w", err)
		}
		matcher.expression = re
	}

	return matcher, nil
}

// matchRoute checks if data matches a route
func (r *Router) matchRoute(matcher *compiledMatcher, data ExportData) bool {
	if matcher.route.Pattern == nil {
		return true // No pattern means match all
	}

	pattern := matcher.route.Pattern

	// Check data type
	if len(pattern.DataType) > 0 {
		matched := false
		for _, dt := range pattern.DataType {
			if dt == data.Type {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Check format
	if len(pattern.Format) > 0 {
		matched := false
		for _, f := range pattern.Format {
			if f == data.Format {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Check tags
	for key, re := range matcher.tagRegexps {
		value, exists := data.Tags[key]
		if !exists || !re.MatchString(value) {
			return false
		}
	}

	// Check metadata
	for key, re := range matcher.metaRegexps {
		value, exists := data.Metadata[key]
		if !exists {
			return false
		}
		
		// Convert to string for matching
		strValue := fmt.Sprintf("%v", value)
		if !re.MatchString(strValue) {
			return false
		}
	}

	// Check expression
	if matcher.expression != nil {
		// Build a string representation of the data for expression matching
		dataStr := fmt.Sprintf("%s:%s", data.Type, data.Format)
		if !matcher.expression.MatchString(dataStr) {
			return false
		}
	}

	return true
}

// updateRouteOrder updates the route order based on priority
func (r *Router) updateRouteOrder() {
	// Create a slice of route IDs
	ids := make([]string, 0, len(r.routes))
	for id := range r.routes {
		ids = append(ids, id)
	}

	// Sort by priority (higher priority first)
	sort.Slice(ids, func(i, j int) bool {
		routeI := r.routes[ids[i]]
		routeJ := r.routes[ids[j]]
		
		// Higher priority comes first
		if routeI.Priority != routeJ.Priority {
			return routeI.Priority > routeJ.Priority
		}
		
		// If same priority, sort by ID for consistency
		return ids[i] < ids[j]
	})

	r.routeOrder = ids
}

// RouteBuilder provides a fluent interface for building routes
type RouteBuilder struct {
	route *ExportRoute
}

// NewRouteBuilder creates a new route builder
func NewRouteBuilder() *RouteBuilder {
	return &RouteBuilder{
		route: &ExportRoute{
			Pattern:  &RoutePattern{},
			Enabled:  true,
			Priority: 50, // Default priority
		},
	}
}

// WithID sets the route ID
func (rb *RouteBuilder) WithID(id string) *RouteBuilder {
	rb.route.ID = id
	return rb
}

// WithName sets the route name
func (rb *RouteBuilder) WithName(name string) *RouteBuilder {
	rb.route.Name = name
	return rb
}

// WithPlugin sets the target plugin
func (rb *RouteBuilder) WithPlugin(pluginName string) *RouteBuilder {
	rb.route.PluginName = pluginName
	return rb
}

// WithPriority sets the route priority
func (rb *RouteBuilder) WithPriority(priority int) *RouteBuilder {
	rb.route.Priority = priority
	return rb
}

// WithDataTypes sets the data types to match
func (rb *RouteBuilder) WithDataTypes(types ...DataType) *RouteBuilder {
	rb.route.Pattern.DataType = types
	return rb
}

// WithFormats sets the formats to match
func (rb *RouteBuilder) WithFormats(formats ...ExportFormat) *RouteBuilder {
	rb.route.Pattern.Format = formats
	return rb
}

// WithTag adds a tag pattern
func (rb *RouteBuilder) WithTag(key, pattern string) *RouteBuilder {
	if rb.route.Pattern.Tags == nil {
		rb.route.Pattern.Tags = make(map[string]string)
	}
	rb.route.Pattern.Tags[key] = pattern
	return rb
}

// WithMetadata adds a metadata pattern
func (rb *RouteBuilder) WithMetadata(key, pattern string) *RouteBuilder {
	if rb.route.Pattern.Metadata == nil {
		rb.route.Pattern.Metadata = make(map[string]string)
	}
	rb.route.Pattern.Metadata[key] = pattern
	return rb
}

// WithExpression sets an expression pattern
func (rb *RouteBuilder) WithExpression(expression string) *RouteBuilder {
	rb.route.Pattern.Expression = expression
	return rb
}

// WithTransform adds transformers
func (rb *RouteBuilder) WithTransform(transformers ...string) *RouteBuilder {
	rb.route.Transform = transformers
	return rb
}

// Build returns the constructed route
func (rb *RouteBuilder) Build() *ExportRoute {
	return rb.route
}

// RouteManager provides higher-level route management
type RouteManager struct {
	router *Router
	mutex  sync.Mutex
}

// NewRouteManager creates a new route manager
func NewRouteManager(router *Router) *RouteManager {
	return &RouteManager{
		router: router,
	}
}

// AddDefaultRoutes adds common default routes
func (rm *RouteManager) AddDefaultRoutes() error {
	defaultRoutes := []*ExportRoute{
		// Route all metrics to Prometheus
		NewRouteBuilder().
			WithID("metrics-to-prometheus").
			WithName("Metrics to Prometheus").
			WithPlugin("prometheus").
			WithDataTypes(DataTypeMetrics).
			WithFormats(FormatPrometheus).
			WithPriority(100).
			Build(),

		// Route all events to OTEL
		NewRouteBuilder().
			WithID("events-to-otel").
			WithName("Events to OpenTelemetry").
			WithPlugin("otel").
			WithDataTypes(DataTypeEvents).
			WithFormats(FormatOTEL).
			WithPriority(90).
			Build(),

		// Route drift reports to webhook
		NewRouteBuilder().
			WithID("drift-to-webhook").
			WithName("Drift Reports to Webhook").
			WithPlugin("webhook").
			WithDataTypes(DataTypeDriftReport).
			WithFormats(FormatWebhook).
			WithPriority(80).
			Build(),

		// Route critical events to multiple destinations
		NewRouteBuilder().
			WithID("critical-events").
			WithName("Critical Events Multi-Export").
			WithPlugin("multi-export").
			WithDataTypes(DataTypeEvents).
			WithTag("severity", "critical|high").
			WithPriority(100).
			Build(),
	}

	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	for _, route := range defaultRoutes {
		if err := rm.router.AddRoute(route); err != nil {
			return fmt.Errorf("failed to add default route %s: %w", route.ID, err)
		}
	}

	return nil
}

// EnableRoute enables a route
func (rm *RouteManager) EnableRoute(routeID string) error {
	route, err := rm.router.GetRoute(routeID)
	if err != nil {
		return err
	}

	route.Enabled = true
	return nil
}

// DisableRoute disables a route
func (rm *RouteManager) DisableRoute(routeID string) error {
	route, err := rm.router.GetRoute(routeID)
	if err != nil {
		return err
	}

	route.Enabled = false
	return nil
}

// UpdateRoutePriority updates a route's priority
func (rm *RouteManager) UpdateRoutePriority(routeID string, priority int) error {
	route, err := rm.router.GetRoute(routeID)
	if err != nil {
		return err
	}

	route.Priority = priority
	
	// Re-sort routes
	rm.router.mutex.Lock()
	rm.router.updateRouteOrder()
	rm.router.mutex.Unlock()
	
	return nil
}