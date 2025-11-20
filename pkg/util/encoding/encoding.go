package encoding

// Re-export from client module to maintain code reuse
import "github.com/grafana/loki/client/util"

type Encbuf = util.Encbuf
type Decbuf = util.Decbuf

func EncWith(b []byte) Encbuf {
	return util.EncWith(b)
}

func EncWrap(inner Encbuf) Encbuf {
	return util.EncWrap(inner)
}

func DecWith(b []byte) Decbuf {
	return util.DecWith(b)
}

func DecWrap(inner Decbuf) Decbuf {
	return util.DecWrap(inner)
}
