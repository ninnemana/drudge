package drudge

import (
	"net/http"
	"path"
	"strings"

	"github.com/golang/glog"
	"go.uber.org/zap"
)

// swaggerServer returns swagger specification files located under "/swagger/"
func swaggerServer(dir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		glog.Infof("Serving %s", r.URL.Path)
		p := strings.TrimPrefix(r.URL.Path, "/openapi/")
		p = path.Join(dir, p)
		http.ServeFile(w, r, p)
	}
}

// allowCORS allows Cross Origin Resoruce Sharing from any origin.
// Don't do this without consideration in production systems.
func allowCORS(lg *zap.Logger, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			if r.Method == "OPTIONS" && r.Header.Get("Access-Control-Request-Method") != "" {
				preflightHandler(lg, w, r)
				return
			}
		}
		h.ServeHTTP(w, r)
	})
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
