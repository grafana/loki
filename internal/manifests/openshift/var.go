package openshift

import (
	"fmt"
)

var (
	// GatewayOPAHTTPPort is the HTTP port of the OpenPolicyAgent sidecar.
	GatewayOPAHTTPPort int32 = 8082
	// GatewayOPAInternalPort is the HTTP metrics port of the OpenPolicyAgent sidecar.
	GatewayOPAInternalPort int32 = 8083

	// GatewayOPAHTTPPortName is the HTTP container port name of the OpenPolicyAgent sidecar.
	GatewayOPAHTTPPortName = "public"
	// GatewayOPAInternalPortName is the HTTP container metrics port name of the OpenPolicyAgent sidecar.
	GatewayOPAInternalPortName = "opa-metrics"

	bearerTokenFile string = "/var/run/secrets/kubernetes.io/serviceaccount/token"

	cookieSecretLength = 32
	allowedRunes       = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

	// ServingCertKey is the annotation key for using the cert-signing service
	// on k8s service objects.
	ServingCertKey = "service.beta.openshift.io/serving-cert-secret-name"
)

func clusterRoleName(opts Options) string {
	return opts.BuildOpts.GatewayName
}

func routeName(opts Options) string {
	return opts.BuildOpts.LokiStackName
}

func serviceAccountName(opts Options) string {
	return opts.BuildOpts.GatewayName
}

func serviceAccountAnnotations(opts Options) map[string]string {
	a := make(map[string]string, len(opts.Authentication))
	for _, auth := range opts.Authentication {
		key := fmt.Sprintf("serviceaccounts.openshift.io/oauth-redirectreference.%s", auth.TenantName)
		value := fmt.Sprintf("{\"kind\":\"OAuthRedirectReference\",\"apiVersion\":\"v1\",\"reference\":{\"kind\":\"Route\",\"name\":\"%s\"}}", routeName(opts))
		a[key] = value
	}

	return a
}
