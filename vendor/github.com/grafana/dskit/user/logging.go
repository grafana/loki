// Provenance-includes-location: https://github.com/weaveworks/common/blob/main/user/logging.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: Weaveworks Ltd.

package user

import (
	"context"

	"github.com/grafana/dskit/log"
)

// LogWith returns user and org information from the context as log fields.
func LogWith(ctx context.Context, log log.Interface) log.Interface {
	userID, err := ExtractUserID(ctx)
	if err == nil {
		log = log.WithField("userID", userID)
	}

	orgID, err := ExtractOrgID(ctx)
	if err == nil {
		log = log.WithField("orgID", orgID)
	}

	return log
}
