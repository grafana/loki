//go:build go1.16
// +build go1.16

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azcore

import (
	"reflect"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/internal/pollers"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
)

// holds sentinel values used to send nulls
var nullables map[reflect.Type]interface{} = map[reflect.Type]interface{}{}

// NullValue is used to send an explicit 'null' within a request.
// This is typically used in JSON-MERGE-PATCH operations to delete a value.
func NullValue(v interface{}) interface{} {
	t := reflect.TypeOf(v)
	if k := t.Kind(); k != reflect.Ptr && k != reflect.Slice && k != reflect.Map {
		// t is not of pointer type, make it be of pointer type
		t = reflect.PtrTo(t)
	}
	v, found := nullables[t]
	if !found {
		var o reflect.Value
		if k := t.Kind(); k == reflect.Slice || k == reflect.Map {
			o = reflect.New(t) // *[]type / *map[]
			o = o.Elem()       // []type / map[]
		} else {
			o = reflect.New(t.Elem())
		}
		v = o.Interface()
		nullables[t] = v
	}
	// return the sentinel object
	return v
}

// IsNullValue returns true if the field contains a null sentinel value.
// This is used by custom marshallers to properly encode a null value.
func IsNullValue(v interface{}) bool {
	// see if our map has a sentinel object for this *T
	t := reflect.TypeOf(v)
	if k := t.Kind(); k != reflect.Ptr && k != reflect.Slice && k != reflect.Map {
		// v isn't a pointer type so it can never be a null
		return false
	}
	if o, found := nullables[t]; found {
		o1 := reflect.ValueOf(o)
		v1 := reflect.ValueOf(v)
		// we found it; return true if v points to the sentinel object.
		// NOTE: maps and slices can only be compared to nil, else you get
		// a runtime panic.  so we compare addresses instead.
		return o1.Pointer() == v1.Pointer()
	}
	// no sentinel object for this *t
	return false
}

// ClientOptions contains configuration settings for a client's pipeline.
type ClientOptions = policy.ClientOptions

// Poller encapsulates state and logic for polling on long-running operations.
type Poller = pollers.Poller
