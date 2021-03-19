package k8sfakes

import (
	"context"
	"reflect"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetSetsFrom is a shortcut that makes Get set the client.Object to the value of actual
//
// Examples:
//   	stack := lokiv1beta1.LokiStack{...}
//      fake.GetSetsFrom(&stack)
//
//      var out lokiv1beta1.LokiStack
//      fake.Get(..., &out)
//      // out == stack
func (fake *FakeClient) GetSetsFrom(actual client.Object) {
	fake.GetStub = func(_ context.Context, name types.NamespacedName, o client.Object) error {
		reflect.Indirect(reflect.ValueOf(o)).Set(reflect.ValueOf(actual).Elem())
		return nil
	}
}
