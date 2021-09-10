package memberlist

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/hashicorp/memberlist"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/atomic"

	"github.com/grafana/dskit/services"
)

// KVInitService initializes a memberlist.KV on first call to GetMemberlistKV, and starts it. On stop,
// KV is stopped too. If KV fails, error is reported from the service.
type KVInitService struct {
	services.Service

	// config used for initialization
	cfg         *KVConfig
	logger      log.Logger
	dnsProvider DNSProvider
	registerer  prometheus.Registerer

	// init function, to avoid multiple initializations.
	init sync.Once

	// state
	kv      atomic.Value
	err     error
	watcher *services.FailureWatcher
}

func NewKVInitService(cfg *KVConfig, logger log.Logger, dnsProvider DNSProvider, registerer prometheus.Registerer) *KVInitService {
	kvinit := &KVInitService{
		cfg:         cfg,
		watcher:     services.NewFailureWatcher(),
		logger:      logger,
		registerer:  registerer,
		dnsProvider: dnsProvider,
	}
	kvinit.Service = services.NewBasicService(nil, kvinit.running, kvinit.stopping).WithName("memberlist KV service")
	return kvinit
}

// GetMemberlistKV will initialize Memberlist.KV on first call, and add it to service failure watcher.
func (kvs *KVInitService) GetMemberlistKV() (*KV, error) {
	kvs.init.Do(func() {
		kv := NewKV(*kvs.cfg, kvs.logger, kvs.dnsProvider, kvs.registerer)
		kvs.watcher.WatchService(kv)
		kvs.err = kv.StartAsync(context.Background())

		kvs.kv.Store(kv)
	})

	return kvs.getKV(), kvs.err
}

// Returns KV if it was initialized, or nil.
func (kvs *KVInitService) getKV() *KV {
	kv := kvs.kv.Load()
	if kv == nil {
		return nil
	}
	return kv.(*KV)
}

func (kvs *KVInitService) running(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return nil
	case err := <-kvs.watcher.Chan():
		// Only happens if KV service was actually initialized in GetMemberlistKV and it fails.
		return err
	}
}

func (kvs *KVInitService) stopping(_ error) error {
	kv := kvs.getKV()
	if kv == nil {
		return nil
	}

	return services.StopAndAwaitTerminated(context.Background(), kv)
}

func (kvs *KVInitService) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	kv := kvs.getKV()
	if kv == nil {
		w.Header().Set("Content-Type", "text/plain")
		// Ignore inactionable errors.
		_, _ = w.Write([]byte("This instance doesn't use memberlist."))
		return
	}

	const (
		downloadKeyParam    = "downloadKey"
		viewKeyParam        = "viewKey"
		viewMsgParam        = "viewMsg"
		deleteMessagesParam = "deleteMessages"
	)

	if err := req.ParseForm(); err == nil {
		if req.Form[downloadKeyParam] != nil {
			downloadKey(w, kv, kv.storeCopy(), req.Form[downloadKeyParam][0]) // Use first value, ignore the rest.
			return
		}

		if req.Form[viewKeyParam] != nil {
			viewKey(w, kv.storeCopy(), req.Form[viewKeyParam][0], getFormat(req))
			return
		}

		if req.Form[viewMsgParam] != nil {
			msgID, err := strconv.Atoi(req.Form[viewMsgParam][0])
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			sent, received := kv.getSentAndReceivedMessages()

			for _, m := range append(sent, received...) {
				if m.ID == msgID {
					viewMessage(w, kv, m, getFormat(req))
					return
				}
			}

			http.Error(w, "message not found", http.StatusNotFound)
			return
		}

		if len(req.Form[deleteMessagesParam]) > 0 && req.Form[deleteMessagesParam][0] == "true" {
			kv.deleteSentReceivedMessages()

			// Redirect back.
			w.Header().Set("Location", "?"+deleteMessagesParam+"=false")
			w.WriteHeader(http.StatusFound)
			return
		}
	}

	members := kv.memberlist.Members()
	sort.Slice(members, func(i, j int) bool {
		return members[i].Name < members[j].Name
	})

	sent, received := kv.getSentAndReceivedMessages()

	v := pageData{
		Now:              time.Now(),
		Memberlist:       kv.memberlist,
		SortedMembers:    members,
		Store:            kv.storeCopy(),
		SentMessages:     sent,
		ReceivedMessages: received,
	}

	accept := req.Header.Get("Accept")
	if strings.Contains(accept, "application/json") {
		w.Header().Set("Content-Type", "application/json")

		data, err := json.Marshal(v)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// We ignore errors here, because we cannot do anything about them.
		// Write will trigger sending Status code, so we cannot send a different status code afterwards.
		// Also this isn't internal error, but error communicating with client.
		_, _ = w.Write(data)
		return
	}

	err := pageTemplate.Execute(w, v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func getFormat(req *http.Request) string {
	const viewFormat = "format"

	format := ""
	if len(req.Form[viewFormat]) > 0 {
		format = req.Form[viewFormat][0]
	}
	return format
}

func viewMessage(w http.ResponseWriter, kv *KV, msg message, format string) {
	c := kv.GetCodec(msg.Pair.Codec)
	if c == nil {
		http.Error(w, "codec not found", http.StatusNotFound)
		return
	}

	val, err := c.Decode(msg.Pair.Value)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to decode: %v", err), http.StatusInternalServerError)
		return
	}

	formatValue(w, val, format)
}

func viewKey(w http.ResponseWriter, store map[string]valueDesc, key string, format string) {
	if store[key].value == nil {
		http.Error(w, "value not found", http.StatusNotFound)
		return
	}

	formatValue(w, store[key].value, format)
}

func formatValue(w http.ResponseWriter, val interface{}, format string) {

	w.WriteHeader(200)
	w.Header().Add("content-type", "text/plain")

	switch format {
	case "json", "json-pretty":
		enc := json.NewEncoder(w)
		if format == "json-pretty" {
			enc.SetIndent("", "    ")
		}

		err := enc.Encode(val)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	default:
		_, _ = fmt.Fprintf(w, "%#v", val)
	}
}

func downloadKey(w http.ResponseWriter, kv *KV, store map[string]valueDesc, key string) {
	if store[key].value == nil {
		http.Error(w, "value not found", http.StatusNotFound)
		return
	}

	val := store[key]

	c := kv.GetCodec(store[key].codecID)
	if c == nil {
		http.Error(w, "codec not found", http.StatusNotFound)
		return
	}

	encoded, err := c.Encode(val.value)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to encode: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Add("content-type", "application/octet-stream")
	// Set content-length so that client knows whether it has received full response or not.
	w.Header().Add("content-length", strconv.Itoa(len(encoded)))
	w.Header().Add("content-disposition", fmt.Sprintf("attachment; filename=%d-%s", val.version, key))
	w.WriteHeader(200)

	// Ignore errors, we cannot do anything about them.
	_, _ = w.Write(encoded)
}

type pageData struct {
	Now              time.Time
	Memberlist       *memberlist.Memberlist
	SortedMembers    []*memberlist.Node
	Store            map[string]valueDesc
	SentMessages     []message
	ReceivedMessages []message
}

var pageTemplate = template.Must(template.New("webpage").Funcs(template.FuncMap{
	"StringsJoin": strings.Join,
}).Parse(pageContent))

const pageContent = `
<!DOCTYPE html>
<html>
	<head>
		<meta charset="UTF-8">
		<title>Memberlist Status</title>
	</head>
	<body>
		<h1>Memberlist Status</h1>
		<p>Current time: {{ .Now }}</p>

		<ul>
		<li>Health Score: {{ .Memberlist.GetHealthScore }} (lower = better, 0 = healthy)</li>
		<li>Members: {{ .Memberlist.NumMembers }}</li>
		</ul>

		<h2>KV Store</h2>

		<table width="100%" border="1">
			<thead>
				<tr>
					<th>Key</th>
					<th>Value Details</th>
					<th>Actions</th>
				</tr>
			</thead>

			<tbody>
				{{ range $k, $v := .Store }}
				<tr>
					<td>{{ $k }}</td>
					<td>{{ $v }}</td>
					<td>
						<a href="?viewKey={{ $k }}&format=json">json</a>
						| <a href="?viewKey={{ $k }}&format=json-pretty">json-pretty</a>
						| <a href="?viewKey={{ $k }}&format=struct">struct</a>
						| <a href="?downloadKey={{ $k }}">download</a>
					</td>
				</tr>
				{{ end }}
			</tbody>
		</table>

		<p>Note that value "version" is node-specific. It starts with 0 (on restart), and increases on each received update. Size is in bytes.</p>

		<h2>Memberlist Cluster Members</h2>

		<table width="100%" border="1">
			<thead>
				<tr>
					<th>Name</th>
					<th>Address</th>
					<th>State</th>
				</tr>
			</thead>

			<tbody>
				{{ range .SortedMembers }}
				<tr>
					<td>{{ .Name }}</td>
					<td>{{ .Address }}</td>
					<td>{{ .State }}</td>
				</tr>
				{{ end }}
			</tbody>
		</table>

		<p>State: 0 = Alive, 1 = Suspect, 2 = Dead, 3 = Left</p>

		<h2>Received Messages</h2>

		<a href="?deleteMessages=true">Delete All Messages (received and sent)</a>

		<table width="100%" border="1">
			<thead>
				<tr>
					<th>ID</th>
					<th>Time</th>
					<th>Key</th>
					<th>Value in the Message</th>
					<th>Version After Update (0 = no change)</th>
					<th>Changes</th>
					<th>Actions</th>
				</tr>
			</thead>

			<tbody>
				{{ range .ReceivedMessages }}
				<tr>
					<td>{{ .ID }}</td>
					<td>{{ .Time.Format "15:04:05.000" }}</td>
					<td>{{ .Pair.Key }}</td>
					<td>size: {{ .Pair.Value | len }}, codec: {{ .Pair.Codec }}</td>
					<td>{{ .Version }}</td>
					<td>{{ StringsJoin .Changes ", " }}</td>
					<td>
						<a href="?viewMsg={{ .ID }}&format=json">json</a>
						| <a href="?viewMsg={{ .ID }}&format=json-pretty">json-pretty</a>
						| <a href="?viewMsg={{ .ID }}&format=struct">struct</a>
					</td>
				</tr>
				{{ end }}
			</tbody>
		</table>

		<h2>Sent Messages</h2>

		<a href="?deleteMessages=true">Delete All Messages (received and sent)</a>

		<table width="100%" border="1">
			<thead>
				<tr>
					<th>ID</th>
					<th>Time</th>
					<th>Key</th>
					<th>Value</th>
					<th>Version</th>
					<th>Changes</th>
					<th>Actions</th>
				</tr>
			</thead>

			<tbody>
				{{ range .SentMessages }}
				<tr>
					<td>{{ .ID }}</td>
					<td>{{ .Time.Format "15:04:05.000" }}</td>
					<td>{{ .Pair.Key }}</td>
					<td>size: {{ .Pair.Value | len }}, codec: {{ .Pair.Codec }}</td>
					<td>{{ .Version }}</td>
					<td>{{ StringsJoin .Changes ", " }}</td>
					<td>
						<a href="?viewMsg={{ .ID }}&format=json">json</a>
						| <a href="?viewMsg={{ .ID }}&format=json-pretty">json-pretty</a>
						| <a href="?viewMsg={{ .ID }}&format=struct">struct</a>
					</td>
				</tr>
				{{ end }}
			</tbody>
		</table>
	</body>
</html>`
