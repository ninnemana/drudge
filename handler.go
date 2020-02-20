package drudge

import (
	"net/http"
	"path"
	"strings"

	"go.uber.org/zap"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// swaggerServer returns swagger specification files located under "/swagger/"
func swaggerServer(lg *zap.Logger, dir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lg.Info("Serving swagger", zap.String("path", r.URL.Path))
		p := strings.TrimPrefix(r.URL.Path, "/openapi/")
		p = path.Join(dir, p)
		http.ServeFile(w, r, p)
	}
}

// allowCORS allows Cross Origin Resoruce Sharing from any origin.
// Don't do this without consideration in production systems.
func allowCORS(lg *zap.Logger, rest, rpc http.Handler) http.Handler {
	return h2c.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 2 && strings.Contains(r.Header.Get("Content-Type"), "application/grpc") {
			rpc.ServeHTTP(w, r)
		} else {
			lg.Info("routing to HTTP", zap.String("referer", r.URL.String()))
			if origin := r.Header.Get("Origin"); origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				if r.Method == "OPTIONS" && r.Header.Get("Access-Control-Request-Method") != "" {
					preflightHandler(lg, w, r)
					return
				}
			}
			rest.ServeHTTP(w, r)
		}
	}), &http2.Server{})
}

// preflightHandler adds the necessary headers in order to serve
// CORS from any origin using the methods "GET", "HEAD", "POST", "PUT", "DELETE"
// We insist, don't do this without consideration in production systems.
func preflightHandler(lg *zap.Logger, w http.ResponseWriter, r *http.Request) {
	headers := []string{"Content-Type", "Accept"}
	w.Header().Set("Access-Control-Allow-Headers", strings.Join(headers, ","))

	methods := []string{"GET", "HEAD", "POST", "PUT", "DELETE"}
	w.Header().Set("Access-Control-Allow-Methods", strings.Join(methods, ","))
	lg.Info("preflight request", zap.String("path", r.URL.Path))
}
