package explorer

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/thanos-io/objstore"
)

//go:embed dist
var uiFS embed.FS

type Service struct {
	*services.BasicService

	bucket objstore.Bucket
	logger log.Logger
	uiFS   fs.FS
}

func New(bucket objstore.Bucket, logger log.Logger) (*Service, error) {
	var ui fs.FS

	var err error
	ui, err = fs.Sub(uiFS, "dist")
	if err != nil {
		return nil, err
	}

	s := &Service{
		bucket: bucket,
		logger: logger,
		uiFS:   ui,
	}

	s.BasicService = services.NewBasicService(nil, s.running, nil)
	return s, nil
}

func (s *Service) running(ctx context.Context) error {
	level.Info(s.logger).Log("msg", "dataobj explorer is running")
	<-ctx.Done()
	return nil
}

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/dataobj/explorer/api/list", s.handleList)
	mux.HandleFunc("/dataobj/explorer/api/inspect", s.handleInspect)
	mux.HandleFunc("/dataobj/explorer/api/download", s.handleDownload)
	mux.HandleFunc("/dataobj/explorer/api/provider", s.handleProvider)

	fsHandler := http.FileServer(http.FS(s.uiFS))
	mux.Handle("/dataobj/explorer/", http.StripPrefix("/dataobj/explorer/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := s.uiFS.Open(strings.TrimPrefix(r.URL.Path, "/")); err != nil {
			r.URL.Path = "/"
		}
		fsHandler.ServeHTTP(w, r)
	})))

	return mux
}

func (s *Service) handleProvider(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	provider := s.bucket.Provider()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"provider": string(provider)}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
