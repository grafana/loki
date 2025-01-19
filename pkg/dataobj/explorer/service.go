package explorer

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"path"
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

type listResponse struct {
	Files   []fileInfo `json:"files"`
	Folders []string   `json:"folders"`
	Parent  string     `json:"parent"`
	Current string     `json:"current"`
}

type fileInfo struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

func New(bucket objstore.Bucket, logger log.Logger) (*Service, error) {
	// Get the embedded UI filesystem
	ui, err := fs.Sub(uiFS, "dist")
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

	// Serve static files from embedded filesystem
	fsHandler := http.FileServer(http.FS(s.uiFS))
	mux.Handle("/dataobj/explorer/", http.StripPrefix("/dataobj/explorer/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If the path doesn't exist, serve index.html
		if _, err := s.uiFS.Open(strings.TrimPrefix(r.URL.Path, "/")); err != nil {
			r.URL.Path = "/"
		}
		fsHandler.ServeHTTP(w, r)
	})))

	return mux
}

func (s *Service) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get path from query parameter and ensure it's not null
	dir := r.URL.Query().Get("path")
	if dir == "" {
		dir = "/"
	}
	level.Debug(s.logger).Log("msg", "listing directory", "path", dir)

	// Initialize empty slices instead of null
	files := make([]fileInfo, 0)
	folders := make([]string, 0)

	// List objects in the bucket
	err := s.bucket.Iter(r.Context(), dir, func(name string) error {
		// Skip the current directory
		if name == dir {
			return nil
		}

		// Get the path relative to the current directory
		relativePath := strings.TrimPrefix(name, dir)
		relativePath = strings.TrimPrefix(relativePath, "/")

		if strings.HasSuffix(name, "/") {
			// This is a folder
			folderName := strings.TrimSuffix(relativePath, "/")
			if folderName != "" { // Only add non-empty folder names
				folders = append(folders, folderName)
			}
		} else {
			// This is a file
			attrs, err := s.bucket.Attributes(r.Context(), name)
			if err != nil {
				level.Error(s.logger).Log("msg", "failed to get object attributes", "name", name, "err", err)
				return nil
			}

			files = append(files, fileInfo{
				Name: relativePath,
				Size: attrs.Size,
			})
		}
		return nil
	})
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to list objects", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Calculate parent directory
	parent := path.Dir(dir)
	if parent == "." || parent == "/" {
		parent = ""
	}

	// Ensure current path is never empty
	current := dir
	if current == "/" {
		current = ""
	}

	resp := listResponse{
		Files:   files,
		Folders: folders,
		Parent:  parent,
		Current: current,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		level.Error(s.logger).Log("msg", "failed to encode response", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
