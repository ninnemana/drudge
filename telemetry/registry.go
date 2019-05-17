package telemetry

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/pkg/errors"
)

var (
	registeredMetrics = &registry{}
)

type registry struct {
	metrics map[string]interface{}
	sync.Mutex
}

type RegistryHandler struct{}

func (r RegistryHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if err := json.NewEncoder(w).Encode(registeredMetrics.metrics); err != nil {
		http.Error(w, errors.Wrap(err, "failed to encode metric list").Error(), http.StatusInternalServerError)
		return
	}
}

func (r *registry) exists(key string) bool {
	_, ok := r.metrics[key]
	return ok
}

func (r *registry) put(key string, m interface{}) {
	r.Lock()
	if r.metrics == nil {
		r.metrics = map[string]interface{}{}
	}

	r.metrics[key] = m
	r.Unlock()
}
