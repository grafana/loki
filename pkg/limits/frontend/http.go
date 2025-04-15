package frontend

import (
	"encoding/json"
	"net/http"
	"text/template"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
)

const ringStreamUsageTemplate = `
<!DOCTYPE html>
<html>
<head>
    <title>Ring Stream Usage Cache</title>
    <style>
        table { border-collapse: collapse; width: 100%; }
        th, td { border: 1px solid #ddd; padding: 8px; text-align: left; }
        th { background-color: #f2f2f2; }
        form { display: inline; }
        button { padding: 5px 10px; margin: 2px; }
    </style>
</head>
<body>
    <h1>Ring Stream Usage Cache</h1>
    <form method="POST">
        <button type="submit">Clear All Cache</button>
    </form>
    <table>
        <tr>
            <th>Instance Address</th>
            <th>Partitions</th>
            <th>Actions</th>
        </tr>
        {{range $addr, $partitions := .Entries}}
        <tr>
            <td>{{$addr}}</td>
            <td>{{range $i, $p := $partitions}}{{if $i}}, {{end}}{{$p}}{{end}}</td>
            <td>
                <form method="POST">
                    <input type="hidden" name="instance" value="{{$addr}}">
                    <button type="submit">Clear Cache</button>
                </form>
            </td>
        </tr>
        {{end}}
    </table>
</body>
</html>`

type httpExceedsLimitsRequest struct {
	TenantID     string   `json:"tenantID"`
	StreamHashes []uint64 `json:"streamHashes"`
}

type httpExceedsLimitsResponse struct {
	Results []*logproto.ExceedsLimitsResult `json:"results,omitempty"`
}

// ServeHTTP implements http.Handler.
func (f *Frontend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req httpExceedsLimitsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "JSON is invalid or does not match expected schema", http.StatusBadRequest)
		return
	}

	if req.TenantID == "" {
		http.Error(w, "tenantID is required", http.StatusBadRequest)
		return
	}

	streams := make([]*logproto.StreamMetadata, 0, len(req.StreamHashes))
	for _, streamHash := range req.StreamHashes {
		streams = append(streams, &logproto.StreamMetadata{
			StreamHash: streamHash,
		})
	}
	protoReq := &logproto.ExceedsLimitsRequest{
		Tenant:  req.TenantID,
		Streams: streams,
	}

	ctx, err := user.InjectIntoGRPCRequest(user.InjectOrgID(r.Context(), req.TenantID))
	if err != nil {
		http.Error(w, "failed to inject org ID", http.StatusInternalServerError)
		return
	}

	resp, err := f.ExceedsLimits(ctx, protoReq)
	if err != nil {
		level.Error(f.logger).Log("msg", "failed to check limits", "err", err)
		http.Error(w, "an unexpected error occurred while checking limits", http.StatusInternalServerError)
		return
	}

	util.WriteJSONResponse(w, httpExceedsLimitsResponse{
		Results: resp.Results,
	})
}

// PartitionConsumersCacheHandler handles the GET request to display the cache.
func (f *Frontend) PartitionConsumersCacheHandler(w http.ResponseWriter, _ *http.Request) {
	data := struct {
		Entries map[string][]int32
	}{
		Entries: make(map[string][]int32),
	}

	for addr, entry := range f.partitionIDCache.Items() {
		assignedPartitions := entry.Value().AssignedPartitions
		for partition := range assignedPartitions {
			data.Entries[addr] = append(data.Entries[addr], partition)
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl := template.Must(template.New("cache").Parse(ringStreamUsageTemplate))
	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, "Failed to render template", http.StatusInternalServerError)
		return
	}
}

// PartitionConsumersCacheEvictHandler handles the POST request to clear the cache.
func (f *Frontend) PartitionConsumersCacheEvictHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	instance := r.FormValue("instance")
	if instance == "" {
		// Clear all cache
		f.partitionIDCache.DeleteAll()
	} else {
		// Clear specific instance
		f.partitionIDCache.Delete(instance)
	}

	// Redirect back to the GET page
	http.Redirect(w, r, r.URL.Path, http.StatusSeeOther)
}
