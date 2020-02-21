package main

import (
	"context"
	"log"
	"sync"

	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/ninnemana/drudge"
	"github.com/ninnemana/drudge/examples/basic/pkg/echo"
	"google.golang.org/grpc"
)

func main() {
	err := drudge.Run(context.Background(), drudge.Options{
		Certificate:    "server.crt",
		CertificateKey: "server.key",
		BasePath:       "/",
		Addr:           ":8088",
		SwaggerDir:     "openapi",
		Mux:            nil,
		OnRegister:     Register,
		Metrics: &drudge.RegistryHandler{
			Mutex: sync.Mutex{},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}

func Register(s *grpc.Server, router *runtime.ServeMux, conn *grpc.ClientConn) error {
	if err := echo.RegisterEchoHandler(context.Background(), router, conn); err != nil {
		return err
	}

	echo.RegisterEchoServer(s, &Service{})
	return nil
}

type Service struct {
}

func (s *Service) Hello(_ context.Context, _ *echo.GetParams) (*echo.Response, error) {
	return &echo.Response{
		Msg: "Hello",
	}, nil
}
