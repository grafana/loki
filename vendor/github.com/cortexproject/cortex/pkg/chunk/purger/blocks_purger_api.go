package purger

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/oklog/ulid"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/thanos/pkg/objstore"

	"github.com/cortexproject/cortex/pkg/storage/bucket"
	cortex_tsdb "github.com/cortexproject/cortex/pkg/storage/tsdb"
	"github.com/cortexproject/cortex/pkg/tenant"
	"github.com/cortexproject/cortex/pkg/util"
)

type BlocksPurgerAPI struct {
	bucketClient objstore.Bucket
	logger       log.Logger
}

func NewBlocksPurgerAPI(storageCfg cortex_tsdb.BlocksStorageConfig, logger log.Logger, reg prometheus.Registerer) (*BlocksPurgerAPI, error) {
	bucketClient, err := createBucketClient(storageCfg, logger, reg)
	if err != nil {
		return nil, err
	}

	return newBlocksPurgerAPI(bucketClient, logger), nil
}

func newBlocksPurgerAPI(bkt objstore.Bucket, logger log.Logger) *BlocksPurgerAPI {
	return &BlocksPurgerAPI{bucketClient: bkt, logger: logger}
}

func (api *BlocksPurgerAPI) DeleteTenant(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	err = cortex_tsdb.WriteTenantDeletionMark(r.Context(), api.bucketClient, userID, cortex_tsdb.NewTenantDeletionMark(time.Now()))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	level.Info(api.logger).Log("msg", "tenant deletion marker created", "user", userID)

	w.WriteHeader(http.StatusOK)
}

type DeleteTenantStatusResponse struct {
	TenantID                  string `json:"tenant_id"`
	BlocksDeleted             bool   `json:"blocks_deleted"`
	RuleGroupsDeleted         bool   `json:"rule_groups_deleted,omitempty"`          // Not yet supported.
	AlertManagerConfigDeleted bool   `json:"alert_manager_config_deleted,omitempty"` // Not yet supported.
}

func (api *BlocksPurgerAPI) DeleteTenantStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	result := DeleteTenantStatusResponse{}
	result.TenantID = userID
	result.BlocksDeleted, err = api.checkBlocksForUser(ctx, userID)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	util.WriteJSONResponse(w, result)
}

func (api *BlocksPurgerAPI) checkBlocksForUser(ctx context.Context, userID string) (bool, error) {
	var errBlockFound = errors.New("block found")

	userBucket := bucket.NewUserBucketClient(userID, api.bucketClient)
	err := userBucket.Iter(ctx, "", func(s string) error {
		s = strings.TrimSuffix(s, "/")

		_, err := ulid.Parse(s)
		if err != nil {
			// not block, keep looking
			return nil
		}

		// Used as shortcut to stop iteration.
		return errBlockFound
	})

	if errors.Is(err, errBlockFound) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	// No blocks found, all good.
	return true, nil
}

func createBucketClient(cfg cortex_tsdb.BlocksStorageConfig, logger log.Logger, reg prometheus.Registerer) (objstore.Bucket, error) {
	bucketClient, err := bucket.NewClient(context.Background(), cfg.Bucket, "purger", logger, reg)
	if err != nil {
		return nil, errors.Wrap(err, "create bucket client")
	}

	return bucketClient, nil
}
