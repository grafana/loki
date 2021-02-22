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

	"github.com/cortexproject/cortex/pkg/ruler/rules"
	"github.com/cortexproject/cortex/pkg/storage/bucket"
	cortex_tsdb "github.com/cortexproject/cortex/pkg/storage/tsdb"
	"github.com/cortexproject/cortex/pkg/tenant"
	"github.com/cortexproject/cortex/pkg/util"
)

type TenantDeletionAPI struct {
	bucketClient objstore.Bucket
	ruleStore    rules.RuleStore
	logger       log.Logger
	cfgProvider  bucket.TenantConfigProvider
}

func NewTenantDeletionAPI(storageCfg cortex_tsdb.BlocksStorageConfig, cfgProvider bucket.TenantConfigProvider, ruleStore rules.RuleStore, logger log.Logger, reg prometheus.Registerer) (*TenantDeletionAPI, error) {
	bucketClient, err := createBucketClient(storageCfg, logger, reg)
	if err != nil {
		return nil, err
	}

	return newTenantDeletionAPI(bucketClient, cfgProvider, ruleStore, logger), nil
}

func newTenantDeletionAPI(bkt objstore.Bucket, cfgProvider bucket.TenantConfigProvider, ruleStore rules.RuleStore, logger log.Logger) *TenantDeletionAPI {
	return &TenantDeletionAPI{
		bucketClient: bkt,
		ruleStore:    ruleStore,
		cfgProvider:  cfgProvider,
		logger:       logger,
	}
}

func (api *TenantDeletionAPI) DeleteTenant(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	err = cortex_tsdb.WriteTenantDeletionMark(r.Context(), api.bucketClient, userID, api.cfgProvider, cortex_tsdb.NewTenantDeletionMark(time.Now()))
	if err != nil {
		level.Error(api.logger).Log("msg", "failed to write tenant deletion mark", "user", userID, "err", err)

		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	level.Info(api.logger).Log("msg", "tenant deletion mark in blocks storage created", "user", userID)

	if api.ruleStore != nil {
		err := api.deleteRules(r.Context(), userID)
		if err != nil {
			level.Error(api.logger).Log("msg", "failed to delete tenant rule groups", "user", userID, "err", err)
			http.Error(w, errors.Wrapf(err, "failed to delete tenant rule groups").Error(), http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
}

func (api *TenantDeletionAPI) deleteRules(ctx context.Context, userID string) error {
	if !api.ruleStore.SupportsModifications() {
		level.Warn(api.logger).Log("msg", "cannot delete tenant rule groups, using read-only rule store", "user", userID)
		return nil
	}

	err := api.ruleStore.DeleteNamespace(ctx, userID, "") // Empty namespace = delete all rule groups.
	if err != nil && !errors.Is(err, rules.ErrGroupNamespaceNotFound) {
		return err
	}

	level.Info(api.logger).Log("msg", "deleted all tenant rule groups", "user", userID)
	return nil
}

type DeleteTenantStatusResponse struct {
	TenantID                  string `json:"tenant_id"`
	BlocksDeleted             bool   `json:"blocks_deleted"`
	RuleGroupsDeleted         bool   `json:"rule_groups_deleted"`
	AlertManagerConfigDeleted bool   `json:"alert_manager_config_deleted,omitempty"` // Not yet supported.
}

func (api *TenantDeletionAPI) DeleteTenantStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	result := DeleteTenantStatusResponse{}
	result.TenantID = userID
	result.BlocksDeleted, err = api.isBlocksForUserDeleted(ctx, userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	result.RuleGroupsDeleted, err = api.isRulesForUserDeleted(ctx, userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	util.WriteJSONResponse(w, result)
}

func (api *TenantDeletionAPI) isRulesForUserDeleted(ctx context.Context, userID string) (bool, error) {
	if api.ruleStore == nil {
		// If API doesn't have access to rule store, then we cannot say that rules have been deleted.
		return false, nil
	}

	list, err := api.ruleStore.ListRuleGroupsForUserAndNamespace(ctx, userID, "")
	if err != nil {
		return false, errors.Wrap(err, "failed to list rule groups for tenant")
	}

	return len(list) == 0, nil
}

func (api *TenantDeletionAPI) isBlocksForUserDeleted(ctx context.Context, userID string) (bool, error) {
	var errBlockFound = errors.New("block found")

	userBucket := bucket.NewUserBucketClient(userID, api.bucketClient, api.cfgProvider)
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
