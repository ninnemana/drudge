package drudge

import (
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"github.com/uber/jaeger-client-go"
	jaegercfg "github.com/uber/jaeger-client-go/config"
	jaegerlog "github.com/uber/jaeger-client-go/log"
	"github.com/uber/jaeger-lib/metrics/prometheus"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
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

type JaegerConfig struct {
	ServiceName string
}

func Jaeger(c interface{}) (func(), error) {
	var conf jaegercfg.Configuration
	switch cfg := c.(type) {
	case JaegerConfig:
		conf = jaegercfg.Configuration{
			ServiceName: cfg.ServiceName,
			Sampler: &jaegercfg.SamplerConfig{
				Type:  jaeger.SamplerTypeConst,
				Param: 1,
			},
			Reporter: &jaegercfg.ReporterConfig{
				LogSpans: true,
			},
		}
	case *jaegercfg.Configuration:
		if cfg == nil {
			return nil, errors.New("configuration was nil")
		}

		conf = *cfg
	default:
		return nil, errors.Errorf("expected Jaeger config, received '%T'", c)
	}

	// Example logger and metrics factory. Use github.com/uber/jaeger-client-go/log
	// and github.com/uber/jaeger-lib/metrics respectively to bind to real logging and metrics
	// frameworks.
	jLogger := jaegerlog.StdLogger

	// Initialize tracer with a logger and a metrics factory
	tracer, closer, err := conf.NewTracer(
		jaegercfg.Logger(jLogger),
		jaegercfg.Metrics(prometheus.New()),
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
