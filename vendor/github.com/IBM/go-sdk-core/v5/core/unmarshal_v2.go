/**
 * (C) Copyright IBM Corp. 2020.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
)

const (
	errorPropertyInsert        = " property '%s' as"
	errorPropertyNameMissing   = "the 'propertyName' parameter is required"
	errorUnmarshalPrimitive    = "error unmarshalling property '%s': %s"
	errorUnmarshalModel        = "error unmarshalling%s %s: %s"
	errorIncorrectInputType    = "expected 'rawInput' to be a %s but was %s"
	errorUnsupportedResultType = "unsupported 'result' type: %s"
	errorUnmarshallInputIsNil  = "input to unmarshall is nil"
)

// UnmarshalPrimitive retrieves the specified property from 'rawInput',
// then unmarshals the resulting value into 'result'.
//
// This function will typically be invoked from within a model's generated "Unmarshal<model>()" method to unmarshal
// a struct field (model property) involving a primitive type (a scalar value, a slice, a map of the primitive, etc.).
// In this context, "primitive" refers to a type other than a user-defined model type within an OpenAPI definition.
// The 'rawInput' parameter is expected to be a map that contains an instance of a user-defined model type.
//
// Parameters:
//
// rawInput: the unmarshal input source in the form of a map[string]json.RawMessage
// The value 'rawInput[propertyName]' must be a json.RawMessage that contains an instance of the type inferred
// from the value passed in as 'result'.
//
// propertyName: the name of the property (map entry) to retrieve from 'rawInput'. This entry's value will
// be used as the unmarshal input source.
//
// result: a pointer to the unmarshal destination. This could be any of the following:
//   - *<primitive-type>
//   - **<primitive-type>
//   - *[]<primitive-type>
//   - *[][]<primitive-type>
//   - *map[string]<primitive-type>
//   - *map[string][]<primitive-type>
//   - *[]map[string]<primitive-type>
//   - *[]map[string][]<primitive-type>
//
// Where <primitive-type> could be any of the following:
//   - string, bool, []byte, int64, float32, float64, strfmt.Date, strfmt.DateTime,
//     strfmt.UUID, interface{} (any), or map[string]interface{} (any object).
//
// Example:
//
//	type MyStruct struct {
//	    Field1 *string,
//	    Field2 map[string]int64
//	}
//
// myStruct := new(MyStruct)
// jsonString := `{ "field1": "value1", "field2": {"foo": 44, "bar": 74}}`
// var rawMessageMap map[string]json.RawMessage
// var err error
// err = UnmarshalPrimitive(rawMessageMap, "field1", &myStruct.Field1)
// err = UnmarshalPrimitive(rawMessageMap, "field2", &myString.Field2)
func UnmarshalPrimitive(rawInput map[string]json.RawMessage, propertyName string, result interface{}) (err error) {
	if propertyName == "" {
		err = errors.New(errorPropertyNameMissing)
		err = SDKErrorf(err, "", "no-prop-name", getComponentInfo())
	}

	rawMsg, foundIt := rawInput[propertyName]
	if foundIt && rawMsg != nil {
		err = json.Unmarshal(rawMsg, result)
		if err != nil {
			err = fmt.Errorf(errorUnmarshalPrimitive, propertyName, err.Error())
			err = SDKErrorf(err, "", "json-unmarshal-error", getComponentInfo())
		}
	}
	return
}

// ModelUnmarshaller defines the interface for a generated Unmarshal<model>() function, which is used
// by the various "UnmarshalModel" functions below to unmarshal an instance of the user-defined model type.
//
// Parameters:
// rawInput: a map[string]json.RawMessage that is assumed to contain an instance of the model type.
//
// result: the unmarshal destination.  This should be a **<model> (i.e. a ptr to a ptr to a model instance).
// A new instance of the model is constructed by the unmarshaller function and is returned through the ptr
// passed in as 'result'.
type ModelUnmarshaller func(rawInput map[string]json.RawMessage, result interface{}) error

// UnmarshalModel unmarshals 'rawInput' into 'result' while using 'unmarshaller' to unmarshal model instances.
// This function is the single public interface to the various flavors of model-related unmarshal functions.
// The values passed in for the 'rawInput', 'propertyName' and 'result' fields will determine the function performed.
//
// Parameters:
// rawInput: the unmarshal input source.  The various types associated with this parameter are described below in
// "Usage Notes".
//
// propertyName: an optional property name.  If specified as "", then 'rawInput' is assumed to directly contain
// the input source to be used for the unmarshal operation.
// If propertyName is specified as a non-empty string, then 'rawInput' is assumed to be a map[string]json.RawMessage,
// and rawInput[propertyName] contains the input source to be unmarshalled.
//
// result: the unmarshal destination.  This should be passed in as one of the following types of values:
//   - **<model> (a ptr to a ptr to a <model> instance)
//   - *[]<model> (a ptr to a <model> slice)
//   - *[][]<model> (a ptr to a slice of <model> slices)
//   - *map[string]<model> (a ptr to a map of <model> instances)
//   - *map[string][]<model> (a ptr to a map of <model> slices)
//
// unmarshaller: the unmarshaller function to be used to unmarshal each model instance
//
// Usage Notes:
// if 'result' is a:  | and propertyName is:     | then 'rawInput' should be:
// -------------------+--------------------------+------------------------------------------------------------------
// **Foo              | == ""                    | a map[string]json.RawMessage which directly
//
//	|                          | contains an instance of model Foo (i.e. each map entry represents
//	|                          | a property of Foo)
//	|                          |
//
// **Foo              | != "" (e.g. "prop")      | a map[string]json.RawMessage and rawInput["prop"]
//
//	|                          | should contain an instance of Foo (i.e. it can itself be
//	|                          | unmarshalled into a map[string]json.RawMessage whose entries
//	|                          | represent the properties of Foo)
//
// -------------------+--------------------------+------------------------------------------------------------------
// *[]Foo             | == ""                    | a []json.RawMessage where each slice element contains
//
//	|                          | an instance of Foo (i.e. the json.RawMessage can be unmarshalled
//	|                          | into a Foo instance)
//	|                          |
//
// *[]Foo             | != "" (e.g. "prop")      | a map[string]json.RawMessage and rawInput["prop"]
//
//	|                          | contains a []Foo (slice of Foo instances)
//
// -------------------+--------------------------+------------------------------------------------------------------
// *[][]Foo           | == ""                    | a []json.RawMessage where each slice element contains
//
//	|                          | a []Foo (i.e. the json.RawMessage can be unmarshalled
//	|                          | into a []Foo)
//	|                          |
//
// *[][]Foo           | != "" (e.g. "prop")      | a map[string]json.RawMessage and rawInput["prop"]
//
//	|                          | contains a [][]Foo (slice of Foo slices)
//
// -------------------+--------------------------+------------------------------------------------------------------
// *map[string]Foo    | == ""                    | a map[string]json.RawMessage which directly contains the
//
//	|                          | map[string]Foo (i.e. the value within each entry in 'rawInput'
//	|                          | contains an instance of Foo)
//	|                          |
//
// *map[string]Foo    | != "" (e.g. "prop")      | a map[string]json.RawMessage and rawInput["prop"]
//
//	|                          | contains an instance of map[string]Foo
//
// -------------------+--------------------------+------------------------------------------------------------------
// *map[string][]Foo  | == ""                    | a map[string]json.RawMessage which directly contains the
//
//	|                          | map[string][]Foo (i.e. the value within each entry in 'rawInput'
//	|                          | contains a []Foo)
//
// *map[string][]Foo  | != "" (e.g. "prop")      | a map[string]json.RawMessage and rawInput["prop"]
//
//	|                          | contains an instance of map[string][]Foo
//
// -------------------+--------------------------+------------------------------------------------------------------
func UnmarshalModel(rawInput interface{}, propertyName string, result interface{}, unmarshaller ModelUnmarshaller) (err error) {
	// Make sure some input is provided. Otherwise return an error.
	if IsNil(rawInput) {
		err = errors.New(errorUnmarshallInputIsNil)
		err = SDKErrorf(err, "", "no-input", getComponentInfo())
		return
	}

	// Reflect on 'result' to determine the type of unmarshal operation being requested.
	rResultType := reflect.TypeOf(result).Elem()

	// Now delegate the work to the appropriate internal unmarshal function.
	switch rResultType.Kind() {
	case reflect.Ptr, reflect.Interface:
		// Unmarshal a single instance of a model.
		err = unmarshalModelInstance(rawInput, propertyName, result, unmarshaller)

	case reflect.Slice:
		// For a slice, we need to look at the slice element type.
		rElementType := rResultType.Elem()
		switch rElementType.Kind() {
		case reflect.Struct, reflect.Interface:
			// A []<model>
			// Slice element type is struct or intf, so must be a slice of model instances.
			// Unmarshal a slice of model instances.
			err = unmarshalModelSlice(rawInput, propertyName, result, unmarshaller)

		case reflect.Slice:
			// If the slice element type is slice (i.e. a slice of slices),
			// then we need to make sure that the inner slice's element type is a model (struct or intf).
			rInnerElementType := rElementType.Elem()
			switch rInnerElementType.Kind() {
			case reflect.Struct, reflect.Interface:
				// A [][]<model>
				err = unmarshalModelSliceSlice(rawInput, propertyName, result, unmarshaller)

			default:
				err = fmt.Errorf(errorUnsupportedResultType, rResultType.String())
				err = SDKErrorf(err, "", "bad-slice-elem-slice-type", getComponentInfo())
			}

		default:
			err = fmt.Errorf(errorUnsupportedResultType, rResultType.String())
			err = SDKErrorf(err, "", "bad-slice-elem-type", getComponentInfo())
			return
		}

	case reflect.Map:
		// For a map, we need to look at the map entry type.
		// We currently support map[string]<model> and map[string][]<model>.
		rEntryType := rResultType.Elem()
		switch rEntryType.Kind() {
		case reflect.Struct, reflect.Interface:
			// A map[string]<model>
			err = unmarshalModelMap(rawInput, propertyName, result, unmarshaller)
		case reflect.Slice:
			// If the map entry type is a slice, make sure it is a slice of model instances.
			rElementType := rEntryType.Elem()
			switch rElementType.Kind() {
			case reflect.Struct, reflect.Interface:
				// A map[string][]<model>
				err = unmarshalModelSliceMap(rawInput, propertyName, result, unmarshaller)

			default:
				err = fmt.Errorf(errorUnsupportedResultType, rResultType.String())
				err = SDKErrorf(err, "", "bad-slice-elem-map-type", getComponentInfo())
				return
			}
		default:
			err = fmt.Errorf(errorUnsupportedResultType, rResultType.String())
			err = SDKErrorf(err, "", "bad-map-entry-type", getComponentInfo())
			return
		}

	default:
		err = fmt.Errorf(errorUnsupportedResultType, rResultType.String())
		err = SDKErrorf(err, "", "bad-model-type", getComponentInfo())
		return
	}

	err = RepurposeSDKProblem(err, "unmarshal-fail")

	return
}

// unmarshalModelInstance unmarshals 'rawInput' into an instance of a model.
//
// Parameters:
//
// rawInput: the unmarshal input source.
//
// propertyName: the name of the property within 'rawInput' that contains the instance of the model.
// If 'propertyName' is specified as "" then 'rawInput' is assumed to contain the model instance directly.
//
// result: should be a ptr to a ptr to a model instance (e.g. **Foo for model type Foo).
// A new instance of the model will be constructed and returned through the model pointer
// that 'result' points to (i.e. it is legal for 'result' to point to a nil pointer).
//
// 'unmarshaller' is the generated unmarshal function used to unmarshal a single instance of the model.
func unmarshalModelInstance(rawInput interface{}, propertyName string, result interface{}, unmarshaller ModelUnmarshaller) (err error) {
	var rawMap map[string]json.RawMessage
	var foundInput bool

	// Obtain the unmarshal input source from 'rawInput'.
	foundInput, rawMap, err = getUnmarshalInputSourceMap(rawInput, propertyName)
	if err != nil {
		err = fmt.Errorf(errorUnmarshalModel, propInsert(propertyName), getModelResultType(result), err.Error())
		err = SDKErrorf(err, "", "input-source-error", getComponentInfo())
		return
	}

	// At this point, 'rawMap' should be the map[string]json.RawMessage which represents model instance:
	// i.e. rawMap --> `{ "prop1": "value1", "prop2": "value2", ...}"

	// Initialize our result to nil.
	// Note: 'result' is a ptr to a ptr to a model struct.
	// We're using 'result' to set the model struct ptr to nil.
	rResult := reflect.ValueOf(result).Elem()
	rResult.Set(reflect.Zero(rResult.Type()))

	// If there is an unmarshal input source, then unmarshal it.
	if foundInput && rawMap != nil {
		err = unmarshaller(rawMap, result)
		if err != nil {
			err = fmt.Errorf(errorUnmarshalModel, propInsert(propertyName), getModelResultType(result), err.Error())
			err = SDKErrorf(err, "", "unmarshaller-error", getComponentInfo())
			return
		}
	}
	return
}

// unmarshalModelSlice unmarshals 'rawInput' into a []<model>.
//
// Parameters:
//
// rawInput: is the unmarshal input source, and should be one of the following:
// 1. a map[string]json.RawMessage - in this case 'propertyName' should specify the name
// of the property to retrieve from the map to obtain the []json.RawMessage containing the slice of model instances
// to be unmarshalled.
//
// 2. a []json.RawMessage - in this case, 'propertyName' should be specified as "" to indicate that
// the []json.RawMessage is available directly via the 'rawInput' parameter.
//
// propertyName: an optional name of a property to be retrieved from 'rawInput'.
// If 'propertyName' is specified as a non-empty string, then 'rawInput' is assumed to be a map[string]RawMessage,
// and the named property is retrieved to obtain the []json.RawMessage to be unmarshalled.
// If 'propertyName' is specified as "", then 'rawInput' is assumed to be a []json.RawMessage and is used directly
// as the unmarshal input source.
//
// result: this should be a pointer to a slice of the model type (e.g. *[]Foo for model type Foo).
// This function will construct a new slice and return it through 'result'.
//
// 'unmarshaller' is the function used to unmarshal a single instance of the model.
func unmarshalModelSlice(rawInput interface{}, propertyName string, result interface{}, unmarshaller ModelUnmarshaller) (err error) {
	var rawSlice []json.RawMessage
	var foundInput bool

	// Obtain the unmarshal input source from 'rawInput'.
	foundInput, rawSlice, err = getUnmarshalInputSourceSlice(rawInput, propertyName)
	if err != nil {
		err = fmt.Errorf(errorUnmarshalModel, propInsert(propertyName),
			reflect.TypeOf(result).Elem().String(), err.Error())
		err = SDKErrorf(err, "", "input-source-error", getComponentInfo())
		return
	}

	if !foundInput {
		return
	}

	// At this point, 'rawSlice' should be the []json.RawMessage which represents the slice of model instances:
	// i.e. rawSlice --> "[ {...}, {...}, ...]"

	// 'sliceSize' is the number of elements found in 'rawSlice'.
	// if 'rawSlice' is nil, 'sliceSize' will be 0, which is what we want.
	sliceSize := len(rawSlice)

	// Get a reflective view of the result and initialize it.
	rResultSlice := reflect.ValueOf(result).Elem()
	rResultSlice.Set(reflect.MakeSlice(reflect.TypeOf(result).Elem(), 0, sliceSize))

	// If there is anything to unmarshal, then unmarshal it.
	if sliceSize > 0 {
		// Determine the type of the 'result' parameter that we'll need to pass to
		// the model-specific unmarshaller (i.e. a *Foo).
		var receiverType reflect.Type
		receiverType, err = getUnmarshalResultType(result)
		if err != nil {
			err = fmt.Errorf(errorUnmarshalModel, propInsert(propertyName),
				reflect.TypeOf(result).Elem().String(), err.Error())
			err = SDKErrorf(err, "", "result-type-error", getComponentInfo())
			return
		}

		for _, rawMsg := range rawSlice {
			// 'rawMsg' should contain an instance of the model - we need to unmarshal it into a map.
			rawMap := make(map[string]json.RawMessage)
			err = json.Unmarshal(rawMsg, &rawMap)
			if err != nil {
				err = fmt.Errorf(errorUnmarshalModel, propInsert(propertyName),
					reflect.TypeOf(result).Elem().String(), err.Error())
				err = SDKErrorf(err, "", "json-unmarshal-error", getComponentInfo())
				return
			}

			// Reflectively construct a receiver of the unmarshal step.
			rModelReceiver := reflect.New(receiverType)

			// Invoke the model-specific unmarshaller.
			err = unmarshaller(rawMap, rModelReceiver.Interface())
			if err != nil {
				err = fmt.Errorf(errorUnmarshalModel, propInsert(propertyName),
					reflect.TypeOf(result).Elem().String(), err.Error())
				err = SDKErrorf(err, "", "unmarshaller-error", getComponentInfo())
				return
			}

			// Add the model instance to the result slice (reflectively, of course :) ).
			rResultSlice.Set(reflect.Append(rResultSlice, rModelReceiver.Elem().Elem()))
		}
	}
	return
}

// unmarshalModelSliceSlice unmarshals 'rawInput' into a [][]<model>.
//
// Parameters:
//
// rawInput: is the unmarshal input source, and should be one of the following:
// 1. a map[string]json.RawMessage - in this case 'propertyName' should specify the name
// of the property to retrieve from the map to obtain the []json.RawMessage containing the slice of model slices
// to be unmarshalled.
//
// 2. a []json.RawMessage - in this case, 'propertyName' should be specified as "" to indicate that
// the []json.RawMessage is available directly via the 'rawInput' parameter.
//
// propertyName: an optional name of a property to be retrieved from 'rawInput'.
// If 'propertyName' is specified as a non-empty string, then 'rawInput' is assumed to be a map[string]json.RawMessage,
// and the named property is retrieved to obtain the []json.RawMessage to be unmarshalled.
// If 'propertyName' is specified as "", then 'rawInput' is assumed to be a []json.RawMessage and is used directly
// as the unmarshal input source.
//
// result: this should be a pointer to a slice of model slices (e.g. *[][]Foo for model type Foo).
// This function will construct a new slice of slices and return it through 'result'.
//
// 'unmarshaller' is the function used to unmarshal a single instance of the model.
func unmarshalModelSliceSlice(rawInput interface{}, propertyName string, result interface{}, unmarshaller ModelUnmarshaller) (err error) {
	var rawSlice []json.RawMessage
	var foundInput bool

	// Obtain the unmarshal input source from 'rawInput'.
	foundInput, rawSlice, err = getUnmarshalInputSourceSlice(rawInput, propertyName)
	if err != nil {
		err = fmt.Errorf(errorUnmarshalModel, propInsert(propertyName),
			reflect.TypeOf(result).Elem().String(), err.Error())
		err = SDKErrorf(err, "", "input-source-error", getComponentInfo())
		return
	}

	if !foundInput {
		return
	}

	// At this point, 'rawSlice' should be the []json.RawMessage which represents the overall slice whose
	// elements should be slices of model instances:
	// rawSlice --> "[ [{}, {},...], [{}, {},...], ...]"

	// Get a reflective view of the result and initialize it.
	// This will be a slice of slices.
	sliceSize := len(rawSlice)

	rResultSlice := reflect.ValueOf(result).Elem()
	rResultSlice.Set(reflect.MakeSlice(reflect.TypeOf(result).Elem(), 0, sliceSize))

	// If there is an unmarshal input source, then unmarshal it.
	if sliceSize > 0 {
		for _, rawMsg := range rawSlice {
			// Make sure our inner slice raw message isn't an explicit JSON null value.
			// Each value in 'rawMap' should contain an instance of []<model>.
			// We'll first unmarshal each value into a []jsonRawMessage, then unmarshal that
			// into a []<model> using unmarshalModelSlice.
			var innerRawSlice []json.RawMessage
			err = json.Unmarshal(rawMsg, &innerRawSlice)
			if err != nil {
				err = fmt.Errorf(errorUnmarshalModel, propInsert(propertyName),
					reflect.TypeOf(result).Elem().String(), err.Error())
				err = SDKErrorf(err, "", "json-unmarshal-error", getComponentInfo())
				return
			}

			// Construct a slice of the correct type (i.e. []Foo)
			rSliceValue := reflect.New(reflect.TypeOf(result).Elem().Elem())

			// Unmarshal 'innerRawSlice' into a slice of models.
			err = unmarshalModelSlice(innerRawSlice, "", rSliceValue.Interface(), unmarshaller)
			if err != nil {
				err = fmt.Errorf(errorUnmarshalModel, propInsert(propertyName),
					reflect.TypeOf(result).Elem().String(), err.Error())
				err = SDKErrorf(err, "", "unmarshaller-error", getComponentInfo())
				return
			}

			// Now add the unmarshalled model slice to the result slice (reflectively, of course :) ).
			rResultSlice.Set(reflect.Append(rResultSlice, rSliceValue.Elem()))
		}
	}
	return
}

// unmarshalModelMap unmarshals 'rawInput' into a map[string]<model>.
//
// Parameters:
//
// rawInput: the unmarshal input source in the form of a map[string]json.RawMessage.
//
// propertyName: an optional name of a property to be retrieved from 'rawInput'.
// If 'propertyName' is specified as a non-empty string, then the specified property value is retrieved
// from 'rawInput', and this value is assumed to contain the map[string]<model> to be unmarshalled.
// If 'propertyName' is specified as "", then 'rawInput' will be used directly as the unmarshal input source.
//
// result: the unmarshal destination. This should be a pointer to map[string]<model> (e.g. *map[string]Foo for model type Foo).
// If 'result' points to a nil map, this function will construct the map prior to adding entries to it.
//
// unmarshaller: the function used to unmarshal a single instance of the model.
func unmarshalModelMap(rawInput interface{}, propertyName string, result interface{}, unmarshaller ModelUnmarshaller) (err error) {
	var rawMap map[string]json.RawMessage
	var foundInput bool

	// Obtain the unmarshal input source from 'rawInput'.
	foundInput, rawMap, err = getUnmarshalInputSourceMap(rawInput, propertyName)
	if err != nil {
		err = fmt.Errorf(errorUnmarshalModel, propInsert(propertyName),
			reflect.TypeOf(result).Elem().String(), err.Error())
		err = SDKErrorf(err, "", "input-source-error", getComponentInfo())
		return
	}

	if !foundInput {
		return
	}

	// Get a "reflective" view of the result map and initialize it.
	rResultMap := reflect.ValueOf(result).Elem()
	rResultMap.Set(reflect.MakeMap(reflect.TypeOf(result).Elem()))

	// If there is an unmarshal input source, then unmarshal it.
	if foundInput && rawMap != nil {
		// Determine the type of the 'result' parameter that we'll need to pass to
		// the model-specific unmarshaller.
		var receiverType reflect.Type
		receiverType, err = getUnmarshalResultType(result)
		if err != nil {
			err = fmt.Errorf(errorUnmarshalModel, propInsert(propertyName),
				reflect.TypeOf(result).Elem().String(), err.Error())
			err = SDKErrorf(err, "", "result-type-error", getComponentInfo())
			return
		}

		for k, v := range rawMap {
			// Unmarshal the map entry's value (a json.RawMessage) into a map[string]RawMessage.
			// The resulting map should contain an instance of the model.
			modelInstanceMap := make(map[string]json.RawMessage)
			err = json.Unmarshal(v, &modelInstanceMap)
			if err != nil {
				err = fmt.Errorf(errorUnmarshalModel, propInsert(propertyName),
					reflect.TypeOf(result).Elem().String(), err.Error())
				err = SDKErrorf(err, "", "json-unmarshal-error", getComponentInfo())
				return
			}

			// Reflectively construct a receiver of the unmarshal step.
			rModelReceiver := reflect.New(receiverType)

			// Unmarshal the model instance contained in 'modelInstanceMap'.
			err = unmarshaller(modelInstanceMap, rModelReceiver.Interface())
			if err != nil {
				err = fmt.Errorf(errorUnmarshalModel, propInsert(propertyName),
					reflect.TypeOf(result).Elem().String(), err.Error())
				err = SDKErrorf(err, "", "unmarshaller-error", getComponentInfo())
				return
			}

			// Now add the unmarshalled model instance to the result map (reflectively, of course :) ).
			rResultMap.SetMapIndex(reflect.ValueOf(k), rModelReceiver.Elem().Elem())
		}
	}
	return
}

// unmarshalModelSliceMap unmarshals 'rawInput' into a map[string][]<model>.
//
// Parameters:
//
// rawInput: the unmarshal input source in the form of a map[string]json.RawMessage.
//
// propertyName: an optional name of a property to be retrieved from 'rawInput'.
// If 'propertyName' is specified as a non-empty string, then the specified property value is retrieved
// from 'rawInput', and this value is assumed to contain the map[string][]<model> to be unmarshalled.
// If 'propertyName' is specified as "", then 'rawInput' will be used directly as the unmarshal input source.
//
// result: the unmarshal destination. This should be a pointer to a map[string][]<model>
// (e.g. *map[string][]Foo for model type Foo).
// If 'result' points to a nil map, this function will construct the map prior to adding entries to it.
//
// unmarshaller: the function used to unmarshal a single instance of the model.
func unmarshalModelSliceMap(rawInput interface{}, propertyName string, result interface{}, unmarshaller ModelUnmarshaller) (err error) {
	var rawMap map[string]json.RawMessage
	var foundInput bool

	// Obtain the unmarshal input source from 'rawInput'.
	foundInput, rawMap, err = getUnmarshalInputSourceMap(rawInput, propertyName)
	if err != nil {
		err = fmt.Errorf(errorUnmarshalModel, propInsert(propertyName),
			reflect.TypeOf(result).Elem().String(), err.Error())
		err = SDKErrorf(err, "", "input-source-error", getComponentInfo())
		return
	}

	if !foundInput {
		return
	}

	// Get a "reflective" view of the result map and initialize it.
	rResultMap := reflect.ValueOf(result).Elem()
	rResultMap.Set(reflect.MakeMap(reflect.TypeOf(result).Elem()))

	if foundInput && rawMap != nil {
		for k, v := range rawMap {
			// Make sure our slice raw message isn't an explicit JSON null value.
			if !isJsonNull(v) {
				// Each value in 'rawMap' should contain an instance of []<model>.
				// We'll first unmarshal each value into a []jsonRawMessage, then unmarshal that
				// into a []<model> using unmarshalModelSlice.
				var rawSlice []json.RawMessage
				err = json.Unmarshal(v, &rawSlice)
				if err != nil {
					err = fmt.Errorf(errorUnmarshalModel, propInsert(propertyName),
						reflect.TypeOf(result).Elem().String(), err.Error())
					err = SDKErrorf(err, "", "json-unmarshal-error", getComponentInfo())
					return
				}

				// Construct a slice of the correct type.
				rSliceValue := reflect.New(reflect.TypeOf(result).Elem().Elem())

				// Unmarshal rawSlice into a model slice.
				err = unmarshalModelSlice(rawSlice, "", rSliceValue.Interface(), unmarshaller)
				if err != nil {
					err = fmt.Errorf(errorUnmarshalModel, propInsert(propertyName),
						reflect.TypeOf(result).Elem().String(), err.Error())
					err = SDKErrorf(err, "", "unmarshaller-error", getComponentInfo())
					return
				}

				// Now add the unmarshalled model slice to the result map (reflectively, of course :) ).
				rResultMap.SetMapIndex(reflect.ValueOf(k), rSliceValue.Elem())
			}
		}
	}
	return
}

// getUnmarshalInputSourceMap returns the appropriate unmarshal input source from 'rawInput'
// in the form of a map[string]json.RawMessage.
//
// Parameters:
// rawInput: the raw unmarshalled input.  This should be in the form of a map[string]json.RawMessage.
//
// propertyName: (optional) the name of the property (map entry) within 'rawInput' that contains the
// unmarshal input source.  If specified as "", then 'rawInput' is assumed to contain the entire input source directly.
func getUnmarshalInputSourceMap(rawInput interface{}, propertyName string) (foundInput bool, inputSource map[string]json.RawMessage, err error) {
	foundInput = true

	// rawInput should be a map[string]json.RawMessage.
	rawMap, ok := rawInput.(map[string]json.RawMessage)
	if !ok {
		foundInput = false
		err = fmt.Errorf(errorIncorrectInputType, "map[string]json.RawMessage", reflect.TypeOf(rawInput).String())
		err = SDKErrorf(err, "", "non-json-source-map", getComponentInfo())
		return
	}

	// If propertyName was specified, then retrieve that entry from 'rawInput' as our unmarshal input source.
	if propertyName != "" {
		var rawMsg json.RawMessage
		rawMsg, foundInput = rawMap[propertyName]
		if !foundInput || isJsonNull(rawMsg) {
			foundInput = false
			return
		} else {
			rawMap = make(map[string]json.RawMessage)
			err = json.Unmarshal(rawMsg, &rawMap)
			if err != nil {
				foundInput = false
				err = SDKErrorf(err, "", "json-unmarshal-error", getComponentInfo())
				return
			}
		}
	}

	inputSource = rawMap
	return
}

// getUnmarshalInputSourceSlice returns the appropriate unmarshal input source from 'rawInput'
// in the form of a []json.RawMessage.
//
// Parameters:
// rawInput: the raw unmarshalled input.  This should be in the form of a map[string]json.RawMessage or []json.RawMessage,
// depending on the value of propertyName.
//
// propertyName: (optional) the name of the property (map entry) within 'rawInput' that contains the
// unmarshal input source.  The specified map entry should contain a []json.RawMessage which
// will be used as the unmarshal input source. If 'propertyName' is specified as "", then 'rawInput'
// is assumed to be a []json.RawMessage and contains the entire input source directly.
func getUnmarshalInputSourceSlice(rawInput interface{}, propertyName string) (foundInput bool, inputSource []json.RawMessage, err error) {
	// If propertyName was specified, then retrieve that entry from 'rawInput' (assumed to be a map[string]json.RawMessage)
	// as our unmarshal input source.  Otherwise, just use 'rawInput' directly.
	if propertyName != "" {
		rawMap, ok := rawInput.(map[string]json.RawMessage)
		if !ok {
			err = fmt.Errorf(errorIncorrectInputType, "map[string]json.RawMessage", reflect.TypeOf(rawInput).String())
			err = SDKErrorf(err, "", "raw-input-error", getComponentInfo())
			return
		}

		var rawMsg json.RawMessage
		rawMsg, ok = rawMap[propertyName]

		// If we didn't find the property containing the JSON input, then bail out now.
		if !ok || isJsonNull(rawMsg) {
			return
		} else {
			// We found the property in the map, so unmarshal the json.RawMessage into a []json.RawMessage
			rawSlice := make([]json.RawMessage, 0)
			err = json.Unmarshal(rawMsg, &rawSlice)
			if err != nil {
				err = fmt.Errorf(errorIncorrectInputType, "map[string][]json.RawMessage", reflect.TypeOf(rawInput).String())
				err = SDKErrorf(err, "", "json-unmarshal-error", getComponentInfo())
				return
			}

			foundInput = true
			inputSource = rawSlice
		}
	} else {
		// 'propertyName' was not specified, so 'rawInput' should be our []json.RawMessage input source.

		rawSlice, ok := rawInput.([]json.RawMessage)
		if !ok {
			err = fmt.Errorf(errorIncorrectInputType, "[]json.RawMessage", reflect.TypeOf(rawInput).String())
			err = SDKErrorf(err, "", "no-name-raw-input-error", getComponentInfo())
			return
		}

		foundInput = true
		inputSource = rawSlice
	}

	return
}

// isJsonNull returns true iff 'rawMsg' is exlicitly nil or contains a JSON "null" value.
func isJsonNull(rawMsg json.RawMessage) bool {
	nullLiteral := []byte("null")
	if rawMsg == nil || string(rawMsg) == string(nullLiteral) {
		return true
	}
	return false
}

// propInsert is a utility function used to optionally include the name of a property in an error message.
func propInsert(propertyName string) string {
	if propertyName != "" {
		return fmt.Sprintf(errorPropertyInsert, propertyName)
	}
	return ""
}

// getUnmarshalResultType returns the type associated with an unmarshal result.
// The resulting type can be constructed with the reflect.New() function.
func getUnmarshalResultType(result interface{}) (ptrType reflect.Type, err error) {
	rResultType := reflect.TypeOf(result).Elem().Elem()
	switch rResultType.Kind() {
	case reflect.Struct, reflect.Slice:
		ptrType = reflect.PointerTo(rResultType)

	case reflect.Interface:
		ptrType = rResultType

	default:
		err = fmt.Errorf(errorUnsupportedResultType, rResultType.String())
		err = SDKErrorf(err, "", "unsupported-type", getComponentInfo())
	}
	return
}

// getModelResultType returns the type of the 'result' parameter as a string.
// This will be something like "mypackagev1.Foo" or "mypackagev1.FooIntf"
func getModelResultType(result interface{}) string {
	rResultType := reflect.TypeOf(result).Elem()
	if rResultType.Kind() == reflect.Ptr {
		rResultType = rResultType.Elem()
	}

	return rResultType.String()
}
