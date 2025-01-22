package explorer

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/thanos-io/objstore"
)

// isDev is set via build flag -tags dev
var isDev bool

//go:embed dist
var uiFS embed.FS

type Service struct {
	*services.BasicService

	bucket   objstore.Bucket
	logger   log.Logger
	uiFS     fs.FS
	devProxy *httputil.ReverseProxy
}

func New(bucket objstore.Bucket, logger log.Logger) (*Service, error) {
	var ui fs.FS
	var devProxy *httputil.ReverseProxy

	if isDev {
		// In dev mode, we'll proxy to the Vite dev server
		devServerURL, err := url.Parse("http://localhost:5173")
		if err != nil {
			return nil, err
		}
		devProxy = httputil.NewSingleHostReverseProxy(devServerURL)
		ui = os.DirFS(filepath.Join("pkg", "dataobj", "explorer", "ui", "dist")) // Fallback
	} else {
		// In production, use the embedded filesystem
		var err error
		ui, err = fs.Sub(uiFS, "dist")
		if err != nil {
			return nil, err
		}
	}

	s := &Service{
		bucket:   bucket,
		logger:   logger,
		uiFS:     ui,
		devProxy: devProxy,
	}

	s.BasicService = services.NewBasicService(nil, s.running, nil)
	return s, nil
}

func (s *Service) running(ctx context.Context) error {
	mode := "production"
	if isDev {
		mode = "development"
	}
	level.Info(s.logger).Log("msg", "dataobj explorer is running", "mode", mode)
	<-ctx.Done()
	return nil
}

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/dataobj/explorer/api/list", s.handleList)
	mux.HandleFunc("/dataobj/explorer/api/inspect", s.handleInspect)
	mux.HandleFunc("/dataobj/explorer/api/download", s.handleDownload)

	// Serve UI
	if isDev && s.devProxy != nil {
		// In dev mode, proxy to Vite dev server
		mux.Handle("/dataobj/explorer/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s.devProxy.ServeHTTP(w, r)
		}))
	} else {
		// In production, serve from embedded filesystem
		fsHandler := http.FileServer(http.FS(s.uiFS))
		mux.Handle("/dataobj/explorer/", http.StripPrefix("/dataobj/explorer/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, err := s.uiFS.Open(strings.TrimPrefix(r.URL.Path, "/")); err != nil {
				r.URL.Path = "/"
			}
			fsHandler.ServeHTTP(w, r)
		})))
	}

	return mux
}
