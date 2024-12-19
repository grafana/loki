package usage

import (
	"html/template"
	"net/http"
	"sort"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/go-kit/log/level"
)

type streamRate struct {
	TenantID string
	Hash     uint64
	BytesPS  string
}

type tenantRate struct {
	TenantID string
	BytesPS  string
}

type partitionView struct {
	Partition      int32
	Offset         int64
	LastUpdate     string
	LastUpdateTime string
	TopStreams     []streamRate
}

var statsTemplate = template.Must(template.New("stats").Parse(`
<!DOCTYPE html>
<html>
<head>
    <title>Usage Statistics</title>
    <style>
        body { font-family: sans-serif; margin: 20px; }
        .partition { margin-bottom: 30px; }
        .partition h2 { color: #333; }
        .partition .meta { color: #666; margin-bottom: 10px; }
        table { border-collapse: collapse; width: 100%; max-width: 800px; }
        th, td { padding: 8px; text-align: left; border-bottom: 1px solid #ddd; }
        th { background-color: #f5f5f5; }
        tr:hover { background-color: #f9f9f9; }
        .tenant-totals { margin-bottom: 30px; }
    </style>
</head>
<body>
    <h1>Usage Statistics</h1>

    <div class="tenant-totals">
        <h2>Tenant Total Rates</h2>
        <table>
            <thead>
                <tr>
                    <th>Tenant ID</th>
                    <th>Total Rate</th>
                </tr>
            </thead>
            <tbody>
                {{range .TenantTotals}}
                <tr>
                    <td>{{.TenantID}}</td>
                    <td>{{.BytesPS}}/s</td>
                </tr>
                {{end}}
            </tbody>
        </table>
    </div>

    {{range .Partitions}}
    <div class="partition">
        <h2>Partition {{.Partition}}</h2>
        <div class="meta">
            Offset: {{.Offset}} | Last Update: {{.LastUpdate}} ({{.LastUpdateTime}})
        </div>
        <table>
            <thead>
                <tr>
                    <th>Tenant ID</th>
                    <th>Stream Hash</th>
                    <th>Rate</th>
                </tr>
            </thead>
            <tbody>
                {{range .TopStreams}}
                <tr>
                    <td>{{.TenantID}}</td>
                    <td>{{.Hash}}</td>
                    <td>{{.BytesPS}}/s</td>
                </tr>
                {{end}}
            </tbody>
        </table>
    </div>
    {{end}}
</body>
</html>
`))

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.statsMtx.RLock()
	defer s.statsMtx.RUnlock()

	// Calculate rates for the last minute
	window := time.Minute
	since := time.Now().Add(-window)

	// Track tenant total rates
	tenantTotals := make(map[string]float64)

	var partitions []partitionView
	for partition, pStats := range s.stats.stats {
		view := partitionView{
			Partition:      partition,
			Offset:         pStats.offset,
			LastUpdate:     humanize.Time(pStats.timestamp),
			LastUpdateTime: pStats.timestamp.Format("2006-01-02 15:04:05 MST"),
		}

		// Collect all stream rates for this partition
		var rates []streamRate
		for tenantID, tStats := range pStats.tenants {
			tenantTotal := float64(0)
			for hash, sStats := range tStats.streams {
				bytes := sStats.totalBytesSince(since)
				if bytes > 0 {
					bytesPerSec := float64(bytes) / window.Seconds()
					tenantTotal += bytesPerSec
					rates = append(rates, streamRate{
						TenantID: tenantID,
						Hash:     hash,
						BytesPS:  humanize.Bytes(uint64(bytesPerSec)),
					})
				}
			}
			tenantTotals[tenantID] += tenantTotal
		}

		// Sort by bytes per second in descending order
		sort.Slice(rates, func(i, j int) bool {
			bytesI, _ := humanize.ParseBytes(rates[i].BytesPS)
			bytesJ, _ := humanize.ParseBytes(rates[j].BytesPS)
			return bytesI > bytesJ
		})

		// Take top 10 streams
		if len(rates) > 10 {
			rates = rates[:10]
		}
		view.TopStreams = rates
		partitions = append(partitions, view)
	}

	// Convert tenant totals to sorted slice
	var tenantRates []tenantRate
	for tenantID, bytesPerSec := range tenantTotals {
		tenantRates = append(tenantRates, tenantRate{
			TenantID: tenantID,
			BytesPS:  humanize.Bytes(uint64(bytesPerSec)),
		})
	}

	// Sort tenant rates by bytes per second in descending order
	sort.Slice(tenantRates, func(i, j int) bool {
		bytesI, _ := humanize.ParseBytes(tenantRates[i].BytesPS)
		bytesJ, _ := humanize.ParseBytes(tenantRates[j].BytesPS)
		return bytesI > bytesJ
	})

	// Sort partitions by partition number
	sort.Slice(partitions, func(i, j int) bool {
		return partitions[i].Partition < partitions[j].Partition
	})

	// Render template
	err := statsTemplate.Execute(w, struct {
		Partitions   []partitionView
		TenantTotals []tenantRate
	}{
		Partitions:   partitions,
		TenantTotals: tenantRates,
	})
	if err != nil {
		level.Error(s.logger).Log("msg", "error executing template", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}
