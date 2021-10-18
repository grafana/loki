package gateway

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/ViaQ/logerr/kverrors"
)

// IngressHost returns a hostname based on the cluster hostname which
// is compatible with v1.Ingress as well as with v1.Route (on OpenShift) or
// an error if the cluster hostname cannot be parsed as a URL.
func IngressHost(stackName, namespace, hostname string) (string, error) {
	u, err := url.Parse(hostname)
	if err != nil {
		return "", kverrors.Wrap(err, "failed to part gateway host")
	}

	parts := strings.Split(u.Hostname(), ".")
	h := strings.Join(parts[1:], ".")

	return fmt.Sprintf("%s-%s.apps.%s", stackName, namespace, h), nil
}
