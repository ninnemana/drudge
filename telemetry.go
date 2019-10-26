package drudge

import (
	"fmt"
	"net/http"
	"time"

	jaegercensus "contrib.go.opencensus.io/exporter/jaeger"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"github.com/pkg/errors"
	"github.com/uber/jaeger-client-go"
	jaegercfg "github.com/uber/jaeger-client-go/config"
	jaegerlog "github.com/uber/jaeger-client-go/log"
	"github.com/uber/jaeger-lib/metrics/prometheus"
	"go.opencensus.io/plugin/ocgrpc"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"go.opencensus.io/trace"
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
	jaegerOpts := jaegercensus.Options{}

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
		jaegerOpts.ServiceName = conf.ServiceName
	case *jaegercfg.Configuration:
		if cfg == nil {
			return nil, errors.New("configuration was nil")
		}

		conf = *cfg
		jaegerOpts.ServiceName = cfg.ServiceName
		jaegerOpts.AgentEndpoint = cfg.Reporter.LocalAgentHostPort
		jaegerOpts.CollectorEndpoint = cfg.Reporter.CollectorEndpoint
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
	// opentracing.SetGlobalTracer(tracer)
	_ = tracer

	je, err := jaegercensus.NewExporter(jaegerOpts)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to create the Jaeger exporter")
	}

	trace.RegisterExporter(je)
	trace.ApplyConfig(trace.Config{DefaultSampler: trace.AlwaysSample()})

	// Register the views to collect server request count.
	if err := view.Register(ocgrpc.DefaultServerViews...); err != nil {
		return nil, errors.WithMessage(err, "failed to register server metric views")
	}

	view.SetReportingPeriod(1 * time.Second)

	return func() {
		_ = closer.Close()
	}, nil
}

var drudgeTag = opentracing.Tag{Key: string(ext.Component), Value: "drudge"}

func tracingWrapper(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			h.ServeHTTP(w, r)
			return
		}

		spanName := fmt.Sprintf("http.%s.[%s]", r.Method, r.URL.Path)

		parentSpanContext, err := opentracing.GlobalTracer().Extract(
			opentracing.HTTPHeaders,
			opentracing.HTTPHeadersCarrier(r.Header),
		)
		if err == nil || err == opentracing.ErrSpanContextNotFound {
			serverSpan := opentracing.GlobalTracer().StartSpan(
				spanName,
				ext.RPCServerOption(parentSpanContext),
				drudgeTag,
			)
			r = r.WithContext(opentracing.ContextWithSpan(r.Context(), serverSpan))
			defer serverSpan.Finish()
		}

		ctx, span := trace.StartSpan(r.Context(), spanName)
		defer span.End()
		r = r.WithContext(ctx)

		h.ServeHTTP(w, r)
	})
}
