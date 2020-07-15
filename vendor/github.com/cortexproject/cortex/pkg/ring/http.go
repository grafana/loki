package ring

import (
	"context"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/go-kit/kit/log/level"

	"github.com/cortexproject/cortex/pkg/util"
)

const pageContent = `
<!DOCTYPE html>
<html>
	<head>
		<meta charset="UTF-8">
		<title>Cortex Ring Status</title>
	</head>
	<body>
		<h1>Cortex Ring Status</h1>
		<p>Current time: {{ .Now }}</p>
		<form action="" method="POST">
			<input type="hidden" name="csrf_token" value="$__CSRF_TOKEN_PLACEHOLDER__">
			<table width="100%" border="1">
				<thead>
					<tr>
						<th>Instance ID</th>
						<th>Availability Zone</th>
						<th>State</th>
						<th>Address</th>
						<th>Last Heartbeat</th>
						<th>Tokens</th>
						<th>Ownership</th>
						<th>Actions</th>
					</tr>
				</thead>
				<tbody>
					{{ range $i, $ing := .Ingesters }}
					{{ if mod $i 2 }}
					<tr>
					{{ else }}
					<tr bgcolor="#BEBEBE">
					{{ end }}
						<td>{{ .ID }}</td>
						<td>{{ .Zone }}</td>
						<td>{{ .State }}</td>
						<td>{{ .Address }}</td>
						<td>{{ .Timestamp }}</td>
						<td>{{ .NumTokens }}</td>
						<td>{{ .Ownership }}%</td>
						<td><button name="forget" value="{{ .ID }}" type="submit">Forget</button></td>
					</tr>
					{{ end }}
				</tbody>
			</table>
			<br>
			{{ if .ShowTokens }}
			<input type="button" value="Hide Tokens" onclick="window.location.href = '?tokens=false' " />
			{{ else }}
			<input type="button" value="Show Tokens" onclick="window.location.href = '?tokens=true'" />
			{{ end }}

			{{ if .ShowTokens }}
				{{ range $i, $ing := .Ingesters }}
					<h2>Instance: {{ .ID }}</h2>
					<p>
						Tokens:<br />
						{{ range $token := .Tokens }}
							{{ $token }}
						{{ end }}
					</p>
				{{ end }}
			{{ end }}
		</form>
	</body>
</html>`

var pageTemplate *template.Template

func init() {
	t := template.New("webpage")
	t.Funcs(template.FuncMap{"mod": func(i, j int) bool { return i%j == 0 }})
	pageTemplate = template.Must(t.Parse(pageContent))
}

func (r *Ring) forget(ctx context.Context, id string) error {
	unregister := func(in interface{}) (out interface{}, retry bool, err error) {
		if in == nil {
			return nil, false, fmt.Errorf("found empty ring when trying to unregister")
		}

		ringDesc := in.(*Desc)
		ringDesc.RemoveIngester(id)
		return ringDesc, true, nil
	}
	return r.KVClient.CAS(ctx, r.key, unregister)
}

func (r *Ring) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method == http.MethodPost {
		ingesterID := req.FormValue("forget")
		if err := r.forget(req.Context(), ingesterID); err != nil {
			level.Error(util.WithContext(req.Context(), util.Logger)).Log("msg", "error forgetting instance", "err", err)
		}

		// Implement PRG pattern to prevent double-POST and work with CSRF middleware.
		// https://en.wikipedia.org/wiki/Post/Redirect/Get

		// http.Redirect() would convert our relative URL to absolute, which is not what we want.
		// Browser knows how to do that, and it also knows real URL. Furthermore it will also preserve tokens parameter.
		// Note that relative Location URLs are explicitly allowed by specification, so we're not doing anything wrong here.
		w.Header().Set("Location", "#")
		w.WriteHeader(http.StatusFound)

		return
	}

	r.mtx.RLock()
	defer r.mtx.RUnlock()

	ingesterIDs := []string{}
	for id := range r.ringDesc.Ingesters {
		ingesterIDs = append(ingesterIDs, id)
	}
	sort.Strings(ingesterIDs)

	ingesters := []interface{}{}
	_, owned := countTokens(r.ringDesc, r.ringTokens)
	for _, id := range ingesterIDs {
		ing := r.ringDesc.Ingesters[id]
		timestamp := time.Unix(ing.Timestamp, 0)
		state := ing.State.String()
		if !r.IsHealthy(&ing, Reporting) {
			state = unhealthy
		}

		ingesters = append(ingesters, struct {
			ID        string   `json:"id"`
			State     string   `json:"state"`
			Address   string   `json:"address"`
			Timestamp string   `json:"timestamp"`
			Zone      string   `json:"zone"`
			Tokens    []uint32 `json:"tokens"`
			NumTokens int      `json:"-"`
			Ownership float64  `json:"-"`
		}{
			ID:        id,
			State:     state,
			Address:   ing.Addr,
			Timestamp: timestamp.String(),
			Tokens:    ing.Tokens,
			Zone:      ing.Zone,
			NumTokens: len(ing.Tokens),
			Ownership: (float64(owned[id]) / float64(math.MaxUint32)) * 100,
		})
	}

	tokensParam := req.URL.Query().Get("tokens")

	util.RenderHTTPResponse(w, struct {
		Ingesters  []interface{} `json:"shards"`
		Now        time.Time     `json:"now"`
		ShowTokens bool          `json:"-"`
	}{
		Ingesters:  ingesters,
		Now:        time.Now(),
		ShowTokens: tokensParam == "true",
	}, pageTemplate, req)
}
