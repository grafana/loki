package kafka

import (
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
	"github.com/twmb/franz-go/pkg/kversion"
)

// DefaultAPIVersionsMaxVersion is the highest ApiVersions request version the
// Kafka clients send by default.
//
// franz-go >= 1.22 sends ApiVersions v5 first. WarpStream agents close the
// connection on v5 instead of replying UNSUPPORTED_VERSION, so the client
// can't fall back on its own. franz-go 1.21.x already sends v4, so the cap
// changes nothing there.
const DefaultAPIVersionsMaxVersion = 4

const apiVersionsMaxVersionUsage = "The highest ApiVersions request version the Kafka client sends. " +
	"The default keeps franz-go >= 1.22 compatible with backends that close the connection on ApiVersions v5 (WarpStream). " +
	"0 to use the franz-go default."

// APIVersionsMaxVersionOpts returns the client options that cap ApiVersions at
// maxVersion, or none if maxVersion is 0.
func APIVersionsMaxVersionOpts(maxVersion int) []kgo.Opt {
	if maxVersion <= 0 {
		return nil
	}
	// kversion.Stable() is franz-go's default, so only ApiVersions changes.
	versions := kversion.Stable()
	versions.SetMaxKeyVersion(kmsg.ApiVersions.Int16(), int16(maxVersion))
	return []kgo.Opt{kgo.MaxVersions(versions)}
}
