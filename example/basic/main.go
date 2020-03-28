package main

import (
	"context"
	"log"

	"github.com/ninnemana/drudge"
	"google.golang.org/grpc"
)

func Register(server *grpc.Server) error {
	RegisterExampleServer(server, &Service{})
	return nil
}

type Service struct {
}

type Params struct {
}

func (s *Service) Hello(ctx context.Context, r *HelloRequest) (*HelloResponse, error) {
	return nil, nil
}

func main() {
	opts := drudge.Options{
		BasePath: "/",
		RPC: drudge.Endpoint{
			Network: "tcp",
			Addr:    ":8081",
		},
		Addr:       ":8080",
		OnRegister: Register,
	}

	log.Fatalf("failed to start server: %s", drudge.Run(context.Background(), opts))
}
