package drudge

import (
	"context"
	"net/http"

	"github.com/opentracing/opentracing-go/ext"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"go.opentelemetry.io/otel/api/core"
	"go.opentelemetry.io/otel/api/correlation"
	"go.opentelemetry.io/otel/api/global"
	"go.opentelemetry.io/otel/api/key"
	"go.opentelemetry.io/otel/api/trace"
	"go.opentelemetry.io/otel/plugin/grpctrace"
	"go.opentelemetry.io/otel/plugin/httptrace"
)

// UnaryServerInterceptor intercepts and extracts incoming trace data
func UnaryServerInterceptor(n string) grpc.UnaryServerInterceptor {
	name := n
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		requestMetadata, _ := metadata.FromIncomingContext(ctx)
		metadataCopy := requestMetadata.Copy()

		entries, spanCtx := grpctrace.Extract(ctx, &metadataCopy)
		ctx = correlation.ContextWithMap(ctx, correlation.NewMap(correlation.MapUpdate{
			MultiKV: entries,
		}))

		grpcServerKey := key.New("grpc.server")
		serverSpanAttrs := []core.KeyValue{
			grpcServerKey.String(name),
		}

		tr := global.Tracer(name)
		ctx, span := tr.Start(
			trace.ContextWithRemoteSpanContext(ctx, spanCtx),
			"drudge.middleware",
			trace.WithAttributes(serverSpanAttrs...),
			trace.WithSpanKind(trace.SpanKindServer),
		)
		defer span.End()

		return handler(ctx, req)
	}
}

func StreamServerInterceptor(n string) grpc.StreamServerInterceptor {
	name := n
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		requestMetadata, _ := metadata.FromIncomingContext(ss.Context())
		metadataCopy := requestMetadata.Copy()

		entries, spanCtx := grpctrace.Extract(ss.Context(), &metadataCopy)
		ctx := correlation.ContextWithMap(ss.Context(), correlation.NewMap(correlation.MapUpdate{
			MultiKV: entries,
		}))

		grpcServerKey := key.New("grpc.server")
		serverSpanAttrs := []core.KeyValue{
			grpcServerKey.String(name),
		}

		tr := global.Tracer(name)
		ctx, span := tr.Start(
			trace.ContextWithRemoteSpanContext(ctx, spanCtx),
			"drudge.middleware",
			trace.WithAttributes(serverSpanAttrs...),
			trace.WithSpanKind(trace.SpanKindServer),
		)
		defer span.End()

		return handler(ctx, ss)
	}
}

// UnaryClientInterceptor intercepts and injects outgoing trace
func UnaryClientInterceptor(n string) grpc.UnaryClientInterceptor {
	name := n
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		requestMetadata, _ := metadata.FromOutgoingContext(ctx)
		metadataCopy := requestMetadata.Copy()

		tr := global.Tracer(name)
		err := tr.WithSpan(ctx, "drudge.middleware",
			func(ctx context.Context) error {
				grpctrace.Inject(ctx, &metadataCopy)
				ctx = metadata.NewOutgoingContext(ctx, metadataCopy)

				err := invoker(ctx, method, req, reply, cc, opts...)
				setTraceStatus(ctx, err)
				return err
			})
		return err
	}
}

// StreamClientInterceptor intercepts and injects outgoing trace
func StreamClientInterceptor(n string) grpc.StreamClientInterceptor {
	name := n
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		requestMetadata, _ := metadata.FromOutgoingContext(ctx)
		metadataCopy := requestMetadata.Copy()

		tr := global.Tracer(name)
		var cs grpc.ClientStream
		err := tr.WithSpan(
			ctx,
			"drudge.middleware",
			func(ctx context.Context) error {
				grpctrace.Inject(ctx, &metadataCopy)
				ctx = metadata.NewOutgoingContext(ctx, metadataCopy)

				var err error
				cs, err = streamer(ctx, desc, cc, method, opts...)
				setTraceStatus(ctx, err)
				return err
			},
		)
		return cs, err
	}
}

func setTraceStatus(ctx context.Context, err error) {
	if err != nil {
		s, _ := status.FromError(err)
		trace.SpanFromContext(ctx).SetStatus(s.Code(), s.Message())
	}
}

func (o Options) tracingWrapper(h http.Handler) http.Handler {
	tr := global.Tracer(o.ServiceName)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			h.ServeHTTP(w, r)
			return
		}

		attrs, entries, spanCtx := httptrace.Extract(r.Context(), r)

		r = r.WithContext(correlation.ContextWithMap(r.Context(), correlation.NewMap(correlation.MapUpdate{
			MultiKV: entries,
		})))

		ctx, span := tr.Start(
			trace.ContextWithRemoteSpanContext(r.Context(), spanCtx),
			"middleware",
			trace.WithAttributes(attrs...),
		)
		defer span.End()

		span.AddEvent(ctx, "Handling HTTP Request")
		span.SetAttributes(core.KeyValue{
			Key:   core.Key(ext.HTTPUrl),
			Value: core.String(r.URL.Path),
		}, core.KeyValue{
			Key:   core.Key(ext.HTTPMethod),
			Value: core.String(r.Method),
		}, core.KeyValue{
			Key:   "HTTP.UserAgent",
			Value: core.String(r.UserAgent()),
		})

		trw := NewTraceableResponseWriter(w)
		h.ServeHTTP(trw, r)
		span.SetAttributes(core.KeyValue{
			Key:   core.Key(ext.HTTPStatusCode),
			Value: core.Int(trw.statusCode),
		})
	})
}

type traceableResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func NewTraceableResponseWriter(w http.ResponseWriter) *traceableResponseWriter {
	return &traceableResponseWriter{w, http.StatusOK}
}

func (trw *traceableResponseWriter) WriteHeader(code int) {
	trw.statusCode = code
	trw.ResponseWriter.WriteHeader(code)
}

func (trw *traceableResponseWriter) Flush() {
	// Work around the fact that WriteHeader and a call to Flush would have caused a 200 response.
	// This is the case when there is no payload.
	trw.ResponseWriter.(http.Flusher).Flush()
}
