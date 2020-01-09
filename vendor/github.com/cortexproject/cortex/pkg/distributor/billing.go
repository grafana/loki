package distributor

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"time"

	billing "github.com/weaveworks/billing-client"
	"github.com/weaveworks/common/user"
)

func init() {
	billing.MustRegisterMetrics()
}

func (d *Distributor) emitBillingRecord(ctx context.Context, buf []byte, samples int64) error {
	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	hasher := sha256.New()
	hasher.Write(buf)
	hash := "sha256:" + base64.URLEncoding.EncodeToString(hasher.Sum(nil))
	amounts := billing.Amounts{
		billing.Samples: samples,
	}
	return d.billingClient.AddAmounts(
		hash,
		userID,
		now,
		amounts,
		nil,
	)
}
