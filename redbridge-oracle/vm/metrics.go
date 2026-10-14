package vm

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds Prometheus counters and histograms for observability.
type Metrics struct {
	blocksBuilt        prometheus.Counter
	blocksAccepted     prometheus.Counter
	blocksRejected     prometheus.Counter
	zcashFetchDuration prometheus.Histogram
	duplicateRequests  prometheus.Counter
	apiRequests        *prometheus.CounterVec
}

// NewMetrics creates and registers all VM metrics with the given registerer.
// If reg is nil, metrics are created but not registered (useful in tests).
func NewMetrics(reg prometheus.Registerer) (*Metrics, error) {
	m := &Metrics{
		blocksBuilt: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "zcash_bridge_blocks_built_total",
			Help: "Total number of blocks built by this node.",
		}),
		blocksAccepted: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "zcash_bridge_blocks_accepted_total",
			Help: "Total number of blocks accepted via consensus.",
		}),
		blocksRejected: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "zcash_bridge_blocks_rejected_total",
			Help: "Total number of blocks rejected via consensus.",
		}),
		zcashFetchDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "zcash_bridge_zcash_fetch_duration_seconds",
			Help:    "Latency of Zcash RPC block fetches.",
			Buckets: prometheus.DefBuckets,
		}),
		duplicateRequests: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "zcash_bridge_duplicate_requests_total",
			Help: "Number of API requests for already-processed Zcash heights.",
		}),
		apiRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "zcash_bridge_api_requests_total",
			Help: "Total API requests by method and status.",
		}, []string{"method", "status"}),
	}

	if reg == nil {
		return m, nil
	}

	collectors := []prometheus.Collector{
		m.blocksBuilt,
		m.blocksAccepted,
		m.blocksRejected,
		m.zcashFetchDuration,
		m.duplicateRequests,
		m.apiRequests,
	}
	for _, c := range collectors {
		if err := reg.Register(c); err != nil {
			return nil, err
		}
	}
	return m, nil
}
