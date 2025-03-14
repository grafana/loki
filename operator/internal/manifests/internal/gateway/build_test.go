package gateway

import (
	"testing"

	"github.com/stretchr/testify/require"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
)

func TestBuild_StaticMode(t *testing.T) {
	for _, tc := range []struct {
		name          string
		authNSpec     []lokiv1.AuthenticationSpec
		tenantSecrets []*Secret
		authZSpec     *lokiv1.AuthorizationSpec
		expTntCfg     string
		expRbacCfg    string
	}{
		{
			name: "oidc",
			authNSpec: []lokiv1.AuthenticationSpec{
				{
					TenantName: "test-a",
					TenantID:   "test",
					OIDC: &lokiv1.OIDCSpec{
						Secret: &lokiv1.TenantSecretSpec{
							Name: "test",
						},
						IssuerCA: &lokiv1.CASpec{
							CA:    "my-custom-ca",
							CAKey: "special-ca.crt",
						},
						IssuerURL:     "https://127.0.0.1:5556/dex",
						RedirectURL:   "https://localhost:8443/oidc/test-a/callback",
						GroupClaim:    "test",
						UsernameClaim: "test",
					},
				},
			},
			tenantSecrets: []*Secret{
				{
					TenantName: "test-a",
					OIDC: &OIDC{
						ClientID:     "test",
						ClientSecret: "test123",
						IssuerCAPath: "/var/run/tenants-ca/test-a/special-ca.crt",
					},
				},
			},
			authZSpec: &lokiv1.AuthorizationSpec{
				Roles: []lokiv1.RoleSpec{
					{
						Name:        "some-name",
						Resources:   []string{"metrics"},
						Tenants:     []string{"test-a"},
						Permissions: []lokiv1.PermissionType{"read"},
					},
				},
				RoleBindings: []lokiv1.RoleBindingsSpec{
					{
						Name: "test-a",
						Subjects: []lokiv1.Subject{
							{
								Name: "test@example.com",
								Kind: "user",
							},
						},
						Roles: []string{"read-write"},
					},
				},
			},
			expTntCfg: `
tenants:
- name: test-a
  id: test
  oidc:
    clientID: test
    clientSecret: test123
    issuerCAPath: /var/run/tenants-ca/test-a/special-ca.crt
    issuerURL: https://127.0.0.1:5556/dex
    redirectURL: https://localhost:8443/oidc/test-a/callback
    usernameClaim: test
    groupClaim: test
  opa:
    query: data.lokistack.allow
    paths:
    - /etc/lokistack-gateway/rbac.yaml
    - /etc/lokistack-gateway/lokistack-gateway.rego
`,
			expRbacCfg: `
roleBindings:
- name: test-a
  roles:
  - read-write
  subjects:
  - kind: user
    name: test@example.com
roles:
- name: some-name
  permissions:
  - read
  resources:
  - metrics
  tenants:
  - test-a
`,
		},
		{
			name: "mTLS",
			authNSpec: []lokiv1.AuthenticationSpec{
				{
					TenantName: "test-a",
					TenantID:   "test",
					MTLS: &lokiv1.MTLSSpec{
						CA: &lokiv1.CASpec{
							CA:    "my-custom-ca",
							CAKey: "special-ca.crt",
						},
					},
				},
			},
			tenantSecrets: []*Secret{
				{
					TenantName: "test-a",
					MTLS: &MTLS{
						CAPath: "/var/run/tenants-ca/test-a/special-ca.crt",
					},
				},
			},
			authZSpec: &lokiv1.AuthorizationSpec{
				Roles: []lokiv1.RoleSpec{
					{
						Name:        "some-name",
						Resources:   []string{"metrics"},
						Tenants:     []string{"test-a"},
						Permissions: []lokiv1.PermissionType{"read"},
					},
				},
				RoleBindings: []lokiv1.RoleBindingsSpec{
					{
						Name: "test-a",
						Subjects: []lokiv1.Subject{
							{
								Name: "test@example.com",
								Kind: "user",
							},
						},
						Roles: []string{"read-write"},
					},
				},
			},
			expTntCfg: `
tenants:
- name: test-a
  id: test
  mTLS:
    caPath: /var/run/tenants-ca/test-a/special-ca.crt
  opa:
    query: data.lokistack.allow
    paths:
    - /etc/lokistack-gateway/rbac.yaml
    - /etc/lokistack-gateway/lokistack-gateway.rego
`,
			expRbacCfg: `
roleBindings:
- name: test-a
  roles:
  - read-write
  subjects:
  - kind: user
    name: test@example.com
roles:
- name: some-name
  permissions:
  - read
  resources:
  - metrics
  tenants:
  - test-a
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := Options{
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode:           lokiv1.Static,
						Authentication: tc.authNSpec,
						Authorization:  tc.authZSpec,
					},
				},
				Namespace:     "test-ns",
				Name:          "test",
				TenantSecrets: tc.tenantSecrets,
			}
			rbacConfig, tenantsConfig, regoCfg, err := Build(opts)
			require.NoError(t, err)
			require.YAMLEq(t, tc.expTntCfg, string(tenantsConfig))
			require.YAMLEq(t, tc.expRbacCfg, string(rbacConfig))
			require.NotEmpty(t, regoCfg)
		})
	}
}

func TestBuild_DynamicMode(t *testing.T) {
	for _, tc := range []struct {
		name          string
		authNSpec     []lokiv1.AuthenticationSpec
		tenantSecrets []*Secret
		expTntCfg     string
	}{
		{
			name: "oidc",
			authNSpec: []lokiv1.AuthenticationSpec{
				{
					TenantName: "test-a",
					TenantID:   "test",
					OIDC: &lokiv1.OIDCSpec{
						Secret: &lokiv1.TenantSecretSpec{
							Name: "test",
						},
						IssuerCA: &lokiv1.CASpec{
							CA:    "my-custom-ca",
							CAKey: "special-ca.crt",
						},
						IssuerURL:     "https://127.0.0.1:5556/dex",
						RedirectURL:   "https://localhost:8443/oidc/test-a/callback",
						GroupClaim:    "test",
						UsernameClaim: "test",
					},
				},
			},
			tenantSecrets: []*Secret{
				{
					TenantName: "test-a",
					OIDC: &OIDC{
						ClientID:     "test",
						ClientSecret: "test123",
						IssuerCAPath: "/var/run/tenants-ca/test-a/special-ca.crt",
					},
				},
			},
			expTntCfg: `
tenants:
- name: test-a
  id: test
  oidc:
    clientID: test
    clientSecret: test123
    issuerCAPath: /var/run/tenants-ca/test-a/special-ca.crt
    issuerURL: https://127.0.0.1:5556/dex
    redirectURL: https://localhost:8443/oidc/test-a/callback
    usernameClaim: test
    groupClaim: test
  opa:
    url: http://127.0.0.1:8181/v1/data/observatorium/allow
`,
		},
		{
			name: "mTLS",
			authNSpec: []lokiv1.AuthenticationSpec{
				{
					TenantName: "test-a",
					TenantID:   "test",
					MTLS: &lokiv1.MTLSSpec{
						CA: &lokiv1.CASpec{
							CA:    "my-custom-ca",
							CAKey: "special-ca.crt",
						},
					},
				},
			},
			tenantSecrets: []*Secret{
				{
					TenantName: "test-a",
					MTLS: &MTLS{
						CAPath: "/var/run/tenants-ca/test-a/special-ca.crt",
					},
				},
			},
			expTntCfg: `
tenants:
- name: test-a
  id: test
  mTLS:
    caPath: /var/run/tenants-ca/test-a/special-ca.crt
  opa:
    url: http://127.0.0.1:8181/v1/data/observatorium/allow
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := Options{
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode:           lokiv1.Dynamic,
						Authentication: tc.authNSpec,
						Authorization: &lokiv1.AuthorizationSpec{
							OPA: &lokiv1.OPASpec{
								URL: "http://127.0.0.1:8181/v1/data/observatorium/allow",
							},
						},
					},
				},
				Namespace:     "test-ns",
				Name:          "test",
				TenantSecrets: tc.tenantSecrets,
			}
			rbacConfig, tenantsConfig, regoCfg, err := Build(opts)
			require.NoError(t, err)
			require.YAMLEq(t, tc.expTntCfg, string(tenantsConfig))
			require.Empty(t, rbacConfig)
			require.Empty(t, regoCfg)
		})
	}
}

func TestBuild_OpenshiftLoggingMode(t *testing.T) {
	expTntCfg := `
tenants:
- name: application
  id: 32e45e3e-b760-43a2-a7e1-02c5631e56e9
  openshift:
    serviceAccount: lokistack-gateway
    redirectURL: https://localhost:8443/openshift/application/callback
    cookieSecret: abcd
  opa:
    url: http://127.0.0.1:8080/v1/data/lokistack/allow
    withAccessToken: true
- name: infrastructure
  id: 40de0532-10a2-430c-9a00-62c46455c118
  openshift:
    serviceAccount: lokistack-gateway
    redirectURL: https://localhost:8443/openshift/infrastructure/callback
    cookieSecret: efgh
  opa:
    url: http://127.0.0.1:8080/v1/data/lokistack/allow
    withAccessToken: true
- name: audit
  id: 26d7c49d-182e-4d93-bade-510c6cc3243d
  openshift:
    serviceAccount: lokistack-gateway
    redirectURL: https://localhost:8443/openshift/audit/callback
    cookieSecret: deadbeef
  opa:
    url: http://127.0.0.1:8080/v1/data/lokistack/allow
    withAccessToken: true
`
	opts := Options{
		Stack: lokiv1.LokiStackSpec{
			Tenants: &lokiv1.TenantsSpec{
				Mode: lokiv1.OpenshiftLogging,
			},
		},
		OpenShiftOptions: openshift.Options{
			Authentication: []openshift.AuthenticationSpec{
				{
					TenantName:     "application",
					TenantID:       "32e45e3e-b760-43a2-a7e1-02c5631e56e9",
					ServiceAccount: "lokistack-gateway",
					RedirectURL:    "https://localhost:8443/openshift/application/callback",
					CookieSecret:   "abcd",
				},
				{
					TenantName:     "infrastructure",
					TenantID:       "40de0532-10a2-430c-9a00-62c46455c118",
					ServiceAccount: "lokistack-gateway",
					RedirectURL:    "https://localhost:8443/openshift/infrastructure/callback",
					CookieSecret:   "efgh",
				},
				{
					TenantName:     "audit",
					TenantID:       "26d7c49d-182e-4d93-bade-510c6cc3243d",
					ServiceAccount: "lokistack-gateway",
					RedirectURL:    "https://localhost:8443/openshift/audit/callback",
					CookieSecret:   "deadbeef",
				},
			},
			Authorization: openshift.AuthorizationSpec{
				OPAUrl: "http://127.0.0.1:8080/v1/data/lokistack/allow",
			},
		},
		Namespace: "test-ns",
		Name:      "test",
		TenantSecrets: []*Secret{
			{
				TenantName: "application",
				OIDC: &OIDC{
					ClientID:     "test",
					ClientSecret: "ZXhhbXBsZS1hcHAtc2VjcmV0",
					IssuerCAPath: "./tmp/certs/ca.pem",
				},
			},
			{
				TenantName: "infrastructure",
				OIDC: &OIDC{
					ClientID:     "test",
					ClientSecret: "ZXhhbXBsZS1hcHAtc2VjcmV0",
					IssuerCAPath: "./tmp/certs/ca.pem",
				},
			},
			{
				TenantName: "audit",
				OIDC: &OIDC{
					ClientID:     "test",
					ClientSecret: "ZXhhbXBsZS1hcHAtc2VjcmV0",
					IssuerCAPath: "./tmp/certs/ca.pem",
				},
			},
		},
	}

	rbacConfig, tenantsConfig, regoCfg, err := Build(opts)
	require.NoError(t, err)
	require.YAMLEq(t, expTntCfg, string(tenantsConfig))
	require.Empty(t, rbacConfig)
	require.Empty(t, regoCfg)
}

func TestBuild_OpenshiftNetworkMode(t *testing.T) {
	expTntCfg := `
tenants:
- name: network
  id: 3e922593-e352-47df-8c5c-c39dbdd5b83c
  openshift:
    serviceAccount: lokistack-gateway
    redirectURL: https://localhost:8443/openshift/network/callback
    cookieSecret: whynot
  opa:
    url: http://127.0.0.1:8080/v1/data/lokistack/allow
    withAccessToken: true
`
	opts := Options{
		Stack: lokiv1.LokiStackSpec{
			Tenants: &lokiv1.TenantsSpec{
				Mode: lokiv1.OpenshiftNetwork,
			},
		},
		OpenShiftOptions: openshift.Options{
			Authentication: []openshift.AuthenticationSpec{
				{
					TenantName:     "network",
					TenantID:       "3e922593-e352-47df-8c5c-c39dbdd5b83c",
					ServiceAccount: "lokistack-gateway",
					RedirectURL:    "https://localhost:8443/openshift/network/callback",
					CookieSecret:   "whynot",
				},
			},
			Authorization: openshift.AuthorizationSpec{
				OPAUrl: "http://127.0.0.1:8080/v1/data/lokistack/allow",
			},
		},
		Namespace: "test-ns",
		Name:      "test",
		TenantSecrets: []*Secret{
			{
				TenantName: "network",
				OIDC: &OIDC{
					ClientID:     "test",
					ClientSecret: "ZXhhbXBsZS1hcHAtc2VjcmV0",
					IssuerCAPath: "./tmp/certs/ca.pem",
				},
			},
		},
	}

	rbacConfig, tenantsConfig, regoCfg, err := Build(opts)
	require.NoError(t, err)
	require.YAMLEq(t, expTntCfg, string(tenantsConfig))
	require.Empty(t, rbacConfig)
	require.Empty(t, regoCfg)
}
