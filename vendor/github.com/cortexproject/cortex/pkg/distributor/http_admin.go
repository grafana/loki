package distributor

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
)

const tpl = `
<!DOCTYPE html>
<html>
	<head>
		<meta charset="UTF-8">
		<title>Cortex Ingester Stats</title>
	</head>
	<body>
		<h1>Cortex Ingester Stats</h1>
		<p>Current time: {{ .Now }}</p>
		<p><b>NB stats do not account for replication factor, which is currently set to {{ .ReplicationFactor }}</b></p>
		<form action="" method="POST">
			<input type="hidden" name="csrf_token" value="$__CSRF_TOKEN_PLACEHOLDER__">
			<table border="1">
				<thead>
					<tr>
						<th>User</th>
						<th># Series</th>
						<th>Total Ingest Rate</th>
						<th>API Ingest Rate</th>
						<th>Rule Ingest Rate</th>
					</tr>
				</thead>
				<tbody>
					{{ range .Stats }}
					<tr>
						<td>{{ .UserID }}</td>
						<td align='right'>{{ .UserStats.NumSeries }}</td>
						<td align='right'>{{ printf "%.2f" .UserStats.IngestionRate }}</td>
						<td align='right'>{{ printf "%.2f" .UserStats.APIIngestionRate }}</td>
						<td align='right'>{{ printf "%.2f" .UserStats.RuleIngestionRate }}</td>
					</tr>
					{{ end }}
				</tbody>
			</table>
		</form>
	</body>
</html>`

var tmpl *template.Template

func init() {
	tmpl = template.Must(template.New("webpage").Parse(tpl))
}

type userStatsByTimeseries []UserIDStats

func (s userStatsByTimeseries) Len() int      { return len(s) }
func (s userStatsByTimeseries) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

func (s userStatsByTimeseries) Less(i, j int) bool {
	return s[i].NumSeries > s[j].NumSeries ||
		(s[i].NumSeries == s[j].NumSeries && s[i].UserID < s[j].UserID)
}

// AllUserStatsHandler shows stats for all users.
func (d *Distributor) AllUserStatsHandler(w http.ResponseWriter, r *http.Request) {
	stats, err := d.AllUserStats(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sort.Sort(userStatsByTimeseries(stats))

	if encodings, found := r.Header["Accept"]; found &&
		len(encodings) > 0 && strings.Contains(encodings[0], "json") {
		if err := json.NewEncoder(w).Encode(stats); err != nil {
			http.Error(w, fmt.Sprintf("Error marshalling response: %v", err), http.StatusInternalServerError)
		}
		return
	}

	util.RenderHTTPResponse(w, struct {
		Now               time.Time     `json:"now"`
		Stats             []UserIDStats `json:"stats"`
		ReplicationFactor int           `json:"replicationFactor"`
	}{
		Now:               time.Now(),
		Stats:             stats,
		ReplicationFactor: d.ingestersRing.ReplicationFactor(),
	}, tmpl, r)
}
