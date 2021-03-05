/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package client

import (
	"fmt"

	jsonpatch "github.com/evanphx/json-patch"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
)

var (
	// Apply uses server-side apply to patch the given object.
	Apply = applyPatch{}

	// Merge uses the raw object as a merge patch, without modifications.
	// Use MergeFrom if you wish to compute a diff instead.
	Merge = mergePatch{}
)

type patch struct {
	patchType types.PatchType
	data      []byte
}

// Type implements Patch.
func (s *patch) Type() types.PatchType {
	return s.patchType
}

// Data implements Patch.
func (s *patch) Data(obj runtime.Object) ([]byte, error) {
	return s.data, nil
}

// RawPatch constructs a new Patch with the given PatchType and data.
func RawPatch(patchType types.PatchType, data []byte) Patch {
	return &patch{patchType, data}
}

// MergeFromWithOptimisticLock can be used if clients want to make sure a patch
// is being applied to the latest resource version of an object.
//
// The behavior is similar to what an Update would do, without the need to send the
// whole object. Usually this method is useful if you might have multiple clients
// acting on the same object and the same API version, but with different versions of the Go structs.
//
// For example, an "older" copy of a Widget that has fields A and B, and a "newer" copy with A, B, and C.
// Sending an update using the older struct definition results in C being dropped, whereas using a patch does not.
type MergeFromWithOptimisticLock struct{}

// ApplyToMergeFrom applies this configuration to the given patch options.
func (m MergeFromWithOptimisticLock) ApplyToMergeFrom(in *MergeFromOptions) {
	in.OptimisticLock = true
}

// MergeFromOption is some configuration that modifies options for a merge-from patch data.
type MergeFromOption interface {
	// ApplyToMergeFrom applies this configuration to the given patch options.
	ApplyToMergeFrom(*MergeFromOptions)
}

// MergeFromOptions contains options to generate a merge-from patch data.
type MergeFromOptions struct {
	// OptimisticLock, when true, includes `metadata.resourceVersion` into the final
	// patch data. If the `resourceVersion` field doesn't match what's stored,
	// the operation results in a conflict and clients will need to try again.
	OptimisticLock bool
}

type mergeFromPatch struct {
	from runtime.Object
	opts MergeFromOptions
}

// Type implements patch.
func (s *mergeFromPatch) Type() types.PatchType {
	return types.MergePatchType
}

// Data implements Patch.
func (s *mergeFromPatch) Data(obj runtime.Object) ([]byte, error) {
	originalJSON, err := json.Marshal(s.from)
	if err != nil {
		return nil, err
	}

	modifiedJSON, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}

	data, err := jsonpatch.CreateMergePatch(originalJSON, modifiedJSON)
	if err != nil {
		return nil, err
	}

	if s.opts.OptimisticLock {
		dataMap := map[string]interface{}{}
		if err := json.Unmarshal(data, &dataMap); err != nil {
			return nil, err
		}
		fromMeta, ok := s.from.(metav1.Object)
		if !ok {
			return nil, fmt.Errorf("cannot use OptimisticLock, from object %q is not a valid metav1.Object", s.from)
		}
		resourceVersion := fromMeta.GetResourceVersion()
		if len(resourceVersion) == 0 {
			return nil, fmt.Errorf("cannot use OptimisticLock, from object %q does not have any resource version we can use", s.from)
		}
		u := &unstructured.Unstructured{Object: dataMap}
		u.SetResourceVersion(resourceVersion)
		data, err = json.Marshal(u)
		if err != nil {
			return nil, err
		}
	}

	return data, nil
}

// MergeFrom creates a Patch that patches using the merge-patch strategy with the given object as base.
func MergeFrom(obj runtime.Object) Patch {
	return &mergeFromPatch{from: obj}
}

// MergeFromWithOptions creates a Patch that patches using the merge-patch strategy with the given object as base.
func MergeFromWithOptions(obj runtime.Object, opts ...MergeFromOption) Patch {
	options := &MergeFromOptions{}
	for _, opt := range opts {
		opt.ApplyToMergeFrom(options)
	}
	return &mergeFromPatch{from: obj, opts: *options}
}

// mergePatch uses a raw merge strategy to patch the object.
type mergePatch struct{}

// Type implements Patch.
func (p mergePatch) Type() types.PatchType {
	return types.MergePatchType
}

// Data implements Patch.
func (p mergePatch) Data(obj runtime.Object) ([]byte, error) {
	// NB(directxman12): we might technically want to be using an actual encoder
	// here (in case some more performant encoder is introduced) but this is
	// correct and sufficient for our uses (it's what the JSON serializer in
	// client-go does, more-or-less).
	return json.Marshal(obj)
}

// applyPatch uses server-side apply to patch the object.
type applyPatch struct{}

// Type implements Patch.
func (p applyPatch) Type() types.PatchType {
	return types.ApplyPatchType
}

// Data implements Patch.
func (p applyPatch) Data(obj runtime.Object) ([]byte, error) {
	// NB(directxman12): we might technically want to be using an actual encoder
	// here (in case some more performant encoder is introduced) but this is
	// correct and sufficient for our uses (it's what the JSON serializer in
	// client-go does, more-or-less).
	return json.Marshal(obj)
}
