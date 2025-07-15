package gateway

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
)

func TestValidateModes_StaticMode(t *testing.T) {
	type test struct {
		name    string
		wantErr string
		stack   *lokiv1.LokiStack
	}
	table := []test{
		{
			name:    "missing authentication spec",
			wantErr: "mandatory configuration - missing tenants' authentication configuration",
			stack: &lokiv1.LokiStack{
				TypeMeta: metav1.TypeMeta{
					Kind: "LokiStack",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-stack",
					Namespace: "some-ns",
					UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
				},
				Spec: lokiv1.LokiStackSpec{
					Size: lokiv1.SizeOneXExtraSmall,
					Tenants: &lokiv1.TenantsSpec{
						Mode: "static",
					},
				},
			},
		},
		{
			name:    "missing roles spec",
			wantErr: "mandatory configuration - missing roles configuration",
			stack: &lokiv1.LokiStack{
				TypeMeta: metav1.TypeMeta{
					Kind: "LokiStack",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-stack",
					Namespace: "some-ns",
					UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
				},
				Spec: lokiv1.LokiStackSpec{
					Size: lokiv1.SizeOneXExtraSmall,
					Tenants: &lokiv1.TenantsSpec{
						Mode: "static",
						Authentication: []lokiv1.AuthenticationSpec{
							{
								TenantName: "test",
								TenantID:   "1234",
								OIDC: &lokiv1.OIDCSpec{
									IssuerURL:     "some-url",
									RedirectURL:   "some-other-url",
									GroupClaim:    "test",
									UsernameClaim: "test",
								},
							},
						},
						Authorization: &lokiv1.AuthorizationSpec{
							Roles: nil,
						},
					},
				},
			},
		},
		{
			name:    "missing role bindings spec",
			wantErr: "mandatory configuration - missing role bindings configuration",
			stack: &lokiv1.LokiStack{
				TypeMeta: metav1.TypeMeta{
					Kind: "LokiStack",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-stack",
					Namespace: "some-ns",
					UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
				},
				Spec: lokiv1.LokiStackSpec{
					Size: lokiv1.SizeOneXExtraSmall,
					Tenants: &lokiv1.TenantsSpec{
						Mode: "static",
						Authentication: []lokiv1.AuthenticationSpec{
							{
								TenantName: "test",
								TenantID:   "1234",
								OIDC: &lokiv1.OIDCSpec{
									IssuerURL:     "some-url",
									RedirectURL:   "some-other-url",
									GroupClaim:    "test",
									UsernameClaim: "test",
								},
							},
						},
						Authorization: &lokiv1.AuthorizationSpec{
							Roles: []lokiv1.RoleSpec{
								{
									Name:        "some-name",
									Resources:   []string{"test"},
									Tenants:     []string{"test"},
									Permissions: []lokiv1.PermissionType{"read"},
								},
							},
							RoleBindings: nil,
						},
					},
				},
			},
		},
		{
			name:    "incompatible OPA URL provided",
			wantErr: "incompatible configuration - OPA URL not required for mode static",
			stack: &lokiv1.LokiStack{
				TypeMeta: metav1.TypeMeta{
					Kind: "LokiStack",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-stack",
					Namespace: "some-ns",
					UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
				},
				Spec: lokiv1.LokiStackSpec{
					Size: lokiv1.SizeOneXExtraSmall,
					Tenants: &lokiv1.TenantsSpec{
						Mode: "static",
						Authentication: []lokiv1.AuthenticationSpec{
							{
								TenantName: "test",
								TenantID:   "1234",
								OIDC: &lokiv1.OIDCSpec{
									IssuerURL:     "some-url",
									RedirectURL:   "some-other-url",
									GroupClaim:    "test",
									UsernameClaim: "test",
								},
							},
						},
						Authorization: &lokiv1.AuthorizationSpec{
							OPA: &lokiv1.OPASpec{
								URL: "some-url",
							},
							Roles: []lokiv1.RoleSpec{
								{
									Name:        "some-name",
									Resources:   []string{"test"},
									Tenants:     []string{"test"},
									Permissions: []lokiv1.PermissionType{"read"},
								},
							},
							RoleBindings: []lokiv1.RoleBindingsSpec{
								{
									Name: "some-name",
									Subjects: []lokiv1.Subject{
										{
											Name: "sub-1",
											Kind: "user",
										},
									},
									Roles: []string{"some-role"},
								},
							},
						},
					},
				},
			},
		},
		{
			name:    "all set",
			wantErr: "",
			stack: &lokiv1.LokiStack{
				TypeMeta: metav1.TypeMeta{
					Kind: "LokiStack",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-stack",
					Namespace: "some-ns",
					UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
				},
				Spec: lokiv1.LokiStackSpec{
					Size: lokiv1.SizeOneXExtraSmall,
					Tenants: &lokiv1.TenantsSpec{
						Mode: "static",
						Authentication: []lokiv1.AuthenticationSpec{
							{
								TenantName: "test",
								TenantID:   "1234",
								OIDC: &lokiv1.OIDCSpec{
									IssuerURL:     "some-url",
									RedirectURL:   "some-other-url",
									GroupClaim:    "test",
									UsernameClaim: "test",
								},
							},
						},
						Authorization: &lokiv1.AuthorizationSpec{
							Roles: []lokiv1.RoleSpec{
								{
									Name:        "some-name",
									Resources:   []string{"test"},
									Tenants:     []string{"test"},
									Permissions: []lokiv1.PermissionType{"read"},
								},
							},
							RoleBindings: []lokiv1.RoleBindingsSpec{
								{
									Name: "some-name",
									Subjects: []lokiv1.Subject{
										{
											Name: "sub-1",
											Kind: "user",
										},
									},
									Roles: []string{"some-role"},
								},
							},
						},
					},
				},
			},
		},
	}
	for _, tst := range table {
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			err := validateModes(tst.stack)
			if tst.wantErr != "" {
				require.EqualError(t, err, tst.wantErr)
			}
		})
	}
}

func TestValidateModes_DynamicMode(t *testing.T) {
	type test struct {
		name    string
		wantErr string
		stack   *lokiv1.LokiStack
	}
	table := []test{
		{
			name:    "missing authentication spec",
			wantErr: "mandatory configuration - missing tenants configuration",
			stack: &lokiv1.LokiStack{
				TypeMeta: metav1.TypeMeta{
					Kind: "LokiStack",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-stack",
					Namespace: "some-ns",
					UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
				},
				Spec: lokiv1.LokiStackSpec{
					Size: lokiv1.SizeOneXExtraSmall,
					Tenants: &lokiv1.TenantsSpec{
						Mode: "dynamic",
					},
				},
			},
		},
		{
			name:    "missing OPA URL spec",
			wantErr: "mandatory configuration - missing OPA Url",
			stack: &lokiv1.LokiStack{
				TypeMeta: metav1.TypeMeta{
					Kind: "LokiStack",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-stack",
					Namespace: "some-ns",
					UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
				},
				Spec: lokiv1.LokiStackSpec{
					Size: lokiv1.SizeOneXExtraSmall,
					Tenants: &lokiv1.TenantsSpec{
						Mode: "dynamic",
						Authentication: []lokiv1.AuthenticationSpec{
							{
								TenantName: "test",
								TenantID:   "1234",
								OIDC: &lokiv1.OIDCSpec{
									IssuerURL:     "some-url",
									RedirectURL:   "some-other-url",
									GroupClaim:    "test",
									UsernameClaim: "test",
								},
							},
						},
						Authorization: &lokiv1.AuthorizationSpec{
							OPA: nil,
						},
					},
				},
			},
		},
		{
			name:    "incompatible roles configuration provided",
			wantErr: "incompatible configuration - static roles not required for mode dynamic",
			stack: &lokiv1.LokiStack{
				TypeMeta: metav1.TypeMeta{
					Kind: "LokiStack",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-stack",
					Namespace: "some-ns",
					UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
				},
				Spec: lokiv1.LokiStackSpec{
					Size: lokiv1.SizeOneXExtraSmall,
					Tenants: &lokiv1.TenantsSpec{
						Mode: "dynamic",
						Authentication: []lokiv1.AuthenticationSpec{
							{
								TenantName: "test",
								TenantID:   "1234",
								OIDC: &lokiv1.OIDCSpec{
									IssuerURL:     "some-url",
									RedirectURL:   "some-other-url",
									GroupClaim:    "test",
									UsernameClaim: "test",
								},
							},
						},
						Authorization: &lokiv1.AuthorizationSpec{
							OPA: &lokiv1.OPASpec{
								URL: "some-url",
							},
							Roles: []lokiv1.RoleSpec{
								{
									Name:        "some-name",
									Resources:   []string{"test"},
									Tenants:     []string{"test"},
									Permissions: []lokiv1.PermissionType{"read"},
								},
							},
						},
					},
				},
			},
		},
		{
			name:    "incompatible roleBindings configuration provided",
			wantErr: "incompatible configuration - static roleBindings not required for mode dynamic",
			stack: &lokiv1.LokiStack{
				TypeMeta: metav1.TypeMeta{
					Kind: "LokiStack",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-stack",
					Namespace: "some-ns",
					UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
				},
				Spec: lokiv1.LokiStackSpec{
					Size: lokiv1.SizeOneXExtraSmall,
					Tenants: &lokiv1.TenantsSpec{
						Mode: "dynamic",
						Authentication: []lokiv1.AuthenticationSpec{
							{
								TenantName: "test",
								TenantID:   "1234",
								OIDC: &lokiv1.OIDCSpec{
									IssuerURL:     "some-url",
									RedirectURL:   "some-other-url",
									GroupClaim:    "test",
									UsernameClaim: "test",
								},
							},
						},
						Authorization: &lokiv1.AuthorizationSpec{
							OPA: &lokiv1.OPASpec{
								URL: "some-url",
							},
							RoleBindings: []lokiv1.RoleBindingsSpec{
								{
									Name: "some-name",
									Subjects: []lokiv1.Subject{
										{
											Name: "sub-1",
											Kind: "user",
										},
									},
									Roles: []string{"some-role"},
								},
							},
						},
					},
				},
			},
		},
		{
			name:    "all set",
			wantErr: "",
			stack: &lokiv1.LokiStack{
				TypeMeta: metav1.TypeMeta{
					Kind: "LokiStack",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-stack",
					Namespace: "some-ns",
					UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
				},
				Spec: lokiv1.LokiStackSpec{
					Size: lokiv1.SizeOneXExtraSmall,
					Tenants: &lokiv1.TenantsSpec{
						Mode: "dynamic",
						Authentication: []lokiv1.AuthenticationSpec{
							{
								TenantName: "test",
								TenantID:   "1234",
								OIDC: &lokiv1.OIDCSpec{
									IssuerURL:     "some-url",
									RedirectURL:   "some-other-url",
									GroupClaim:    "test",
									UsernameClaim: "test",
								},
							},
						},
						Authorization: &lokiv1.AuthorizationSpec{
							OPA: &lokiv1.OPASpec{
								URL: "some-url",
							},
						},
					},
				},
			},
		},
	}
	for _, tst := range table {
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			err := validateModes(tst.stack)
			if tst.wantErr != "" {
				require.EqualError(t, err, tst.wantErr)
			}
		})
	}
}

func TestValidateModes_OpenshiftLoggingMode(t *testing.T) {
	type test struct {
		name    string
		wantErr string
		stack   *lokiv1.LokiStack
	}
	table := []test{
		{
			name:    "incompatible authentication spec provided",
			wantErr: "incompatible configuration - custom tenants configuration not required",
			stack: &lokiv1.LokiStack{
				TypeMeta: metav1.TypeMeta{
					Kind: "LokiStack",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-stack",
					Namespace: "some-ns",
					UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
				},
				Spec: lokiv1.LokiStackSpec{
					Size: lokiv1.SizeOneXExtraSmall,
					Tenants: &lokiv1.TenantsSpec{
						Mode: "openshift-logging",
						Authentication: []lokiv1.AuthenticationSpec{
							{
								TenantName: "test",
								TenantID:   "1234",
								OIDC: &lokiv1.OIDCSpec{
									IssuerURL:     "some-url",
									RedirectURL:   "some-other-url",
									GroupClaim:    "test",
									UsernameClaim: "test",
								},
							},
						},
					},
				},
			},
		},
		{
			name:    "incompatible authorization spec provided",
			wantErr: "incompatible configuration - custom tenants configuration not required",
			stack: &lokiv1.LokiStack{
				TypeMeta: metav1.TypeMeta{
					Kind: "LokiStack",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-stack",
					Namespace: "some-ns",
					UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
				},
				Spec: lokiv1.LokiStackSpec{
					Size: lokiv1.SizeOneXExtraSmall,
					Tenants: &lokiv1.TenantsSpec{
						Mode:           "openshift-logging",
						Authentication: nil,
						Authorization: &lokiv1.AuthorizationSpec{
							OPA: &lokiv1.OPASpec{
								URL: "some-url",
							},
						},
					},
				},
			},
		},
		{
			name:    "all set",
			wantErr: "",
			stack: &lokiv1.LokiStack{
				TypeMeta: metav1.TypeMeta{
					Kind: "LokiStack",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-stack",
					Namespace: "some-ns",
					UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
				},
				Spec: lokiv1.LokiStackSpec{
					Size: lokiv1.SizeOneXExtraSmall,
					Tenants: &lokiv1.TenantsSpec{
						Mode: "openshift-logging",
					},
				},
			},
		},
	}
	for _, tst := range table {
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			err := validateModes(tst.stack)
			if tst.wantErr != "" {
				require.EqualError(t, err, tst.wantErr)
			}
		})
	}
}
