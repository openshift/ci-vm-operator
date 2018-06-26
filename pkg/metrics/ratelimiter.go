package metrics

import (
	"fmt"
	"sync"
	"time"

	"k8s.io/client-go/util/flowcontrol"

	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	updatePeriod = 5 * time.Second
)

var (
	metricsLock        sync.Mutex
	rateLimiterMetrics = make(map[string]prometheus.Gauge)
)

func registerRateLimiterMetric(ownerName string) error {
	metricsLock.Lock()
	defer metricsLock.Unlock()

	if _, ok := rateLimiterMetrics[ownerName]; ok {
		glog.Errorf("metric for %v already registered", ownerName)
		return fmt.Errorf("metric for %v already registered", ownerName)
	}
	metric := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:      "rate_limiter_use",
		Subsystem: ownerName,
		Help:      fmt.Sprintf("A metric measuring the saturation of the rate limiter for %v", ownerName),
	})
	rateLimiterMetrics[ownerName] = metric
	if err := prometheus.Register(metric); err != nil {
		return fmt.Errorf("error registering rate limiter usage metric: %v", err)
	}
	return nil
}

// RegisterMetricAndTrackRateLimiterUsage registers a metric ownerName_rate_limiter_use in prometheus to track
// how much used rateLimiter is and starts a goroutine that updates this metric every updatePeriod
func RegisterMetricAndTrackRateLimiterUsage(ownerName string, rateLimiter flowcontrol.RateLimiter) error {
	err := registerRateLimiterMetric(ownerName)
	if err != nil {
		return err
	}
	// TODO: determine how to track rate limiter saturation
	// See discussion at https://go-review.googlesource.com/c/time/+/29958#message-4caffc11669cadd90e2da4c05122cfec50ea6a22
	// go wait.Until(func() {
	//   metricsLock.Lock()
	//   defer metricsLock.Unlock()
	//   rateLimiterMetrics[ownerName].metric.Set()
	// }, updatePeriod, rateLimiterMetrics[ownerName].stopCh)
	return nil
}
