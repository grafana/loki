package envtest

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	crdScheme = runtime.NewScheme()
)

// init is required to correctly initialize the crdScheme package variable.
func init() {
	_ = apiextensionsv1.AddToScheme(crdScheme)
	_ = apiextensionsv1beta1.AddToScheme(crdScheme)
}

// mergePaths merges two string slices containing paths.
// This function makes no guarantees about order of the merged slice.
func mergePaths(s1, s2 []string) []string {
	m := make(map[string]struct{})
	for _, s := range s1 {
		m[s] = struct{}{}
	}
	for _, s := range s2 {
		m[s] = struct{}{}
	}
	merged := make([]string, len(m))
	i := 0
	for key := range m {
		merged[i] = key
		i++
	}
	return merged
}

// mergeCRDs merges two CRD slices using their names.
// This function makes no guarantees about order of the merged slice.
func mergeCRDs(s1, s2 []client.Object) []client.Object {
	m := make(map[string]*unstructured.Unstructured)
	for _, obj := range runtimeCRDListToUnstructured(s1) {
		m[obj.GetName()] = obj
	}
	for _, obj := range runtimeCRDListToUnstructured(s2) {
		m[obj.GetName()] = obj
	}
	merged := make([]client.Object, len(m))
	i := 0
	for _, obj := range m {
		merged[i] = obj
		i++
	}
	return merged
}

func runtimeCRDListToUnstructured(l []client.Object) []*unstructured.Unstructured {
	res := []*unstructured.Unstructured{}
	for _, obj := range l {
		u := &unstructured.Unstructured{}
		if err := crdScheme.Convert(obj, u, nil); err != nil {
			log.Error(err, "error converting to unstructured object", "object-kind", obj.GetObjectKind())
			continue
		}
		res = append(res, u)
	}
	return res
}
