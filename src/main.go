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

//////////////////////////////////////////////////////////////////
// 1. PROMETHEUS METRICS
//////////////////////////////////////////////////////////////////

var (
	opsProcessed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mil_processed_messages_total",
		Help: "Total number of processed IoT messages",
	})

	inferenceLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "mil_inference_latency_ms",
			Help:    "Inference latency by adapter/model type",
			Buckets: []float64{1, 2, 5, 10, 15, 20, 30},
		},
		[]string{"model_type"},
	)

	routingLatency = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "mil_routing_latency_ms",
			Help:    "Latency of trust-aware routing decisions",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10},
		},
	)

	trustGauge = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "mil_device_trust_score",
			Help: "Current trust score per device",
		},
		[]string{"device_id"},
	)

	thresholdGauge = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "mil_dynamic_threshold",
			Help: "Current adaptive trust threshold",
		},
	)

	modelUsage = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mil_model_selection_total",
			Help: "Number of times each model path was selected",
		},
		[]string{"model_type"},
	)
)

//////////////////////////////////////////////////////////////////
// 2. MODEL / INFERENCE ENGINE
//////////////////////////////////////////////////////////////////

type ModelType string

const (
	TFLiteINT8 ModelType = "TFLite_INT8_Quantized"
	FP32Heavy  ModelType = "Standard_FP32"
)

type InferenceEngine struct {
	mType ModelType
}

func (ie *InferenceEngine) Predict(input float64) float64 {
	start := time.Now()
	var result float64

	switch ie.mType {
	case TFLiteINT8:
		result = math.Round(input*255) / 255
		time.Sleep(2 * time.Millisecond)
	case FP32Heavy:
		result = input
		time.Sleep(15 * time.Millisecond)
	}

	inferenceLatency.
		WithLabelValues(string(ie.mType)).
		Observe(time.Since(start).Seconds() * 1000)

	return result
}

//////////////////////////////////////////////////////////////////
// 3. MESSAGE STRUCTURE
//////////////////////////////////////////////////////////////////

type UMO struct {
	SourceID      string
	Payload       float64
	BehaviorScore float64
}

//////////////////////////////////////////////////////////////////
// 4. TRUST ENGINE
//////////////////////////////////////////////////////////////////

type TrustEngine struct {
	mu            sync.RWMutex
	scores        map[string]float64
	lastMalicious map[string]int
	currentStep   int
}

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

//////////////////////////////////////////////////////////////////
// 5. DYNAMIC THRESHOLD MANAGER
//////////////////////////////////////////////////////////////////

type ThresholdManager struct {
	mu   sync.Mutex
	prev float64
}

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

	final := math.Max(0.3, math.Min(0.95, tm.prev))
	thresholdGauge.Set(final)

	return final
}

//////////////////////////////////////////////////////////////////
// 6. SWITCHBOARD
//////////////////////////////////////////////////////////////////

type Switchboard struct {
	Engine     *TrustEngine
	Threshold  *ThresholdManager
	FastModel  *InferenceEngine
	HeavyModel *InferenceEngine
}

func (sb *Switchboard) Route(p UMO) {
	start := time.Now()

	sigma := sb.Engine.UpdateTrust(p.SourceID, p.BehaviorScore)
	tau := sb.Threshold.Compute(sb.Engine.scores)

	var prediction float64

	if sigma >= tau {
		prediction = sb.FastModel.Predict(p.BehaviorScore)
		modelUsage.WithLabelValues(string(TFLiteINT8)).Inc()
		fmt.Printf("[⚡ FAST] %s | σ=%.2f τ=%.2f | %s | output=%.4f\n",
			p.SourceID, sigma, tau, TFLiteINT8, prediction)
	} else {
		prediction = sb.HeavyModel.Predict(p.BehaviorScore)
		modelUsage.WithLabelValues(string(FP32Heavy)).Inc()
		fmt.Printf("[🛡️ HEAVY] %s | σ=%.2f τ=%.2f | %s | output=%.4f\n",
			p.SourceID, sigma, tau, FP32Heavy, prediction)
	}

	routingLatency.Observe(time.Since(start).Seconds() * 1000)
	opsProcessed.Inc()
}

//////////////////////////////////////////////////////////////////
// 7. MAIN
//////////////////////////////////////////////////////////////////

func main() {
	rand.Seed(time.Now().UnixNano())

	engine := &TrustEngine{
		scores:        make(map[string]float64),
		lastMalicious: make(map[string]int),
	}

	thresholdManager := &ThresholdManager{prev: 0.5}

	switchboard := &Switchboard{
		Engine:     engine,
		Threshold:  thresholdManager,
		FastModel:  &InferenceEngine{mType: TFLiteINT8},
		HeavyModel: &InferenceEngine{mType: FP32Heavy},
	}

	// Prometheus Metrics Server
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		fmt.Println("📊 Prometheus telemetry active at http://localhost:2112/metrics")
		if err := http.ListenAndServe(":2112", nil); err != nil {
			fmt.Printf("Metrics server error: %v\n", err)
		}
	}()

	fmt.Println("🚀 Adaptive MIL-Switchboard Active")

	devices := []string{
		"Edge_Node_01",
		"Edge_Node_02",
		"Industrial_Sensor_A",
		"Industrial_Sensor_B",
		"Drone_Unit_X",
	}

	// Simulation loop
	for step := 0; ; step++ {
		engine.currentStep = step

		for _, dev := range devices {
			var score float64

			// Sybil / Coordinated Attack simulation
			if step >= 50 && step <= 60 && (dev == "Industrial_Sensor_A" || dev == "Industrial_Sensor_B" || dev == "Drone_Unit_X") {
				score = 0.05
				if dev == "Drone_Unit_X" {
					fmt.Printf("\n🚨 ATTACK STEP %d: %s injecting malicious payload!\n", step, dev)
				}
			} else {
				score = 0.82 + (rand.Float64() * 0.15)
			}

			switchboard.Route(UMO{
				SourceID:      dev,
				BehaviorScore: score,
				Payload:       rand.Float64(),
			})
		}

		fmt.Printf("--- System State: Step %d | Threshold (τ): %.3f ---\n", step, thresholdManager.prev)
		time.Sleep(1 * time.Second)
	}
}
