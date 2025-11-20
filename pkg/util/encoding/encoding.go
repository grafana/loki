package encoding

// Re-export from client module to maintain code reuse
import (
	"github.com/grafana/loki/client/util"
	tsdb_enc "github.com/prometheus/prometheus/tsdb/encoding"
)

type Encbuf = util.Encbuf
type Decbuf = util.Decbuf

func EncWith(b []byte) Encbuf {
	return util.EncWith(b)
}

// EncWrap wraps a Prometheus encoding.Encbuf into our Encbuf
func EncWrap(inner tsdb_enc.Encbuf) Encbuf {
	return util.EncWrap(inner)
}

func DecWith(b []byte) Decbuf {
	return util.DecWith(b)
}

// DecWrap wraps a Prometheus encoding.Decbuf into our Decbuf
func DecWrap(inner tsdb_enc.Decbuf) Decbuf {
	return util.DecWrap(inner)
}
