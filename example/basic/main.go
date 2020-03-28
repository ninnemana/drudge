package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/ninnemana/drudge"
	"go.opentelemetry.io/otel/api/global"
	"go.opentelemetry.io/otel/exporters/trace/stdout"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/grpc"
)

func Register(server *grpc.Server) error {
	RegisterExampleServer(server, &Service{
		up: time.Now(),
	})
	return nil
}

type Service struct {
	up time.Time
}

func (s *Service) Hello(ctx context.Context, r *HelloRequest) (*HelloResponse, error) {
	host, err := os.Hostname()
	if err != nil {
		return nil, err

	}
	return &HelloResponse{
		Uptime:  time.Since(s.up).String(),
		Machine: host,
	}, nil
}

func main() {
	exporter, err := stdout.NewExporter(stdout.Options{PrettyPrint: true})
	if err != nil {
		log.Fatalf("failed to create trace exporter: %v", err)
	}

	tp, err := sdktrace.NewProvider(
		sdktrace.WithConfig(sdktrace.Config{DefaultSampler: sdktrace.AlwaysSample()}),
		sdktrace.WithSyncer(exporter),
	)
	if err != nil {
		log.Fatalf("failed to create trace provider: %v", err)
	}
	global.SetTraceProvider(tp)

	opts := drudge.Options{
		BasePath: "/",
		RPC: drudge.Endpoint{
			Network: "tcp",
			Addr:    ":8081",
		},
		ServiceName: "example-basic",
		Addr:        ":8080",
		OnRegister:  Register,
		Handlers: []drudge.Handler{
			RegisterExampleHandler,
		},
	}

	log.Fatalf("failed to start server: %s", drudge.Run(context.Background(), opts))
}
