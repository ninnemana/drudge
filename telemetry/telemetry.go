package telemetry

import (
	"context"
	"log"
	"net/http"
	"time"

	"contrib.go.opencensus.io/exporter/prometheus"
	"contrib.go.opencensus.io/exporter/stackdriver"
	"github.com/pkg/errors"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"go.opencensus.io/trace"
	"google.golang.org/api/option"
)

var (
	LatencyTag, _  = tag.NewKey("latency")
	ErrorTag, _    = tag.NewKey("error")
	EndpointTag, _ = tag.NewKey("endpoint")
	MethodTag, _   = tag.NewKey("method")
	StatusTag, _   = tag.NewKey("status")
	ServiceTag, _  = tag.NewKey("service")

	LatencyDistribution = view.Distribution(25, 50, 75, 100, 200, 400, 600, 800, 1000, 2000, 4000, 6000)
)

// StackDriver registers the StackDriver OpenCensus Exporter.
func StackDriver(projectID, prefix, serviceAccount string) (func(), error) {
	opt := []option.ClientOption{
		option.WithCredentialsJSON([]byte(serviceAccount)),
	}

	sd, err := stackdriver.NewExporter(stackdriver.Options{
		ProjectID: projectID,
		// MetricPrefix helps uniquely identify your metrics.
		MetricPrefix: prefix,
		Location:     "k8s_container",
		OnError: func(err error) {
			log.Printf("failed to export: %v\n", err)
		},
		MonitoringClientOptions: opt,
		TraceClientOptions:      opt,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to create stats exporter")
	}

	// Register it as a metrics exporter
	view.RegisterExporter(sd)
	view.SetReportingPeriod(60 * time.Second)

	// Register it as a trace exporter
	trace.RegisterExporter(sd)
	trace.ApplyConfig(trace.Config{DefaultSampler: trace.AlwaysSample()})

	return sd.Flush, nil
}

// StartPrometheus establishes a Prometheus exporter for OpenCensus instrumentation and creates an HTTP server
// allowing a Prometheus instance to scrape the recorded metrics by the application.
func StartPrometheus(addr string) error {
	if addr == "" {
		return errors.Errorf("the provided '%s' address is not valid", addr)
	}

	pe, err := prometheus.NewExporter(prometheus.Options{
		OnError: func(err error) {
			log.Println("prom error: ", err)
		},
	})
	if err != nil {
		return errors.Wrap(err, "failed to create Prometheus exporter")
	}

	// Ensure that we register it as a stats exporter.
	view.RegisterExporter(pe)
	view.SetReportingPeriod(time.Second * 10)

	mux := http.NewServeMux()
	mux.Handle("/metrics", pe)
	mux.Handle("/metrics/list", RegistryHandler{})
	return errors.Wrap(
		http.ListenAndServe(addr, mux),
		"failed to run Prometheus /metrics endpoint",
	)
}

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

// Int64Measure establishes a new OpenCensus Integer Metric based on the provided information and registers
// a configured stats.View.
func Int64Measure(name, description, unit string, tags []tag.Key, aggregate *view.Aggregation) *stats.Int64Measure {
	if registeredMetrics.exists(name) {
		log.Fatalf("the provided metric name '%s' is already registered", name)
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

	registeredMetrics.put(name, s)

	return s
}

// Float64Measure establishes a new OpenCensus Floating Point Metric based on the provided information and registers
// a configured stats.View.
func Float64Measure(name, description, unit string, tags []tag.Key, aggregate *view.Aggregation) *stats.Float64Measure {
	if registeredMetrics.exists(name) {
		log.Fatalf("the provided metric name '%s' is already registered", name)
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

	registeredMetrics.put(name, s)

	return s
}
