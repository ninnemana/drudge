package drudge

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_zap "github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	grpc_ctxtags "github.com/grpc-ecosystem/go-grpc-middleware/tags"
	grpc_opentracing "github.com/grpc-ecosystem/go-grpc-middleware/tracing/opentracing"
	grpc_validator "github.com/grpc-ecosystem/go-grpc-middleware/validator"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	gwruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/opentracing/opentracing-go"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opencensus.io/plugin/ocgrpc"
	"go.opencensus.io/plugin/ochttp"
	"go.opentelemetry.io/otel/api/global"
	"go.opentelemetry.io/otel/exporters/trace/stdout"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

const (
	GoogleProjectID      = "GCE_PROJECT_ID"
	GoogleServiceAccount = "GCE_SERVICE_ACCOUNT"
)

// Endpoint describes a gRPC endpoint
type Endpoint struct {
	Network string
	Addr    string
}

// Options is a set of options to be passed to Run
type Options struct {
	// BasePath is the root path that the HTTP service listens on
	BasePath string

	// Addr is the address to listen
	Addr string

	// GRPCServer defines an endpoint of a gRPC service
	RPC Endpoint

	// Defines the RPC Clients to pass requests through
	Handlers []Handler

	// SwaggerDir is a path to a directory from which the server
	// serves swagger specs.
	SwaggerDir string

	// Mux is a list of options to be passed to the grpc-gateway multiplexer
	Mux []gwruntime.ServeMuxOption

	OnRegister func(server *grpc.Server) error

	// TraceExporter TraceExporter
	ServiceName string
	TraceConfig interface{}

	Metrics *RegistryHandler
}

func Run(ctx context.Context, opts Options) error {
	lg := initLogger(-1, time.RFC3339)
	// Make sure that log statements internal to gRPC library are logged using the zapLogger as well.
	grpc_zap.ReplaceGrpcLogger(lg)

	if opts.Metrics == nil {
		opts.Metrics = &RegistryHandler{
			log: lg,
		}
	}

	exporter, err := stdout.NewExporter(stdout.Options{PrettyPrint: true})
	if err != nil {
		lg.Fatal("failed to create trace exporter", zap.Error(err))
	}
	tp, err := sdktrace.NewProvider(
		sdktrace.WithConfig(sdktrace.Config{DefaultSampler: sdktrace.AlwaysSample()}),
		sdktrace.WithSyncer(exporter),
	)
	if err != nil {
		lg.Fatal("failed to create trace provider", zap.Error(err))
	}
	global.SetTraceProvider(tp)

	ctx, cancel := context.WithCancel(ctx)

	defer func() {
		if cancel != nil {
			cancel()
		}

		if r := recover(); r != nil {
			lg.Fatal("Recovered from fatal error", zap.Any("recovery", r))
		}
	}()

	rpc := grpc.NewServer(
		grpc.UnaryInterceptor(opts.UnaryServerInterceptor),
		grpc_middleware.WithUnaryServerChain(
			grpc_validator.UnaryServerInterceptor(),
			grpc_ctxtags.UnaryServerInterceptor(grpc_ctxtags.WithFieldExtractor(grpc_ctxtags.CodeGenRequestFieldExtractor)),
			grpc_zap.UnaryServerInterceptor(lg, grpc_zap.WithLevels(codeToLevel)),
			grpc_prometheus.UnaryServerInterceptor,
		),
		grpc_middleware.WithStreamServerChain(
			grpc_validator.StreamServerInterceptor(),
			grpc_opentracing.StreamServerInterceptor(grpc_opentracing.WithTracer(opentracing.GlobalTracer())),
			grpc_ctxtags.StreamServerInterceptor(grpc_ctxtags.WithFieldExtractor(grpc_ctxtags.CodeGenRequestFieldExtractor)),
			grpc_zap.StreamServerInterceptor(lg, grpc_zap.WithLevels(codeToLevel)),
			grpc_prometheus.StreamServerInterceptor,
		),
		grpc.StatsHandler(&ocgrpc.ServerHandler{}),
	)

	if opts.OnRegister == nil {
		return errors.New("no register callback was defined, this is required for registering the RPC server")
	}

	if err := opts.OnRegister(rpc); err != nil {
		return errors.Wrap(err, "failed to register RPC service")
	}

	grpc.EnableTracing = true

	grpc_prometheus.Register(rpc)

	list, err := net.Listen("tcp", opts.RPC.Addr)
	if err != nil {
		return errors.Wrap(err, "failed to open TCP connection")
	}

	lg.Info("Serve gRPC", zap.String("address", fmt.Sprintf("http://%s", opts.RPC.Addr)))

	go func() {
		lg.Fatal("failed to serve gRPC", zap.Error(rpc.Serve(list)))
	}()

	lg.Info(
		"Dialing RPC service connection",
		zap.String("address", opts.RPC.Addr),
		zap.String("network", opts.RPC.Network),
	)

	conn, err := dial(ctx, opts.RPC.Network, opts.RPC.Addr)
	if err != nil {
		return errors.Wrapf(err, "failed to create network connection for '%s' on '%s'", opts.RPC.Network, opts.RPC.Addr)
	}

	go func() {
		<-ctx.Done()
		if err := conn.Close(); err != nil {
			lg.Fatal("Failed to close a client connection to the gRPC server", zap.Error(err))
		}
	}()

	gw, err := newGateway(ctx, conn, opts.Mux, opts.Handlers)
	if err != nil {
		return err
	}

	r := http.NewServeMux()

	r.HandleFunc("/openapi/", swaggerServer(lg, opts.SwaggerDir))

	// Register Prometheus metrics handler.
	r.Handle("/metrics", promhttp.Handler())
	r.Handle("/metrics/list", opts.Metrics)

	// must be registered last
	r.Handle("/", gw)

	s := &http.Server{
		Addr: opts.Addr,
		Handler: &ochttp.Handler{
			Handler: grpcWrapper(rpc, opts.tracingWrapper(allowCORS(lg, r))),
		},
	}

	go func() {
		<-ctx.Done()
		lg.Info("shutting down the http server")
		if err := s.Shutdown(context.Background()); err != nil {
			lg.Fatal("failed to shutdown http server", zap.Error(err))
		}
	}()

	lg.Info("starting HTTP server", zap.String("address", opts.Addr))

	if err := s.ListenAndServe(); err != http.ErrServerClosed {
		lg.Fatal("failed to listen and serve", zap.Error(err))
		return err
	}

	return nil
}

func grpcWrapper(rpc, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 2 && strings.Contains(r.Header.Get("Content-Type"), "application/grpc") {
			rpc.ServeHTTP(w, r)
		} else {
			handler.ServeHTTP(w, r)
		}
	})
}
