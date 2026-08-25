package loki

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/labelaccess"
)

// TestLBACAuthMiddlewareRegisteredOnce drives both wiring steps that touch
// HTTPAuthMiddleware: setupAuthMiddleware() (base auth, from New()) and the
// AuthMiddleware module (initAuthMiddleware, which merges the LBAC middleware).
// The LBAC middleware must be installed exactly once
func TestLBACAuthMiddlewareRegisteredOnce(t *testing.T) {
	l := &Loki{}
	l.Cfg.AuthEnabled = false
	l.Cfg.LBAC.Enabled = true

	l.setupAuthMiddleware()
	_, err := l.initAuthMiddleware()
	require.NoError(t, err)

	var seenQuery string
	final := l.HTTPAuthMiddleware.Wrap(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seenQuery = r.URL.Query().Get("query")
	}))

	q := url.Values{}
	q.Set("query", `{__aggregated_metric__="varlog_service"}`)
	req := httptest.NewRequest("GET", "/loki/api/v1/query_range?"+q.Encode(), nil)
	req.Header.Set("X-Scope-OrgID", "test_tenant")
	req.Header.Add(labelaccess.HTTPHeaderKey, "test_tenant:"+url.PathEscape(`{env="dev"}`))

	final.ServeHTTP(httptest.NewRecorder(), req)

	require.Equal(t, 1, strings.Count(seenQuery, "logfmt"),
		"LBAC query modification must be applied exactly once; got query: %s", seenQuery)
}

// lbacModules are the modules setupLBAC registers.
var lbacModules = []string{
	AuthMiddleware,
	LabelAccess,
	LabelAccessStoreWrapper,
	LabelAccessIngesterWrapper,
	LabelAccessV2Engine,
	LabelAccessInterceptors,
	LabelAccessTripperware,
	Filterers,
	LabelAccessUserIDTransformer,
	AuthTripperware,
}

// TestSetupLBACEnabled exercises setupLBAC through setupModuleManager.
// It confirmed that all the registered modules exist without cycles
func TestSetupLBACEnabled(t *testing.T) {
	l := &Loki{}
	l.Cfg.Target = flagext.StringSliceCSV{All}
	l.Cfg.LBAC.Enabled = true

	require.NoError(t, l.setupModuleManager())

	for _, mod := range lbacModules {
		require.True(t, l.ModuleManager.IsModuleRegistered(mod),
			"module %s should be registered when LBAC is enabled", mod)
	}

	// DependenciesForModule returns transitive deps, so assert containment of
	// the edges setupLBAC adds rather than exact equality.
	edges := map[string][]string{
		Querier:       {AuthMiddleware, LabelAccess, LabelAccessStoreWrapper},
		QueryFrontend: {AuthMiddleware, LabelAccessTripperware},
		QueryEngine:   {AuthMiddleware, LabelAccessV2Engine},
		Ingester:      {LabelAccessStoreWrapper, LabelAccessIngesterWrapper, Filterers},
		Server:        {LabelAccessInterceptors, LabelAccessUserIDTransformer},
		IndexGateway:  {LabelAccess},
	}
	for mod, deps := range edges {
		got := l.ModuleManager.DependenciesForModule(mod)
		for _, dep := range deps {
			require.Contains(t, got, dep,
				"module %s should depend on %s under LBAC", mod, dep)
		}
	}
}

// TestSetupLBACDisabled confirms the LBAC modules are absent when LBAC is off,
// so setupLBAC's wiring is gated on the feature flag.
func TestSetupLBACDisabled(t *testing.T) {
	l := &Loki{}
	l.Cfg.Target = flagext.StringSliceCSV{All}
	l.Cfg.LBAC.Enabled = false

	require.NoError(t, l.setupModuleManager())

	for _, mod := range lbacModules {
		// AuthTripperware and AuthMiddleware names are unique to LBAC wiring.
		require.False(t, l.ModuleManager.IsModuleRegistered(mod),
			"module %s should not be registered when LBAC is disabled", mod)
	}
}
