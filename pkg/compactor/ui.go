package compactor

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/grafana/dskit/middleware"

	"github.com/grafana/loki/v3/pkg/compactor/deletion"
)

func (c *Compactor) Handler() (string, http.Handler) {
	mux := http.NewServeMux()

	mw := middleware.Merge(
		middleware.AuthenticateUser,
		deletion.TenantMiddleware(c.limits),
		// Automatically parse form data
		middleware.Func(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				next.ServeHTTP(w, r)
			})
		}),
	)

	// API endpoints
	mux.Handle("/compactor/ring", c.ring)
	// Custom UI endpoints for the compactor
	mux.HandleFunc("/compactor/ui/api/v1/deletes", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			c.handleListDeleteRequests(w, r)
		case http.MethodPost:
			mw.Wrap(http.HandlerFunc(c.DeleteRequestsHandler.AddDeleteRequestHandler)).ServeHTTP(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	return "/compactor", mux
}

type DeleteRequestResponse struct {
	RequestID    string `json:"request_id"`
	StartTime    int64  `json:"start_time"`
	EndTime      int64  `json:"end_time"`
	Query        string `json:"query"`
	Status       string `json:"status"`
	CreatedAt    int64  `json:"created_at"`
	UserID       string `json:"user_id"`
	DeletedLines int32  `json:"deleted_lines"`
}

func (c *Compactor) handleListDeleteRequests(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = string(deletion.StatusReceived)
	}

	ctx := r.Context()
	if c.deleteRequestsStore == nil {
		http.Error(w, "Retention is not enabled", http.StatusBadRequest)
		return
	}
	requests, err := c.deleteRequestsStore.GetAllRequests(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Filter requests by status
	filtered := requests[:0]
	for _, req := range requests {
		if req.Status == deletion.DeleteRequestStatus(status) {
			filtered = append(filtered, req)
		}
	}
	requests = filtered

	// Sort by creation time descending
	sort.Slice(requests, func(i, j int) bool {
		return requests[i].CreatedAt > requests[j].CreatedAt
	})

	// Take only last 100
	if len(requests) > 100 {
		requests = requests[:100]
	}

	response := make([]DeleteRequestResponse, 0, len(requests))
	for _, req := range requests {
		response = append(response, DeleteRequestResponse{
			RequestID:    req.RequestID,
			StartTime:    int64(req.StartTime),
			EndTime:      int64(req.EndTime),
			Query:        req.Query,
			Status:       string(req.Status),
			CreatedAt:    int64(req.CreatedAt),
			UserID:       req.UserID,
			DeletedLines: req.DeletedLines,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
