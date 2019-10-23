package drudge

import (
	"time"

	"contrib.go.opencensus.io/exporter/stackdriver"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"github.com/uber/jaeger-client-go"
	jaegercfg "github.com/uber/jaeger-client-go/config"
	jaegerlog "github.com/uber/jaeger-client-go/log"
	"github.com/uber/jaeger-lib/metrics"
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

type TraceExporter func(interface{}) (func(), error)

type StackDriverConfig struct {
	ProjectID      string
	Prefix         string
	ServiceAccount string
}

type JaegerConfig struct {
	ServiceName string
}

func Jaeger(c interface{}) (func(), error) {
	conf, ok := c.(JaegerConfig)
	if !ok {
		return nil, errors.Errorf("expected '%T', received '%T' as configuration", JaegerConfig{}, c)
	}

	cfg := jaegercfg.Configuration{
		ServiceName: conf.ServiceName,
		Sampler: &jaegercfg.SamplerConfig{
			Type:  jaeger.SamplerTypeConst,
			Param: 1,
		},
		Reporter: &jaegercfg.ReporterConfig{
			LogSpans: true,
		},
	}

	// Example logger and metrics factory. Use github.com/uber/jaeger-client-go/log
	// and github.com/uber/jaeger-lib/metrics respectively to bind to real logging and metrics
	// frameworks.
	jLogger := jaegerlog.StdLogger
	jMetricsFactory := metrics.NullFactory

	// Initialize tracer with a logger and a metrics factory
	tracer, closer, err := cfg.NewTracer(
		jaegercfg.Logger(jLogger),
		jaegercfg.Metrics(jMetricsFactory),
	)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to create tracer")
	}

	// Set the singleton opentracing.Tracer with the Jaeger tracer.
	opentracing.SetGlobalTracer(tracer)

	return func() {
		_ = closer.Close()
	}, nil
}

// StackDriver registers the StackDriver OpenCensus Exporter.
func StackDriver(c interface{}) (func(), error) {
	cfg, ok := c.(StackDriverConfig)
	if !ok {
		return nil, errors.Errorf("expected '%T', received '%T' as configuration", StackDriverConfig{}, c)
	}

	opt := []option.ClientOption{
		option.WithCredentialsJSON([]byte(cfg.ServiceAccount)),
	}

	sd, err := stackdriver.NewExporter(stackdriver.Options{
		ProjectID: cfg.ProjectID,
		// MetricPrefix helps uniquely identify your metrics.
		MetricPrefix:            cfg.Prefix,
		Location:                "k8s_container",
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