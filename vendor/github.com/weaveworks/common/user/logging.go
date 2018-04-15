package user

import (
	"golang.org/x/net/context"

	log "github.com/sirupsen/logrus"
)

// LogFields returns user and org information from the context as log fields.
func LogFields(ctx context.Context) log.Fields {
	fields := log.Fields{}
	userID, err := ExtractUserID(ctx)
	if err == nil {
		fields["userID"] = userID
	}
	orgID, err := ExtractOrgID(ctx)
	if err == nil {
		fields["orgID"] = orgID
	}
	return fields
}
