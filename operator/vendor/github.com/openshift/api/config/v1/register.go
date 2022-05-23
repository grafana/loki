package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	GroupName     = "config.openshift.io"
	GroupVersion  = schema.GroupVersion{Group: GroupName, Version: "v1"}
	schemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)
	// Install is a function which adds this version to a scheme
	Install = schemeBuilder.AddToScheme

	// SchemeGroupVersion generated code relies on this name
	// Deprecated
	SchemeGroupVersion = GroupVersion
	// AddToScheme exists solely to keep the old generators creating valid code
	// DEPRECATED
	AddToScheme = schemeBuilder.AddToScheme
)

// Resource generated code relies on this being here, but it logically belongs to the group
// DEPRECATED
func Resource(resource string) schema.GroupResource {
	return schema.GroupResource{Group: GroupName, Resource: resource}
}

// Adds the list of known types to api.Scheme.
func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion,
		&APIServer{},
		&APIServerList{},
		&Authentication{},
		&AuthenticationList{},
		&Build{},
		&BuildList{},
		&ClusterOperator{},
		&ClusterOperatorList{},
		&ClusterVersion{},
		&ClusterVersionList{},
		&Console{},
		&ConsoleList{},
		&DNS{},
		&DNSList{},
		&FeatureGate{},
		&FeatureGateList{},
		&Image{},
		&ImageList{},
		&Infrastructure{},
		&InfrastructureList{},
		&Ingress{},
		&IngressList{},
		&Network{},
		&NetworkList{},
		&OAuth{},
		&OAuthList{},
		&OperatorHub{},
		&OperatorHubList{},
		&Project{},
		&ProjectList{},
		&Proxy{},
		&ProxyList{},
		&Scheduler{},
		&SchedulerList{},
	)
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
