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

package admission

import (
	"context"
	"encoding/json"
	"net/http"

	"k8s.io/apimachinery/pkg/runtime"
)

// Defaulter defines functions for setting defaults on resources
type Defaulter interface {
	runtime.Object
	Default()
}

// DefaultingWebhookFor creates a new Webhook for Defaulting the provided type.
func DefaultingWebhookFor(defaulter Defaulter) *Webhook {
	return &Webhook{
		Handler: &mutatingHandler{defaulter: defaulter},
	}
}

type mutatingHandler struct {
	defaulter Defaulter
	decoder   *Decoder
}

var _ DecoderInjector = &mutatingHandler{}

// InjectDecoder injects the decoder into a mutatingHandler.
func (h *mutatingHandler) InjectDecoder(d *Decoder) error {
	h.decoder = d
	return nil
}

// Handle handles admission requests.
func (h *mutatingHandler) Handle(ctx context.Context, req Request) Response {
	if h.defaulter == nil {
		panic("defaulter should never be nil")
	}

	// Get the object in the request
	obj := h.defaulter.DeepCopyObject().(Defaulter)
	err := h.decoder.Decode(req, obj)
	if err != nil {
		return Errored(http.StatusBadRequest, err)
	}

	// Default the object
	obj.Default()
	marshalled, err := json.Marshal(obj)
	if err != nil {
		return Errored(http.StatusInternalServerError, err)
	}

	// Create the patch
	return PatchResponseFromRaw(req.Object.Raw, marshalled)
}
