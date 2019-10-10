package drudge

import (
	"context"
	"log"
	"net"
	"net/http"
	"time"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_zap "github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	grpc_ctxtags "github.com/grpc-ecosystem/go-grpc-middleware/tags"
	grpc_opentracing "github.com/grpc-ecosystem/go-grpc-middleware/tracing/opentracing"
	grpc_validator "github.com/grpc-ecosystem/go-grpc-middleware/validator"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	gwruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/pkg/errors"
	"go.opencensus.io/plugin/ocgrpc"
	"go.opencensus.io/plugin/ochttp"
	"google.golang.org/grpc"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/ninnemana/drudge/telemetry"
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

// Metrics defines the options that are unique to instrumentation of the application.
type Metrics struct {
	// Prefix is the prefix that will be applied to all stats.
	Prefix string

	// PullAddress is the network address that the collected stats
	// should be served from.
	PullAddress string
}

// Options is a set of options to be passed to Run
type Options struct {
	// Metrics contains all properties pertaining to instrumentation.
	Metrics *Metrics

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

	TraceExporter telemetry.TraceExporter
	TraceConfig   interface{}
}

func Run(ctx context.Context, opts Options) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if opts.TraceExporter != nil {
		flush, err := opts.TraceExporter(opts.TraceConfig)
		if err != nil {
			return errors.WithMessage(err, "failed to register trace exporter")
		}
		defer flush()
	}

	if opts.Metrics != nil && opts.Metrics.PullAddress != "" {
		go func() {
			if err := telemetry.StartPrometheus(opts.Metrics.PullAddress); err != nil {
				log.Printf("Failed to register metric exporter: %v", err)
			}
		}()
	}

	lg := initLogger(-1, time.RFC3339)
	// Make sure that log statements internal to gRPC library are logged using the zapLogger as well.
	grpc_zap.ReplaceGrpcLogger(lg)
	grpc.EnableTracing = true

	rpc := grpc.NewServer(
		grpc_middleware.WithUnaryServerChain(
			grpc_validator.UnaryServerInterceptor(),
			grpc_opentracing.UnaryServerInterceptor(),
			grpc_ctxtags.UnaryServerInterceptor(grpc_ctxtags.WithFieldExtractor(grpc_ctxtags.CodeGenRequestFieldExtractor)),
			grpc_zap.UnaryServerInterceptor(lg, grpc_zap.WithLevels(codeToLevel)),
			grpc_prometheus.UnaryServerInterceptor,
		),
		grpc_middleware.WithStreamServerChain(
			grpc_validator.StreamServerInterceptor(),
			grpc_opentracing.StreamServerInterceptor(),
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

	grpc_prometheus.Register(rpc)

	list, err := net.Listen("tcp", opts.RPC.Addr)
	if err != nil {
		return errors.Wrap(err, "failed to open TCP connection")
	}

	log.Println("Serve gRPC on http://", opts.RPC.Addr)
	go func() {
		log.Fatal(errors.Wrap(rpc.Serve(list), "failed to serve gRPC"))
	}()

	conn, err := dial(ctx, opts.RPC.Network, opts.RPC.Addr)
	if err != nil {
		return errors.Wrapf(err, "failed to create network connection for '%s' on '%s'", opts.RPC.Network, opts.RPC.Addr)
	}

	go func() {
		<-ctx.Done()
		if err := conn.Close(); err != nil {
			log.Fatalf("Failed to close a client connection to the gRPC server: %v", err)
		}
	}()

	r := http.NewServeMux()

	r.HandleFunc("/openapi/", swaggerServer(opts.SwaggerDir))
	
	// Register Prometheus metrics handler.    
    http.Handle("/metrics", promhttp.Handler())

	gw, err := newGateway(ctx, conn, opts.Mux, opts.Handlers)
	if err != nil {
		return err
	}
	r.Handle("/", gw)

	s := &http.Server{
		Addr: opts.Addr,
		Handler: &ochttp.Handler{
			Handler: allowCORS(r),
		},
	}
	go func() {
		<-ctx.Done()
		log.Println("shutting down the http server")
		if err := s.Shutdown(context.Background()); err != nil {
			log.Fatalf("failed to shutdown http server: %v", err)
		}
	}()

	log.Printf("starting listening at %s\n", opts.Addr)
	if err := s.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("failed to listen and serve: %v", err)
		return err
	}

	return nil
}
