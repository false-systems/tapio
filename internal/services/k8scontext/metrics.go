package k8scontext

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// CacheSize tracks the number of items in the cache.
	CacheSize = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "tapio",
			Subsystem: "k8scontext",
			Name:      "cache_size",
			Help:      "Number of items in k8scontext cache",
		},
		[]string{"type"}, // pod, service, tombstone
	)

	// LookupTotal tracks lookup attempts by type and result.
	LookupTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "tapio",
			Subsystem: "k8scontext",
			Name:      "lookups_total",
			Help:      "Total lookups by type and result",
		},
		[]string{"type", "found"}, // pod_by_ip/true, service_by_ip/false
	)

	// InformerEvents tracks informer events by type and action.
	InformerEvents = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "tapio",
			Subsystem: "k8scontext",
			Name:      "informer_events_total",
			Help:      "Informer events by type and action",
		},
		[]string{"type", "action"}, // pod/add, service/delete
	)

	// InformerSynced indicates whether informers are synced.
	InformerSynced = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "tapio",
			Subsystem: "k8scontext",
			Name:      "informer_synced",
			Help:      "1 if informers are synced, 0 otherwise",
		},
	)
)

// RecordLookup records a lookup attempt.
func RecordLookup(lookupType string, found bool) {
	foundStr := "false"
	if found {
		foundStr = "true"
	}
	LookupTotal.WithLabelValues(lookupType, foundStr).Inc()
}

// RecordInformerEvent records an informer event.
func RecordInformerEvent(resourceType, action string) {
	InformerEvents.WithLabelValues(resourceType, action).Inc()
}

// UpdateCacheSize updates the cache size gauge.
func UpdateCacheSize(resourceType string, size int) {
	CacheSize.WithLabelValues(resourceType).Set(float64(size))
}
