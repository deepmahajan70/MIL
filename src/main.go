package main

import (
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// --- 1. Global Metrics ---

var (
	opsProcessed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mil_processed_messages_total",
		Help: "The total number of processed IoT messages",
	})
	routingLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "mil_routing_latency_milliseconds",
		Help:    "Latency of the trust-aware switchboard routing",
		Buckets: []float64{0.5, 1, 2, 5, 10, 20},
	})
	trustGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "mil_device_trust_score",
		Help: "Current trust score (sigma) per device",
	}, []string{"device_id"})
	thresholdGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "mil_system_threshold",
		Help: "Current adaptive security threshold",
	})
)

// --- 2. Core Logic ---

// UMO represents a Universal Message Object containing device data and behavior scores.
type UMO struct {
	SourceID      string
	Payload       float64
	BehaviorScore float64
}

// TrustEngine manages the reputation state and temporal penalties of devices.
type TrustEngine struct {
	mu            sync.RWMutex
	scores        map[string]float64
	lastMalicious map[string]int
	currentStep   int
}

// UpdateTrust calculates the new trust score using an asymmetric EWMA and stability hysteresis.
func (te *TrustEngine) UpdateTrust(id string, behaviorScore float64) float64 {
	te.mu.Lock()
	defer te.mu.Unlock()

	current, exists := te.scores[id]
	if !exists {
		current = 1.0
	}

	alpha := 0.1
	if behaviorScore < current {
		alpha = 0.4
	}

	updated := (alpha * behaviorScore) + ((1 - alpha) * current)

	if current > 0.7 && behaviorScore > 0.5 {
		updated = (0.8 * current) + (0.2 * behaviorScore)
	}

	if te.currentStep-te.lastMalicious[id] < 5 {
		updated = math.Min(updated, 0.45)
	}

	if behaviorScore < 0.3 {
		te.lastMalicious[id] = te.currentStep
	}

	finalScore := math.Max(0.0, math.Min(1.0, updated))
	te.scores[id] = finalScore
	trustGauge.WithLabelValues(id).Set(finalScore)
	return finalScore
}

// ThresholdManager calculates a dynamic security threshold based on network-wide trust variance.
type ThresholdManager struct {
	mu   sync.Mutex
	prev float64
}

// Compute determines the required trust level based on statistical mean and variance.
func (tm *ThresholdManager) Compute(scores map[string]float64) float64 {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if len(scores) == 0 {
		return 0.5
	}

	var sum, sumSq float64
	n := float64(len(scores))
	for _, v := range scores {
		sum += v
		sumSq += v * v
	}

	mean := sum / n
	variance := (sumSq / n) - (mean * mean)

	target := mean - 0.1 + (0.2 * variance)
	tm.prev = (0.3 * target) + (0.7 * tm.prev)

	val := math.Max(0.3, tm.prev)
	thresholdGauge.Set(val)
	return val
}

// Switchboard facilitates trust-aware routing between lightweight and heavy adapters.
type Switchboard struct {
	Engine    *TrustEngine
	Threshold *ThresholdManager
}

// Route directs packets based on the device's trust score relative to the system threshold.
func (sb *Switchboard) Route(p UMO) {
	start := time.Now()

	sigma := sb.Engine.UpdateTrust(p.SourceID, p.BehaviorScore)
	tau := sb.Threshold.Compute(sb.Engine.scores)

	if sigma >= tau {
		fmt.Printf("[MIL] PASS: %s (σ:%.2f >= τ:%.2f)\n", p.SourceID, sigma, tau)
	} else {
		fmt.Printf("[MIL] ALERT: %s (σ:%.2f < τ:%.2f) -> Redirecting to Heavy Adapter\n", p.SourceID, sigma, tau)
	}

	routingLatency.Observe(time.Since(start).Seconds() * 1000)
	opsProcessed.Inc()
}

// --- 3. Main ---

func main() {
	engine := &TrustEngine{
		scores:        make(map[string]float64),
		lastMalicious: make(map[string]int),
	}
	tm := &ThresholdManager{prev: 0.5}
	sb := &Switchboard{Engine: engine, Threshold: tm}

	go func() {
		http.Handle("/metrics", promhttp.Handler())
		fmt.Println("Prometheus telemetry active on :2112/metrics")
		// Corrected: Wrapped ListenAndServe to handle potential unhandled error
		if err := http.ListenAndServe(":2112", nil); err != nil {
			fmt.Printf("Server error: %v\n", err)
		}
	}()

	for step := 0; ; step++ {
		engine.currentStep = step

		score := 0.8 + (rand.Float64() * 0.2)
		if rand.Float64() < 0.1 {
			score = 0.15
		}

		sb.Route(UMO{
			SourceID:      "Device_01",
			BehaviorScore: score,
		})

		time.Sleep(time.Second)
	}
}
