package bpf_common

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/cilium/ebpf"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// FilterType represents the type of filter
type FilterType uint32

const (
	FilterTypePID       FilterType = 0x01
	FilterTypeNamespace FilterType = 0x02
	FilterTypeNetwork   FilterType = 0x04
	FilterTypeCgroup    FilterType = 0x08
	FilterTypeUID       FilterType = 0x10
	FilterTypeComm      FilterType = 0x20
)

// FilterMode represents the filter mode
type FilterMode uint32

const (
	FilterModeAllow FilterMode = 0 // Allowlist mode
	FilterModeDeny  FilterMode = 1 // Denylist mode
)

// FilterConfig represents the filter configuration
type FilterConfig struct {
	EnabledFilters    uint32  `json:"enabled_filters"`
	FilterMode        uint32  `json:"filter_mode"`
	SampleRate        uint32  `json:"sample_rate"`
	BatchSize         uint32  `json:"batch_size"`
	RateLimit         uint64  `json:"rate_limit"`
	LastBatchNs       uint64  `json:"last_batch_ns"`
	CurrentBatchCount uint32  `json:"current_batch_count"`
}

// NetworkFilter represents a network filter rule
type NetworkFilter struct {
	Addr       [4]uint32 `json:"addr"`
	Port       uint16    `json:"port"`
	IPVersion  uint8     `json:"ip_version"`
	Protocol   uint8     `json:"protocol"`
	PrefixLen  uint32    `json:"prefix_len"`
}

// FilterManager manages dynamic eBPF filters
type FilterManager struct {
	mu     sync.RWMutex
	logger *zap.Logger
	
	// OTEL instrumentation
	meter             metric.Meter
	filterUpdates     metric.Int64Counter
	filterHits        metric.Int64Counter
	filterMisses      metric.Int64Counter
	activeFilters     metric.Int64UpDownCounter
	
	// eBPF maps
	pidFilterMap      *ebpf.Map
	nsFilterMap       *ebpf.Map
	netFilterMap      *ebpf.Map
	configMap         *ebpf.Map
	
	// Current configuration
	config            *FilterConfig
	
	// Filter entries
	pidFilters        map[uint32]bool
	namespaceFilters  map[uint64]bool
	networkFilters    map[string]*NetworkFilter
	
	// Statistics
	stats             *FilterStatistics
}

// FilterStatistics tracks filter performance
type FilterStatistics struct {
	PIDFilterHits       uint64    `json:"pid_filter_hits"`
	PIDFilterMisses     uint64    `json:"pid_filter_misses"`
	NamespaceFilterHits uint64    `json:"namespace_filter_hits"`
	NamespaceFilterMisses uint64  `json:"namespace_filter_misses"`
	NetworkFilterHits   uint64    `json:"network_filter_hits"`
	NetworkFilterMisses uint64    `json:"network_filter_misses"`
	TotalFiltered       uint64    `json:"total_filtered"`
	TotalPassed         uint64    `json:"total_passed"`
	LastUpdate          time.Time `json:"last_update"`
}

// NewFilterManager creates a new filter manager
func NewFilterManager(logger *zap.Logger, pidMap, nsMap, netMap, configMap *ebpf.Map) (*FilterManager, error) {
	if logger == nil {
		var err error
		logger, err = zap.NewProduction()
		if err != nil {
			return nil, fmt.Errorf("failed to create logger: %w", err)
		}
	}
	
	meter := otel.Meter("tapio.bpf.filters")
	
	filterUpdates, err := meter.Int64Counter(
		"bpf_filter_updates_total",
		metric.WithDescription("Total filter updates"),
	)
	if err != nil {
		logger.Warn("Failed to create filter_updates metric", zap.Error(err))
	}
	
	filterHits, err := meter.Int64Counter(
		"bpf_filter_hits_total",
		metric.WithDescription("Total filter hits"),
	)
	if err != nil {
		logger.Warn("Failed to create filter_hits metric", zap.Error(err))
	}
	
	filterMisses, err := meter.Int64Counter(
		"bpf_filter_misses_total",
		metric.WithDescription("Total filter misses"),
	)
	if err != nil {
		logger.Warn("Failed to create filter_misses metric", zap.Error(err))
	}
	
	activeFilters, err := meter.Int64UpDownCounter(
		"bpf_active_filters",
		metric.WithDescription("Number of active filters"),
	)
	if err != nil {
		logger.Warn("Failed to create active_filters metric", zap.Error(err))
	}
	
	fm := &FilterManager{
		logger:           logger,
		meter:            meter,
		filterUpdates:    filterUpdates,
		filterHits:       filterHits,
		filterMisses:     filterMisses,
		activeFilters:    activeFilters,
		pidFilterMap:     pidMap,
		nsFilterMap:      nsMap,
		netFilterMap:     netMap,
		configMap:        configMap,
		pidFilters:       make(map[uint32]bool),
		namespaceFilters: make(map[uint64]bool),
		networkFilters:   make(map[string]*NetworkFilter),
		stats:            &FilterStatistics{LastUpdate: time.Now()},
		config:           &FilterConfig{},
	}
	
	// Load initial configuration
	if err := fm.loadConfig(); err != nil {
		logger.Warn("Failed to load initial filter config", zap.Error(err))
	}
	
	return fm, nil
}

// loadConfig loads the current configuration from the eBPF map
func (fm *FilterManager) loadConfig() error {
	if fm.configMap == nil {
		return nil
	}
	
	key := uint32(0)
	config := &FilterConfig{}
	
	if err := fm.configMap.Lookup(key, config); err != nil {
		return fmt.Errorf("failed to lookup filter config: %w", err)
	}
	
	fm.config = config
	return nil
}

// UpdateConfig updates the filter configuration
func (fm *FilterManager) UpdateConfig(config *FilterConfig) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	
	if fm.configMap == nil {
		return fmt.Errorf("config map not initialized")
	}
	
	key := uint32(0)
	if err := fm.configMap.Update(key, config, ebpf.UpdateAny); err != nil {
		return fmt.Errorf("failed to update filter config: %w", err)
	}
	
	fm.config = config
	
	if fm.filterUpdates != nil {
		fm.filterUpdates.Add(context.Background(), 1, metric.WithAttributes(
			attribute.String("update_type", "config"),
		))
	}
	
	fm.logger.Info("Updated filter configuration",
		zap.Uint32("enabled_filters", config.EnabledFilters),
		zap.Uint32("filter_mode", config.FilterMode),
		zap.Uint32("sample_rate", config.SampleRate),
	)
	
	return nil
}

// AddPIDFilter adds a PID to the filter
func (fm *FilterManager) AddPIDFilter(pid uint32, allow bool) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	
	if fm.pidFilterMap == nil {
		return fmt.Errorf("PID filter map not initialized")
	}
	
	value := uint8(0)
	if allow {
		value = 1
	}
	
	if err := fm.pidFilterMap.Update(pid, value, ebpf.UpdateAny); err != nil {
		return fmt.Errorf("failed to add PID filter: %w", err)
	}
	
	fm.pidFilters[pid] = allow
	
	if fm.filterUpdates != nil {
		fm.filterUpdates.Add(context.Background(), 1, metric.WithAttributes(
			attribute.String("update_type", "pid"),
			attribute.String("action", "add"),
		))
	}
	
	if fm.activeFilters != nil {
		fm.activeFilters.Add(context.Background(), 1, metric.WithAttributes(
			attribute.String("filter_type", "pid"),
		))
	}
	
	fm.logger.Debug("Added PID filter",
		zap.Uint32("pid", pid),
		zap.Bool("allow", allow),
	)
	
	return nil
}

// RemovePIDFilter removes a PID from the filter
func (fm *FilterManager) RemovePIDFilter(pid uint32) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	
	if fm.pidFilterMap == nil {
		return fmt.Errorf("PID filter map not initialized")
	}
	
	if err := fm.pidFilterMap.Delete(pid); err != nil {
		return fmt.Errorf("failed to remove PID filter: %w", err)
	}
	
	delete(fm.pidFilters, pid)
	
	if fm.filterUpdates != nil {
		fm.filterUpdates.Add(context.Background(), 1, metric.WithAttributes(
			attribute.String("update_type", "pid"),
			attribute.String("action", "remove"),
		))
	}
	
	if fm.activeFilters != nil {
		fm.activeFilters.Add(context.Background(), -1, metric.WithAttributes(
			attribute.String("filter_type", "pid"),
		))
	}
	
	fm.logger.Debug("Removed PID filter", zap.Uint32("pid", pid))
	
	return nil
}

// AddNamespaceFilter adds a namespace to the filter
func (fm *FilterManager) AddNamespaceFilter(nsID uint64, allow bool) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	
	if fm.nsFilterMap == nil {
		return fmt.Errorf("namespace filter map not initialized")
	}
	
	value := uint8(0)
	if allow {
		value = 1
	}
	
	if err := fm.nsFilterMap.Update(nsID, value, ebpf.UpdateAny); err != nil {
		return fmt.Errorf("failed to add namespace filter: %w", err)
	}
	
	fm.namespaceFilters[nsID] = allow
	
	if fm.filterUpdates != nil {
		fm.filterUpdates.Add(context.Background(), 1, metric.WithAttributes(
			attribute.String("update_type", "namespace"),
			attribute.String("action", "add"),
		))
	}
	
	if fm.activeFilters != nil {
		fm.activeFilters.Add(context.Background(), 1, metric.WithAttributes(
			attribute.String("filter_type", "namespace"),
		))
	}
	
	fm.logger.Debug("Added namespace filter",
		zap.Uint64("namespace_id", nsID),
		zap.Bool("allow", allow),
	)
	
	return nil
}

// RemoveNamespaceFilter removes a namespace from the filter
func (fm *FilterManager) RemoveNamespaceFilter(nsID uint64) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	
	if fm.nsFilterMap == nil {
		return fmt.Errorf("namespace filter map not initialized")
	}
	
	if err := fm.nsFilterMap.Delete(nsID); err != nil {
		return fmt.Errorf("failed to remove namespace filter: %w", err)
	}
	
	delete(fm.namespaceFilters, nsID)
	
	if fm.filterUpdates != nil {
		fm.filterUpdates.Add(context.Background(), 1, metric.WithAttributes(
			attribute.String("update_type", "namespace"),
			attribute.String("action", "remove"),
		))
	}
	
	if fm.activeFilters != nil {
		fm.activeFilters.Add(context.Background(), -1, metric.WithAttributes(
			attribute.String("filter_type", "namespace"),
		))
	}
	
	fm.logger.Debug("Removed namespace filter", zap.Uint64("namespace_id", nsID))
	
	return nil
}

// AddNetworkFilter adds a network filter rule
func (fm *FilterManager) AddNetworkFilter(ip string, port uint16, protocol uint8, allow bool) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	
	if fm.netFilterMap == nil {
		return fmt.Errorf("network filter map not initialized")
	}
	
	// Parse IP address
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return fmt.Errorf("invalid IP address: %s", ip)
	}
	
	filter := &NetworkFilter{
		Port:     port,
		Protocol: protocol,
	}
	
	// Determine IP version and convert to bytes
	if ipv4 := parsedIP.To4(); ipv4 != nil {
		filter.IPVersion = 4
		// Convert IPv4 bytes to uint32
		filter.Addr[0] = uint32(ipv4[0])<<24 | uint32(ipv4[1])<<16 | uint32(ipv4[2])<<8 | uint32(ipv4[3])
	} else {
		filter.IPVersion = 6
		ipv6 := parsedIP.To16()
		for i := 0; i < 4; i++ {
			filter.Addr[i] = uint32(ipv6[i*4])<<24 | uint32(ipv6[i*4+1])<<16 | 
			               uint32(ipv6[i*4+2])<<8 | uint32(ipv6[i*4+3])
		}
	}
	
	value := uint8(0)
	if allow {
		value = 1
	}
	
	if err := fm.netFilterMap.Update(filter, value, ebpf.UpdateAny); err != nil {
		return fmt.Errorf("failed to add network filter: %w", err)
	}
	
	key := fmt.Sprintf("%s:%d:%d", ip, port, protocol)
	fm.networkFilters[key] = filter
	
	if fm.filterUpdates != nil {
		fm.filterUpdates.Add(context.Background(), 1, metric.WithAttributes(
			attribute.String("update_type", "network"),
			attribute.String("action", "add"),
		))
	}
	
	if fm.activeFilters != nil {
		fm.activeFilters.Add(context.Background(), 1, metric.WithAttributes(
			attribute.String("filter_type", "network"),
		))
	}
	
	fm.logger.Debug("Added network filter",
		zap.String("ip", ip),
		zap.Uint16("port", port),
		zap.Uint8("protocol", protocol),
		zap.Bool("allow", allow),
	)
	
	return nil
}

// RemoveNetworkFilter removes a network filter rule
func (fm *FilterManager) RemoveNetworkFilter(ip string, port uint16, protocol uint8) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	
	if fm.netFilterMap == nil {
		return fmt.Errorf("network filter map not initialized")
	}
	
	key := fmt.Sprintf("%s:%d:%d", ip, port, protocol)
	filter, exists := fm.networkFilters[key]
	if !exists {
		return fmt.Errorf("network filter not found: %s", key)
	}
	
	if err := fm.netFilterMap.Delete(filter); err != nil {
		return fmt.Errorf("failed to remove network filter: %w", err)
	}
	
	delete(fm.networkFilters, key)
	
	if fm.filterUpdates != nil {
		fm.filterUpdates.Add(context.Background(), 1, metric.WithAttributes(
			attribute.String("update_type", "network"),
			attribute.String("action", "remove"),
		))
	}
	
	if fm.activeFilters != nil {
		fm.activeFilters.Add(context.Background(), -1, metric.WithAttributes(
			attribute.String("filter_type", "network"),
		))
	}
	
	fm.logger.Debug("Removed network filter",
		zap.String("ip", ip),
		zap.Uint16("port", port),
		zap.Uint8("protocol", protocol),
	)
	
	return nil
}

// ClearAllFilters removes all filters
func (fm *FilterManager) ClearAllFilters() error {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	
	// Clear PID filters
	for pid := range fm.pidFilters {
		if fm.pidFilterMap != nil {
			fm.pidFilterMap.Delete(pid)
		}
	}
	fm.pidFilters = make(map[uint32]bool)
	
	// Clear namespace filters
	for nsID := range fm.namespaceFilters {
		if fm.nsFilterMap != nil {
			fm.nsFilterMap.Delete(nsID)
		}
	}
	fm.namespaceFilters = make(map[uint64]bool)
	
	// Clear network filters
	for _, filter := range fm.networkFilters {
		if fm.netFilterMap != nil {
			fm.netFilterMap.Delete(filter)
		}
	}
	fm.networkFilters = make(map[string]*NetworkFilter)
	
	if fm.activeFilters != nil {
		// Reset counter to 0
		fm.activeFilters.Add(context.Background(), -int64(len(fm.pidFilters)+len(fm.namespaceFilters)+len(fm.networkFilters)),
			metric.WithAttributes(attribute.String("filter_type", "all")))
	}
	
	fm.logger.Info("Cleared all filters")
	
	return nil
}

// GetStatistics returns current filter statistics
func (fm *FilterManager) GetStatistics() *FilterStatistics {
	fm.mu.RLock()
	defer fm.mu.RUnlock()
	
	// Create a copy
	stats := *fm.stats
	stats.LastUpdate = time.Now()
	
	return &stats
}

// UpdateStatistics updates filter hit/miss statistics
func (fm *FilterManager) UpdateStatistics(filterType FilterType, hit bool) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	
	ctx := context.Background()
	
	switch filterType {
	case FilterTypePID:
		if hit {
			fm.stats.PIDFilterHits++
			if fm.filterHits != nil {
				fm.filterHits.Add(ctx, 1, metric.WithAttributes(
					attribute.String("filter_type", "pid"),
				))
			}
		} else {
			fm.stats.PIDFilterMisses++
			if fm.filterMisses != nil {
				fm.filterMisses.Add(ctx, 1, metric.WithAttributes(
					attribute.String("filter_type", "pid"),
				))
			}
		}
	case FilterTypeNamespace:
		if hit {
			fm.stats.NamespaceFilterHits++
			if fm.filterHits != nil {
				fm.filterHits.Add(ctx, 1, metric.WithAttributes(
					attribute.String("filter_type", "namespace"),
				))
			}
		} else {
			fm.stats.NamespaceFilterMisses++
			if fm.filterMisses != nil {
				fm.filterMisses.Add(ctx, 1, metric.WithAttributes(
					attribute.String("filter_type", "namespace"),
				))
			}
		}
	case FilterTypeNetwork:
		if hit {
			fm.stats.NetworkFilterHits++
			if fm.filterHits != nil {
				fm.filterHits.Add(ctx, 1, metric.WithAttributes(
					attribute.String("filter_type", "network"),
				))
			}
		} else {
			fm.stats.NetworkFilterMisses++
			if fm.filterMisses != nil {
				fm.filterMisses.Add(ctx, 1, metric.WithAttributes(
					attribute.String("filter_type", "network"),
				))
			}
		}
	}
	
	if hit {
		fm.stats.TotalPassed++
	} else {
		fm.stats.TotalFiltered++
	}
	
	fm.stats.LastUpdate = time.Now()
}

// SetSamplingRate sets the sampling rate (0-100)
func (fm *FilterManager) SetSamplingRate(rate uint32) error {
	if rate > 100 {
		return fmt.Errorf("sampling rate must be between 0 and 100")
	}
	
	fm.mu.Lock()
	defer fm.mu.Unlock()
	
	fm.config.SampleRate = rate
	return fm.UpdateConfig(fm.config)
}

// SetRateLimit sets the rate limit in events per second
func (fm *FilterManager) SetRateLimit(limit uint64) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	
	fm.config.RateLimit = limit
	return fm.UpdateConfig(fm.config)
}

// EnableFilterType enables a specific filter type
func (fm *FilterManager) EnableFilterType(filterType FilterType) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	
	fm.config.EnabledFilters |= uint32(filterType)
	return fm.UpdateConfig(fm.config)
}

// DisableFilterType disables a specific filter type
func (fm *FilterManager) DisableFilterType(filterType FilterType) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	
	fm.config.EnabledFilters &= ^uint32(filterType)
	return fm.UpdateConfig(fm.config)
}

// SetFilterMode sets the filter mode (allow or deny)
func (fm *FilterManager) SetFilterMode(mode FilterMode) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	
	fm.config.FilterMode = uint32(mode)
	return fm.UpdateConfig(fm.config)
}