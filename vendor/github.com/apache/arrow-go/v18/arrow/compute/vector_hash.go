// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements.  See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership.  The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build go1.18

package compute

import (
	"context"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/compute/internal/kernels"
)

var (
	uniqueDoc = FunctionDoc{
		Summary: "Compute unique elements",
		Description: "Return an array with distinct values.\n" +
			"Nulls in the input are considered a distinct value",
		ArgNames: []string{"array"},
	}
	dictionaryEncodeDoc = FunctionDoc{
		Summary: "Dictionary encode an array",
		Description: "Return a dictionary-encoded array with the distinct values in the dictionary.\n" +
			"If the input is already dictionary encoded, it is returned unchanged,\n" +
			"including its index type and null representation.\n" +
			"Newly encoded arrays use int32 dictionary indices.",
		ArgNames:    []string{"array"},
		OptionsType: "DictionaryEncodeOptions",
	}
)

// NullEncodingBehavior controls how null input values are represented.
type NullEncodingBehavior = kernels.NullEncodingBehavior

const (
	// NullEncodingMask keeps null input values null in the indices array.
	NullEncodingMask = kernels.NullEncodingMask
	// NullEncodingEncode adds null input values to the dictionary as a regular entry.
	NullEncodingEncode = kernels.NullEncodingEncode
)

// DictionaryEncodeOptions controls dictionary encoding behavior.
type DictionaryEncodeOptions = kernels.DictionaryEncodeOptions

func Unique(ctx context.Context, values Datum) (Datum, error) {
	return CallFunction(ctx, "unique", nil, values)
}

func UniqueArray(ctx context.Context, values arrow.Array) (arrow.Array, error) {
	out, err := Unique(ctx, &ArrayDatum{Value: values.Data()})
	if err != nil {
		return nil, err
	}
	defer out.Release()

	return out.(*ArrayDatum).MakeArray(), nil
}

// DictionaryEncode returns a dictionary-encoded version of values.
// Newly encoded arrays use int32 indices. For newly encoded arrays, nulls are
// masked unless NullEncodingEncode is selected. Existing dictionary arrays are
// returned unchanged, including their index type and null representation.
func DictionaryEncode(ctx context.Context, opts DictionaryEncodeOptions, values Datum) (Datum, error) {
	return CallFunction(ctx, "dictionary_encode", &opts, values)
}

// DictionaryEncodeArray returns a dictionary-encoded version of values.
func DictionaryEncodeArray(ctx context.Context, opts DictionaryEncodeOptions, values arrow.Array) (arrow.Array, error) {
	datum, err := DictionaryEncode(ctx, opts, &ArrayDatum{Value: values.Data()})
	if err != nil {
		return nil, err
	}
	defer datum.Release()

	switch out := datum.(type) {
	case *ArrayDatum:
		return out.MakeArray(), nil
	case *ChunkedDatum:
		return array.Concatenate(out.Chunks(), GetAllocator(ctx))
	default:
		return nil, fmt.Errorf(
			"%w: dictionary_encode returned unexpected datum kind %s",
			arrow.ErrInvalid,
			datum.Kind(),
		)
	}
}

func RegisterVectorHash(reg FunctionRegistry) {
	unique, _, dictEncode := kernels.GetVectorHashKernels()
	uniqFn := NewVectorFunction("unique", Unary(), uniqueDoc)
	for _, vd := range unique {
		if err := uniqFn.AddKernel(vd); err != nil {
			panic(err)
		}
	}
	reg.AddFunction(uniqFn, false)

	dictFn := NewVectorFunction("dictionary_encode", Unary(), dictionaryEncodeDoc)
	dictFn.SetDefaultOptions(&DictionaryEncodeOptions{})
	for _, vd := range dictEncode {
		if err := dictFn.AddKernel(vd); err != nil {
			panic(err)
		}
	}
	reg.AddFunction(dictFn, false)
}
