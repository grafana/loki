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

	defaultConfigMapMode = int32(420)

	// ServingCertKey is the annotation key for services used the
	// cert-signing service to create a new key/cert pair signed
	// by the service CA stored in a secret with the same name
	// as the annotated service.
	ServingCertKey = "service.beta.openshift.io/serving-cert-secret-name"
	// InjectCABundleKey is the annotation key for configmaps used by the
	// cert-signing service to inject the service CA into the annotated
	// configmap.
	InjectCABundleKey = "service.beta.openshift.io/inject-cabundle"
)

func clusterRoleName(opts Options) string {
	return opts.BuildOpts.GatewayName
}

func ingressHost(stackName, namespace, baseDomain string) string {
	return fmt.Sprintf("%s-%s.apps.%s", stackName, namespace, baseDomain)
}

func routeName(opts Options) string {
	return opts.BuildOpts.LokiStackName
}

func serviceAccountName(opts Options) string {
	return opts.BuildOpts.GatewayName
}

func serviceCABundleName(opts Options) string {
	return fmt.Sprintf("%s-ca-bundle", opts.BuildOpts.GatewayName)
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
