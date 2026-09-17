package loki

import (
	"context"
	"flag"
	"testing"

	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/labelaccess"
	labelaccesstypes "github.com/grafana/loki/v3/pkg/labelaccess/types"
	loglinequeryfrontend "github.com/grafana/loki/v3/pkg/logline/queryfrontend"
	"github.com/grafana/loki/v3/pkg/logline/store"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/bucket"
	"github.com/grafana/loki/v3/pkg/storage/bucket/filesystem"
	storageconfig "github.com/grafana/loki/v3/pkg/storage/config"
)

func TestLoglineConfigRegistration(t *testing.T) {
	cfg := Config{}
	flags := flag.NewFlagSet("test", flag.PanicOnError)

	cfg.RegisterFlags(flags)

	require.NotNil(t, flags.Lookup("logline.enabled"))
	require.NotNil(t, flags.Lookup("logline-store.min-date"))
	require.NotNil(t, flags.Lookup("logline-query-frontend.hint-timeout"))
	require.False(t, cfg.Logline.Enabled)
}

func TestLoglineConfigValidationAtRoot(t *testing.T) {
	cfg := minimalWorkingConfig(t, t.TempDir(), QueryFrontend)
	cfg.Logline.Enabled = true

	err := cfg.Validate()
	require.ErrorContains(t, err, "CONFIG ERROR: invalid logline config")
	require.ErrorContains(t, err, "min_date is required")
}

func TestLoglineModuleDependencies(t *testing.T) {
	for _, tc := range []struct {
		name        string
		lbacEnabled bool
	}{
		{name: "without LBAC"},
		{name: "with LBAC", lbacEnabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := &Loki{}
			l.Cfg.Target = flagext.StringSliceCSV{QueryFrontend}
			l.Cfg.LBAC.Enabled = tc.lbacEnabled

			require.NoError(t, l.setupModuleManager())
			require.True(t, l.ModuleManager.IsModuleRegistered(LoglineTripperware))
			require.Contains(t, l.ModuleManager.DependenciesForModule(LoglineTripperware), QueryFrontendTripperware)
			require.Contains(t, l.ModuleManager.DependenciesForModule(QueryFrontend), LoglineTripperware)

			if tc.lbacEnabled {
				require.Contains(t, l.ModuleManager.DependenciesForModule(LabelAccessTripperware), LoglineTripperware)
			}
		})
	}
}

func TestLabelAccessTripperwareWrapsLogline(t *testing.T) {
	const tenant = "test-tenant"

	var transformedTenant string
	loglineMiddleware := queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
			transformedTenant = labelaccess.UserIDTransformer(ctx, tenant)
			return next.Do(ctx, req)
		})
	})

	l := &Loki{QueryFrontEndMiddleware: loglineMiddleware}
	_, err := l.initLabelAccessMiddleware()
	require.NoError(t, err)

	ctx := user.InjectOrgID(context.Background(), tenant)
	ctx = labelaccess.InjectLabelMatchersContext(ctx, labelaccess.LabelPolicySet{
		tenant: []*labelaccesstypes.LabelPolicy{{}},
	})
	handler := l.QueryFrontEndMiddleware.Wrap(queryrangebase.HandlerFunc(
		func(context.Context, queryrangebase.Request) (queryrangebase.Response, error) {
			return nil, nil
		},
	))

	_, err = handler.Do(ctx, nil)
	require.NoError(t, err)
	require.NotEmpty(t, transformedTenant)
	require.NotEqual(t, tenant, transformedTenant)
}

func TestInitLoglineMiddlewareDisabledDoesNotCallExtension(t *testing.T) {
	called := false
	l := &Loki{
		GetLoglineTenantSettings: func() loglinequeryfrontend.TenantSettings {
			called = true
			return nil
		},
	}

	service, err := l.initLoglineMiddleware()
	require.NoError(t, err)
	require.Nil(t, service)
	require.False(t, called)
}

func TestInitLoglineMiddlewareUsesExtension(t *testing.T) {
	prepareGlobalMetricsRegistry(t)

	called := false
	l := &Loki{
		Cfg: Config{
			Logline: loglinequeryfrontend.Config{
				Enabled: true,
				Store: store.Config{
					MinDate: "2026-09-01",
				},
			},
			SchemaConfig: storageconfig.SchemaConfig{
				Configs: []storageconfig.PeriodConfig{{
					From:       storageconfig.DayTime{Time: 0},
					ObjectType: bucket.Filesystem,
				}},
			},
			StorageConfig: storage.Config{
				ObjectStore: bucket.ConfigWithNamedStores{
					Config: bucket.Config{
						Filesystem: filesystem.Config{Directory: t.TempDir()},
					},
				},
			},
		},
		GetLoglineTenantSettings: func() loglinequeryfrontend.TenantSettings {
			called = true
			return nil
		},
	}

	service, err := l.initLoglineMiddleware()
	require.NoError(t, err)
	require.NotNil(t, service)
	require.True(t, called)
	require.NotNil(t, l.QueryFrontEndMiddleware)
}
