package drudge

import (
	"context"
	"net/http"
	"time"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_zap "github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	grpc_ctxtags "github.com/grpc-ecosystem/go-grpc-middleware/tags"
	grpc_opentracing "github.com/grpc-ecosystem/go-grpc-middleware/tracing/opentracing"
	grpc_validator "github.com/grpc-ecosystem/go-grpc-middleware/validator"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	gwruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"go.opencensus.io/plugin/ocgrpc"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
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

	// SwaggerDir is a path to a directory from which the server
	// serves swagger specs.
	SwaggerDir string

	// Mux is a list of options to be passed to the grpc-gateway multiplexer
	Mux []gwruntime.ServeMuxOption

	OnRegister func(server *grpc.Server, router *runtime.ServeMux, conn *grpc.ClientConn) error

	TraceExporter TraceExporter
	TraceConfig   interface{}

	Metrics *RegistryHandler

	Certificate    string
	CertificateKey string
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

	var flush func()

	if opts.TraceExporter != nil {
		var err error

		flush, err = opts.TraceExporter(opts.TraceConfig)
		if err != nil {
			return errors.WithMessage(err, "failed to register trace exporter")
		}
	}

	ctx, cancel := context.WithCancel(ctx)

	defer func() {
		if cancel != nil {
			cancel()
		}

		if flush != nil {
			flush()
		}

		if r := recover(); r != nil {
			lg.Fatal("Recovered from fatal error", zap.Any("recovery", r))
		}
	}()

	serverCert, err := credentials.NewServerTLSFromFile(opts.Certificate, opts.CertificateKey)
	if err != nil {
		return errors.Wrap(err, "failed to create server TLS credentials")
	}

	rpc := grpc.NewServer(
		grpc_middleware.WithUnaryServerChain(
			grpc_validator.UnaryServerInterceptor(),
			grpc_opentracing.UnaryServerInterceptor(grpc_opentracing.WithTracer(opentracing.GlobalTracer())),
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
		grpc.Creds(serverCert),
	)

	grpc.EnableTracing = true
	grpc_prometheus.Register(rpc)

	clientCert, err := credentials.NewClientTLSFromFile(opts.Certificate, "")
	if err != nil {
		return errors.Wrapf(err, "failed to create client TLS credentials")
	}

	conn, err := grpc.DialContext(
		context.Background(),
		"localhost:8080",
		grpc.WithTransportCredentials(clientCert),
	)
	if err != nil {
		return err
	}

	if opts.OnRegister == nil {
		return errors.New("no register callback was defined, this is required for registering the RPC server")
	}

	router := runtime.NewServeMux()
	if err := opts.OnRegister(rpc, router, conn); err != nil {
		return errors.Wrap(err, "failed to register RPC service")
	}

	return errors.Wrap(
		http.ListenAndServeTLS(":8080", opts.Certificate, opts.CertificateKey, tracingWrapper(allowCORS(lg, router, rpc))),
		"failed to start HTTP server",
	)
}
