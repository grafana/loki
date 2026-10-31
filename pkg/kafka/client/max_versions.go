package client

import (
	"github.com/twmb/franz-go/pkg/kmsg"
	"github.com/twmb/franz-go/pkg/kversion"
)

// maxAPIVersionsRequestVersion is the highest ApiVersions request version that clients configured
// with MaxVersions send.
const maxAPIVersionsRequestVersion = 4

// MaxVersions returns the maximum Kafka request versions Loki's Kafka clients use.
//
// Since v1.22.0, franz-go begins every connection with an ApiVersions v5 request (KIP-1242). A Kafka
// broker that doesn't support v5 replies UNSUPPORTED_VERSION and the client retries at a lower
// version, but some Kafka-compatible backends, such as WarpStream, close the connection instead.
// franz-go only falls back to a lower version when the connection is reset, not when it is closed
// cleanly (EOF), so the client never connects. Capping ApiVersions at v4 avoids this. All other
// requests keep franz-go's default maximum versions, which are still negotiated down to what each
// broker supports.
func MaxVersions() *kversion.Versions {
	versions := kversion.Stable()
	versions.SetMaxKeyVersion(kmsg.ApiVersions.Int16(), maxAPIVersionsRequestVersion)
	return versions
}
