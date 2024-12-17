package openshift

import (
	"fmt"

	routev1 "github.com/openshift/api/route/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BuildRoute builds an OpenShift route object for the LokiStack Gateway
func BuildRoute(opts Options) client.Object {
	return &routev1.Route{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Route",
			APIVersion: routev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      routeName(opts),
			Namespace: opts.BuildOpts.LokiStackNamespace,
			Labels:    opts.BuildOpts.Labels,
			Annotations: map[string]string{
				annotationGatewayRouteTimeout: fmt.Sprintf("%.fs", opts.BuildOpts.GatewayRouteTimeout.Seconds()),
			},
		},
		Spec: routev1.RouteSpec{
			To: routev1.RouteTargetReference{
				Kind:   "Service",
				Name:   opts.BuildOpts.GatewaySvcName,
				Weight: ptr.To[int32](100),
			},
			Port: &routev1.RoutePort{
				TargetPort: intstr.FromString(opts.BuildOpts.GatewaySvcTargetPort),
			},
			TLS: &routev1.TLSConfig{
				Termination: routev1.TLSTerminationReencrypt,
			},
			WildcardPolicy: routev1.WildcardPolicyNone,
		},
	}
}
