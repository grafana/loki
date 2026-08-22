package explorer

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/thanos-io/objstore"
)

// errInvalidObjectKey is returned for a request key that could escape the
// configured object store root. The filesystem backend joins the key onto its
// root directory with no containment, so a key like "../../etc/passwd" would
// otherwise resolve to an arbitrary path on the host.
var errInvalidObjectKey = errors.New("invalid path: must stay within the object store root")

// validateObjectKey rejects absolute keys and keys with ".." segments that
// traverse outside the bucket root. filepath.IsLocal mirrors the filepath.Join
// the filesystem backend uses, so it accepts ordinary object keys and rejects
// exactly the keys that would escape.
func validateObjectKey(key string) error {
	if !filepath.IsLocal(key) {
		return errInvalidObjectKey
	}
	return nil
}

type Service struct {
	*services.BasicService

	bucket objstore.Bucket
	logger log.Logger
}

func New(bucket objstore.Bucket, logger log.Logger) (*Service, error) {
	s := &Service{
		bucket: bucket,
		logger: logger,
	}

	s.BasicService = services.NewBasicService(nil, s.running, nil)
	return s, nil
}

func (s *Service) running(ctx context.Context) error {
	level.Info(s.logger).Log("msg", "dataobj explorer is running")
	<-ctx.Done()
	return nil
}

func (s *Service) Handler() (string, http.Handler) {
	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/dataobj/api/v1/list", s.handleList)
	mux.HandleFunc("/dataobj/api/v1/inspect", s.handleInspect)
	mux.HandleFunc("/dataobj/api/v1/download", s.handleDownload)
	mux.HandleFunc("/dataobj/api/v1/provider", s.handleProvider)

	return "/dataobj", mux
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
