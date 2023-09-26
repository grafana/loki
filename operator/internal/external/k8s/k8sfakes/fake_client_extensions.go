package k8sfakes

import (
	"reflect"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SetClientObject sets out to v.
// This is primarily used within the GetStub to fake the object returned from the API to the vaule of v
//
// Examples:
//
//	k.GetStub = func(_ context.Context, _ types.NamespacedName, object client.Object) error {
//		k.SetClientObject(object, &stack)
//		return nil
//	}
func (fake *FakeClient) SetClientObject(out, v client.Object) {
	reflect.Indirect(reflect.ValueOf(out)).Set(reflect.ValueOf(v).Elem())
}

// SetClientObjectList sets out list to v.
// This is primarily used within the GetStub to fake the object returned from the API to the vaule of v
//
// Examples:
//
//	k.GetStub = func(_ context.Context, _ types.NamespacedName, list client.ObjectList) error {
//		k.SetClientObjectList(list, &podList)
//		return nil
//	}
func (fake *FakeClient) SetClientObjectList(out, v client.ObjectList) {
	reflect.Indirect(reflect.ValueOf(out)).Set(reflect.ValueOf(v).Elem())
}
