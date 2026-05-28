package models

const (
	// Counter identifies counter metrics in API payloads and storage backends.
	Counter = "counter"
	// Gauge identifies gauge metrics in API payloads and storage backends.
	Gauge = "gauge"
)

// Metrics describes a single metric value transferred between agent, server and storage.
type Metrics struct {
	// ID is the metric name.
	ID string `json:"id"`
	// MType is the metric kind: gauge or counter.
	MType string `json:"type"`
	// Delta contains the counter increment for counter metrics.
	Delta *int64 `json:"delta,omitempty"`
	// Value contains the floating-point value for gauge metrics.
	Value *float64 `json:"value,omitempty"`
	// Hash carries the optional request signature for compatibility with the API schema.
	Hash string `json:"hash,omitempty"`
}
