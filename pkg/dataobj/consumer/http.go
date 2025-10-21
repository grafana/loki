package consumer

import (
	"net/http"

	"github.com/go-kit/log/level"
)

func (s *Service) CatchAllHandler(w http.ResponseWriter, r *http.Request) {
	level.Info(s.logger).Log("msg", "catch all handler called", "method", r.Method, "path", r.URL.Path)
}
