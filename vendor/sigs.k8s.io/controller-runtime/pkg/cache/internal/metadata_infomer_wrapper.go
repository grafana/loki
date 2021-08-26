/*
Copyright 2021 The Kubernetes Authors.

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

package internal

import (
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
)

func metadataSharedIndexInformerPreserveGVK(gvk schema.GroupVersionKind, si cache.SharedIndexInformer) cache.SharedIndexInformer {
	return &sharedInformerWrapper{
		gvk:                 gvk,
		SharedIndexInformer: si,
	}
}

type sharedInformerWrapper struct {
	gvk schema.GroupVersionKind
	cache.SharedIndexInformer
}

func (s *sharedInformerWrapper) AddEventHandler(handler cache.ResourceEventHandler) {
	s.SharedIndexInformer.AddEventHandler(&handlerPreserveGVK{s.gvk, handler})
}

func (s *sharedInformerWrapper) AddEventHandlerWithResyncPeriod(handler cache.ResourceEventHandler, resyncPeriod time.Duration) {
	s.SharedIndexInformer.AddEventHandlerWithResyncPeriod(&handlerPreserveGVK{s.gvk, handler}, resyncPeriod)
}

type handlerPreserveGVK struct {
	gvk schema.GroupVersionKind
	cache.ResourceEventHandler
}

func (h *handlerPreserveGVK) resetGroupVersionKind(obj interface{}) {
	if v, ok := obj.(schema.ObjectKind); ok {
		v.SetGroupVersionKind(h.gvk)
	}
}

func (h *handlerPreserveGVK) OnAdd(obj interface{}) {
	h.resetGroupVersionKind(obj)
	h.ResourceEventHandler.OnAdd(obj)
}

func (h *handlerPreserveGVK) OnUpdate(oldObj, newObj interface{}) {
	h.resetGroupVersionKind(oldObj)
	h.resetGroupVersionKind(newObj)
	h.ResourceEventHandler.OnUpdate(oldObj, newObj)
}

func (h *handlerPreserveGVK) OnDelete(obj interface{}) {
	h.resetGroupVersionKind(obj)
	h.ResourceEventHandler.OnDelete(obj)
}
