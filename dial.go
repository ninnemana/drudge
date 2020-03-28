package drudge

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"

	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	gwruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"
	"go.opencensus.io/plugin/ocgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type Handler func(context.Context, *gwruntime.ServeMux, *grpc.ClientConn) error

func dial(ctx context.Context, network, addr string, certs ...tls.Certificate) (*grpc.ClientConn, error) {
	switch network {
	case "tcp":
		return dialTCP(ctx, addr, certs...)
	case "unix":
		return dialUnix(ctx, addr, certs...)
	default:
		return nil, fmt.Errorf("unsupported network type %q", network)
	}
}

// dialTCP creates a client connection via TCP.
// "addr" must be a valid TCP address with a port number.
func dialTCP(ctx context.Context, addr string, certs ...tls.Certificate) (*grpc.ClientConn, error) {
	return grpc.DialContext(
		ctx,
		addr,
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			Certificates:       certs,
			InsecureSkipVerify: true,
		})),
		grpc.WithStatsHandler(&ocgrpc.ClientHandler{}),
		grpc.WithUnaryInterceptor(UnaryClientInterceptor(serviceName)),
		grpc.WithStreamInterceptor(StreamClientInterceptor(serviceName)),
	)
}

// dialUnix creates a client connection via a unix domain socket.
// "addr" must be a valid path to the socket.
func dialUnix(ctx context.Context, addr string, certs ...tls.Certificate) (*grpc.ClientConn, error) {
	d := func(ctx context.Context, addr string) (net.Conn, error) {
		return net.Dial("unix", addr)
	}

	return grpc.DialContext(
		ctx,
		addr,
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			Certificates:       certs,
			InsecureSkipVerify: true,
		})),
		grpc.WithContextDialer(d),
		grpc.WithStatsHandler(&ocgrpc.ClientHandler{}),
		grpc.WithUnaryInterceptor(UnaryClientInterceptor(serviceName)),
		grpc.WithUnaryInterceptor(grpc_prometheus.UnaryClientInterceptor),
		grpc.WithStreamInterceptor(grpc_prometheus.StreamClientInterceptor),
		grpc.WithStreamInterceptor(StreamClientInterceptor(serviceName)),
	)
}

// newGateway returns a new gateway server which translates HTTP into gRPC.
func newGateway(
	ctx context.Context,
	conn *grpc.ClientConn,
	opts []gwruntime.ServeMuxOption,
	handlers []Handler,
) (http.Handler, error) {
	mux := gwruntime.NewServeMux(opts...)

	for _, f := range handlers {
		if err := f(ctx, mux, conn); err != nil {
			return nil, err
		}
	}

	return mux, nil
}
