package analytics

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

var (
	seed = &ClusterSeed{}
	rw   sync.RWMutex
)

func setSeed(s *ClusterSeed) {
	rw.Lock()
	defer rw.Unlock()
	seed = s
}

func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		rw.RLock()
		defer rw.RUnlock()
		report := buildReport(seed, time.Now())
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(report); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})
}
