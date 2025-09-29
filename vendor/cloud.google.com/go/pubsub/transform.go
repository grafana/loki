// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pubsub

import (
	pb "cloud.google.com/go/pubsub/apiv1/pubsubpb"
)

// MessageTransform is a single instance of a message transformation to apply to messages.
type MessageTransform struct {
	// The transform to apply to messages.
	// If multiple JavaScriptUDF's are specified on a resource,
	// each must have a unique `function_name`.
	Transform Transform

	// If true, the transform is disabled and will not be applied to
	// messages. Defaults to `false`.
	Disabled bool
}

func messageTransformsToProto(m []MessageTransform) []*pb.MessageTransform {
	if m == nil {
		return nil
	}
	var transforms []*pb.MessageTransform
	for _, mt := range m {
		switch transform := mt.Transform.(type) {
		case JavaScriptUDF:
			transforms = append(transforms, &pb.MessageTransform{
				Disabled:  mt.Disabled,
				Transform: transform.toProto(),
			})
		default:
		}
	}
	return transforms
}

func protoToMessageTransforms(m []*pb.MessageTransform) []MessageTransform {
	if m == nil {
		return nil
	}
	var transforms []MessageTransform
	for _, mt := range m {
		switch t := mt.Transform.(type) {
		case *pb.MessageTransform_JavascriptUdf:
			transform := MessageTransform{
				Transform: protoToJavaScriptUDF(t),
				Disabled:  mt.Disabled,
			}
			transforms = append(transforms, transform)
		default:
		}
	}
	return transforms
}

// Transform represents the type of transforms that can be applied to messages.
// Currently JavaScriptUDF is the only type that satisfies this.
type Transform interface {
	isTransform() bool
}

// JavaScriptUDF is a user-defined JavaScript function
// that can transform or filter a Pub/Sub message.
type JavaScriptUDF struct {
	// Name of the JavaScript function that should applied to Pub/Sub
	// messages.
	FunctionName string

	// JavaScript code that contains a function `function_name` with the
	// below signature:
	//
	//   /**
	//   * Transforms a Pub/Sub message.
	//
	//   * @return {(Object<string, (string | Object<string, string>)>|null)} - To
	//   * filter a message, return `null`. To transform a message return a map
	//   * with the following keys:
	//   *   - (required) 'data' : {string}
	//   *   - (optional) 'attributes' : {Object<string, string>}
	//   * Returning empty `attributes` will remove all attributes from the
	//   * message.
	//   *
	//   * @param  {(Object<string, (string | Object<string, string>)>} Pub/Sub
	//   * message. Keys:
	//   *   - (required) 'data' : {string}
	//   *   - (required) 'attributes' : {Object<string, string>}
	//   *
	//   * @param  {Object<string, any>} metadata - Pub/Sub message metadata.
	//   * Keys:
	//   *   - (required) 'message_id'  : {string}
	//   *   - (optional) 'publish_time': {string} YYYY-MM-DDTHH:MM:SSZ format
	//   *   - (optional) 'ordering_key': {string}
	//   */
	//
	//   function <function_name>(message, metadata) {
	//   }
	Code string
}

var _ Transform = (*JavaScriptUDF)(nil)

func (i JavaScriptUDF) isTransform() bool {
	return true
}

func (j *JavaScriptUDF) toProto() *pb.MessageTransform_JavascriptUdf {
	return &pb.MessageTransform_JavascriptUdf{
		JavascriptUdf: &pb.JavaScriptUDF{
			FunctionName: j.FunctionName,
			Code:         j.Code,
		},
	}
}

func protoToJavaScriptUDF(m *pb.MessageTransform_JavascriptUdf) JavaScriptUDF {
	return JavaScriptUDF{
		FunctionName: m.JavascriptUdf.FunctionName,
		Code:         m.JavascriptUdf.Code,
	}
}
