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
		Help: "Total number of IoT messages processed",
	})
	routingLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "mil_inference_latency_ms",
		Help:    "Latency based on model type (Quantized vs Full)",
		Buckets: []float64{1, 2, 5, 10, 15, 20, 25},
	}, []string{"model_type"})
	trustGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "mil_device_trust_score",
		Help: "Current trust score (sigma) per device",
	}, []string{"device_id"})
)

// --- 2. Quantization Simulation ---

type ModelType string

const (
	TFLiteINT8   ModelType = "TFLite_INT8_Quantized"
	StandardFP32 ModelType = "Standard_FP32"
)

// InferenceEngine simulates the behavior of a real ML model
type InferenceEngine struct {
	mType ModelType
}

// Predict simulates inference. Quantized models are faster but have "rounding noise".
func (ie *InferenceEngine) Predict(input float64) float64 {
	start := time.Now()
	var result float64

	if ie.mType == TFLiteINT8 {
		// SIMULATION: INT8 Quantization introduces small rounding errors
		// scale := 0.0039 // (1/255)
		result = math.Round(input*255) / 255
		time.Sleep(2 * time.Millisecond) // Fast Inference
	} else {
		// SIMULATION: FP32 High precision
		result = input
		time.Sleep(15 * time.Millisecond) // Heavy Inference
	}

	routingLatency.WithLabelValues(string(ie.mType)).Observe(time.Since(start).Seconds() * 1000)
	return result
}

// --- 3. Core Logic ---

type UMO struct {
	SourceID      string
	Payload       float64
	BehaviorScore float64
}

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

type Switchboard struct {
	Engine     *TrustEngine
	Threshold  float64
	FastModel  *InferenceEngine // TFLite Quantized
	HeavyModel *InferenceEngine // Standard FP32
}

func (sb *Switchboard) Route(p UMO) {
	sigma := sb.Engine.UpdateTrust(p.SourceID, p.BehaviorScore)

	var prediction float64
	if sigma >= sb.Threshold {
		// Trusted Device: Run Fast Quantized Model
		prediction = sb.FastModel.Predict(p.BehaviorScore)
		fmt.Printf("[⚡ FAST] Device: %s | Trust: %.2f | Model: %s | Output: %.4f\n",
			p.SourceID, sigma, TFLiteINT8, prediction)
	} else {
		// Untrusted Device: Run Heavy FP32 Model
		prediction = sb.HeavyModel.Predict(p.BehaviorScore)
		fmt.Printf("[🛡️ HEAVY] Device: %s | Trust: %.2f | Model: %s | Output: %.4f\n",
			p.SourceID, sigma, StandardFP32, prediction)
	}
	opsProcessed.Inc()
}

// --- 4. Execution ---

func main() {
	engine := &TrustEngine{
		scores:        make(map[string]float64),
		lastMalicious: make(map[string]int),
	}

	sb := &Switchboard{
		Engine:     engine,
		Threshold:  0.65, // Static for demo, could be dynamic
		FastModel:  &InferenceEngine{mType: TFLiteINT8},
		HeavyModel: &InferenceEngine{mType: StandardFP32},
	}

	// Metrics Server
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		fmt.Println("📊 Telemetry: http://localhost:2112/metrics")
		http.ListenAndServe(":2112", nil)
	}()

	fmt.Println("🚀 MIL-Switchboard with TFLite Quantization Simulation active...")

	for step := 0; ; step++ {
		engine.currentStep = step

		// Normal behavior (High Score)
		score := 0.8 + (rand.Float64() * 0.2)

		// Inject anomaly every 15 seconds to trigger Heavy Adapter
		if step > 0 && step%15 == 0 {
			score = 0.1
			fmt.Println("\n⚠️ ANOMALY DETECTED - DROPPING TRUST...")
		}

		sb.Route(UMO{
			SourceID:      "IoT_Sensor_01",
			BehaviorScore: score,
		})

		time.Sleep(1 * time.Second)
	}
}
