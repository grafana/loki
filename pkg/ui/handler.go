// Package ui provides HTTP handlers for the Loki UI and cluster management interface.
package ui

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"strings"

	"github.com/go-kit/log/level"
	"github.com/gorilla/mux"

	"github.com/grafana/loki/v3/pkg/analytics"
)

const (
	proxyScheme     = "http"
	prefixPath      = "/ui"
	proxyPath       = prefixPath + "/api/v1/proxy/{nodename}/"
	clusterPath     = prefixPath + "/api/v1/cluster/nodes"
	clusterSelfPath = prefixPath + "/api/v1/cluster/nodes/self/details"
	analyticsPath   = prefixPath + "/api/v1/analytics"
	notFoundPath    = prefixPath + "/api/v1/404"
	contentTypeJSON = "application/json"
)

//go:embed frontend/dist
var uiFS embed.FS

// RegisterHandler registers all UI API routes with the provided router.
func (s *Service) RegisterHandler() {
	// Register the node handler
	route, handler := s.node.Handler()
	s.router.PathPrefix(route).Handler(handler)

	s.router.Path(analyticsPath).Handler(analytics.Handler())
	s.router.Path(clusterPath).Handler(s.clusterMembersHandler())
	s.router.Path(clusterSelfPath).Handler(s.clusterSelfHandler())

	s.router.PathPrefix(proxyPath).Handler(s.clusterProxyHandler())
	s.router.PathPrefix(notFoundPath).Handler(s.notFoundHandler())

	fsHandler := http.FileServer(http.FS(s.uiFS))
	s.router.PathPrefix(prefixPath + "/").Handler(http.StripPrefix(prefixPath+"/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		// Don't redirect for root UI path
		if path == "" || path == "/" || path == "404" {
			r.URL.Path = "/"
			fsHandler.ServeHTTP(w, r)
			return
		}
		if _, err := s.uiFS.Open(path); err != nil {
			r.URL.Path = "/"
			fsHandler.ServeHTTP(w, r)
			return
		}
		fsHandler.ServeHTTP(w, r)
	})))
	s.router.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/404?path="+r.URL.Path, http.StatusTemporaryRedirect)
	})
}

func (s *Service) initUIFs() error {
	var err error
	s.uiFS, err = fs.Sub(uiFS, "frontend/dist")
	if err != nil {
		return err
	}
	return nil
}

// clusterProxyHandler returns a handler that proxies requests to the target node.
func (s *Service) clusterProxyHandler() http.Handler {
	proxy := &httputil.ReverseProxy{
		Transport: s.client.Transport,
		Director: func(r *http.Request) {
			r.URL.Scheme = proxyScheme
			vars := mux.Vars(r)
			nodeName := vars["nodename"]
			if nodeName == "" {
				level.Error(s.logger).Log("msg", "node name not found in URL")
				s.redirectToNotFound(r, nodeName)
				return
			}

			peer, err := s.findPeerByName(nodeName)
			if err != nil {
				level.Warn(s.logger).Log("msg", "node not found in cluster state", "node", nodeName, "err", err)
				s.redirectToNotFound(r, nodeName)
				return
			}

			// Calculate the path without the proxy prefix
			trimPrefix := fmt.Sprintf("/ui/api/v1/proxy/%s", nodeName)
			newPath := strings.TrimPrefix(r.URL.Path, trimPrefix)
			if newPath == "" {
				newPath = "/"
			}

			// Rewrite the URL to forward to the target node
			r.URL.Host = peer.Addr
			r.URL.Path = newPath
			r.RequestURI = "" // Must be cleared according to Go docs

			level.Debug(s.logger).Log(
				"msg", "proxying request",
				"node", nodeName,
				"target", r.URL.String(),
				"original_path", r.URL.Path,
			)
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			level.Error(s.logger).Log("msg", "proxy error", "err", err, "path", r.URL.Path)
			s.writeJSONError(w, http.StatusBadGateway, err.Error())
		},
	}
	return proxy
}

func (s *Service) clusterMembersHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state, err := s.fetchClusterMembers(r.Context())
		if err != nil {
			level.Error(s.logger).Log("msg", "failed to fetch cluster state", "err", err)
			s.writeJSONError(w, http.StatusInternalServerError, "failed to fetch cluster state")
			return
		}
		w.Header().Set("Content-Type", contentTypeJSON)
		if err := json.NewEncoder(w).Encode(state); err != nil {
			level.Error(s.logger).Log("msg", "failed to encode cluster state", "err", err)
			s.writeJSONError(w, http.StatusInternalServerError, "failed to encode response")
			return
		}
	})
}

func (s *Service) clusterSelfHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state, err := s.fetchSelfDetails(r.Context())
		if err != nil {
			level.Error(s.logger).Log("msg", "failed to fetch node details", "err", err)
			s.writeJSONError(w, http.StatusInternalServerError, "failed to fetch node details")
			return
		}
		w.Header().Set("Content-Type", contentTypeJSON)
		if err := json.NewEncoder(w).Encode(state); err != nil {
			level.Error(s.logger).Log("msg", "failed to encode node details", "err", err)
			s.writeJSONError(w, http.StatusInternalServerError, "failed to encode response")
			return
		}
	})
}

func (s *Service) notFoundHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		node := r.URL.Query().Get("node")
		s.writeJSONError(w, http.StatusNotFound, fmt.Sprintf("node %s not found", node))
	})
}

// redirectToNotFound updates the request URL to redirect to the not found handler
func (s *Service) redirectToNotFound(r *http.Request, nodeName string) {
	r.URL.Path = notFoundPath
	r.URL.RawQuery = "?node=" + nodeName
}

// writeJSONError writes a JSON error response with the given status code and message
func (s *Service) writeJSONError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": message}); err != nil {
		level.Error(s.logger).Log("msg", "failed to encode error response", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
