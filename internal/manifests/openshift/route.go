package openshift

import (
	routev1 "github.com/openshift/api/route/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
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
			Namespace: opts.BuildOpts.GatewayNamespace,
			Labels:    opts.BuildOpts.Labels,
		},
		Spec: routev1.RouteSpec{
			To: routev1.RouteTargetReference{
				Kind:   "Service",
				Name:   opts.BuildOpts.GatewaySvcName,
				Weight: pointer.Int32(100),
			},
			Port: &routev1.RoutePort{
				TargetPort: intstr.FromString(opts.BuildOpts.GatewaySvcTargetPort),
			},
			WildcardPolicy: routev1.WildcardPolicyNone,
		},
	}
}
