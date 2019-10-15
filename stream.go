package drudge

import (
	context "context"
	fmt "fmt"
	io "io"
	"net/http"

	proto "github.com/gogo/protobuf/proto"
	types "github.com/gogo/protobuf/types"
	goproto "github.com/golang/protobuf/proto"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/status"
)

// ForwardResponseStream forwards the stream from gRPC server to REST client.
func ForwardResponseStream(
	ctx context.Context,
	mux *runtime.ServeMux,
	marshaler runtime.Marshaler,
	w http.ResponseWriter,
	req *http.Request,
	recv func() (goproto.Message, error), opts ...func(context.Context, http.ResponseWriter, goproto.Message) error,
) {
	f, ok := w.(http.Flusher)
	if !ok {
		grpclog.Infof("Flush not supported in %T", w)
		http.Error(w, "unexpected type of web server", http.StatusInternalServerError)

		return
	}

	md, ok := runtime.ServerMetadataFromContext(ctx)
	if !ok {
		grpclog.Infof("Failed to extract ServerMetadata from context")
		http.Error(w, "unexpected error", http.StatusInternalServerError)

		return
	}

	handleForwardResponseServerMetadata(w, md)

	w.Header().Set("Content-Type", marshaler.ContentType())

	if err := handleForwardResponseOptions(ctx, w, nil, opts); err != nil {
		runtime.HTTPError(ctx, mux, marshaler, w, req, err)
		return
	}

	chunks := []goproto.Message{}

	for {
		resp, err := recv()
		if err == io.EOF {
			break
		}

		if err != nil {
			handleForwardResponseStreamError(marshaler, w, err)
			return
		}

		if err := handleForwardResponseOptions(ctx, w, resp, opts); err != nil {
			handleForwardResponseStreamError(marshaler, w, err)
			return
		}

		chunks = append(chunks, resp)
	}

	buf, err := marshaler.Marshal(chunks)
	if err != nil {
		grpclog.Infof("Failed to marshal response: %v", err)
		handleForwardResponseStreamError(marshaler, w, err)

		return
	}

	if _, err = w.Write(buf); err != nil {
		grpclog.Infof("Failed to send response: %v", err)
		return
	}

	f.Flush()
}

func handleForwardResponseServerMetadata(w http.ResponseWriter, md runtime.ServerMetadata) {
	for k, vs := range md.HeaderMD {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
}

func handleForwardResponseOptions(
	ctx context.Context,
	w http.ResponseWriter,
	resp proto.Message,
	opts []func(context.Context, http.ResponseWriter, goproto.Message) error,
) error {
	if len(opts) == 0 {
		return nil
	}

	for _, opt := range opts {
		if err := opt(ctx, w, resp); err != nil {
			grpclog.Infof("Error handling ForwardResponseOptions: %v", err)
			return err
		}
	}

	return nil
}

func handleForwardResponseStreamError(marshaler runtime.Marshaler, w http.ResponseWriter, err error) {
	buf, merr := marshaler.Marshal(streamChunk(nil, err))
	if merr != nil {
		grpclog.Infof("Failed to marshal an error: %v", merr)
		return
	}

	s, ok := status.FromError(err)
	if !ok {
		s = status.New(codes.Unknown, err.Error())
	}

	w.WriteHeader(runtime.HTTPStatusFromCode(s.Code()))

	if _, werr := w.Write(buf); werr != nil {
		grpclog.Infof("Failed to notify error to client: %v", werr)
		return
	}
}

func streamChunk(result proto.Message, err error) map[string]proto.Message {
	if err != nil {
		grpcCode := codes.Unknown
		grpcMessage := err.Error()

		var grpcDetails []*types.Any

		if s, ok := status.FromError(err); ok {
			grpcCode = s.Code()
			grpcMessage = s.Message()

			if s.Proto() != nil {
				grpcDetails = make([]*types.Any, len(s.Proto().GetDetails()))
				for i, d := range s.Proto().GetDetails() {
					grpcDetails[i] = &types.Any{
						TypeUrl: d.GetTypeUrl(),
						Value:   d.GetValue(),
					}
				}
			}
		}

		httpCode := runtime.HTTPStatusFromCode(grpcCode)

		return map[string]proto.Message{
			"error": &StreamError{
				GrpcCode:   int32(grpcCode),
				HttpCode:   int32(httpCode),
				Message:    grpcMessage,
				HttpStatus: http.StatusText(httpCode),
				Details:    grpcDetails,
			},
		}
	}

	if result == nil {
		return streamChunk(nil, fmt.Errorf("empty response"))
	}

	return map[string]proto.Message{"result": result}
}
