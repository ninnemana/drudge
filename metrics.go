package drudge

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/pkg/errors"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"go.uber.org/zap"
)

func MeasureInt(ctx context.Context, m *stats.Int64Measure, v int64, tags ...tag.Mutator) {
	if m == nil {
		return
	}

	switch len(tags) {
	case 0:
		stats.Record(ctx, m.M(v))
	default:
		_ = stats.RecordWithTags(ctx, tags, m.M(v))
	}
}

func MeasureFloat(ctx context.Context, m *stats.Float64Measure, v float64, tags ...tag.Mutator) {
	if m == nil {
		return
	}

	switch len(tags) {
	case 0:
		stats.Record(ctx, m.M(v))
	default:
		_ = stats.RecordWithTags(ctx, tags, m.M(v))
	}
}

type RegistryHandler struct {
	metrics map[string]interface{}
	log     *zap.Logger
	sync.Mutex
}

// Int64Measure establishes a new OpenCensus Integer Metric based on the provided information and registers
// a configured stats.View.
func (r *RegistryHandler) Int64Measure(name, description, unit string, tags []tag.Key, aggregate *view.Aggregation) *stats.Int64Measure {
	if r.exists(name) {
		r.log.Fatal("the provided metric name is already registered", zap.String("name", name))
	}

	s := stats.Int64(name, description, unit)

	if err := view.Register(&view.View{
		Name:        name,
		Measure:     s,
		Description: description,
		Aggregation: aggregate,
		TagKeys:     tags,
	}); err != nil {
		_ = err
	}

	r.put(name, s)

	return s
}

// Float64Measure establishes a new OpenCensus Floating Point Metric based on the provided information and registers
// a configured stats.View.
func (r *RegistryHandler) Float64Measure(name, description, unit string, tags []tag.Key, aggregate *view.Aggregation) *stats.Float64Measure {
	if r.exists(name) {
		r.log.Fatal("the provided metric name is already registered", zap.String("name", name))
	}

	s := stats.Float64(name, description, unit)

	if err := view.Register(&view.View{
		Name:        name,
		Measure:     s,
		Description: description,
		Aggregation: aggregate,
		TagKeys:     tags,
	}); err != nil {
		_ = err
	}

	r.put(name, s)

	return s
}

func (r *RegistryHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if err := json.NewEncoder(w).Encode(r.metrics); err != nil {
		http.Error(w, errors.Wrap(err, "failed to encode metric list").Error(), http.StatusInternalServerError)
		return
	}
}

func (r *RegistryHandler) Metrics() map[string]interface{} {
	return r.metrics
}

func (r *RegistryHandler) exists(key string) bool {
	_, ok := r.metrics[key]
	return ok
}

func (r *RegistryHandler) put(key string, m interface{}) {
	r.Lock()
	if r.metrics == nil {
		r.metrics = map[string]interface{}{}
	}

	r.metrics[key] = m
	r.Unlock()
}
