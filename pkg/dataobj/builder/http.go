package builder

import (
	"net/http"

	"github.com/go-kit/log/level"
)

// PrepareDownscaleHandler is a special handler called by the rollout operator
// immediately before the pod is downscaled. It can stop a downscale by
// responding with a non 2xx status code.
func (s *Service) PrepareDownscaleHandler(w http.ResponseWriter, r *http.Request) {
	isDownscalePermitted, err := s.downscalePermitted(r.Context())
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to check if downscale is permitted", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if !isDownscalePermitted {
		w.WriteHeader(http.StatusBadRequest)
	}
}
