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
	"github.com/apache/arrow-go/v18/arrow/compute/exec"
	"github.com/apache/arrow-go/v18/arrow/compute/internal/kernels"
	"github.com/apache/arrow-go/v18/arrow/scalar"
)

var (
	cumulativeSumDoc = FunctionDoc{
		Summary: "Compute the cumulative sum of numeric input",
		Description: `Return the cumulative sum of the input array. Integer values
wrap on overflow; nulls stop the remaining output unless SkipNulls is enabled.
A nil Start uses zero. For chunked input, accumulation continues across all
chunks and the result is returned as a chunked array.`,
		ArgNames:    []string{"values"},
		OptionsType: "CumulativeOptions",
	}
	cumulativeSumCheckedDoc = FunctionDoc{
		Summary: "Compute cumulative sum of numeric input with overflow checking",
		Description: `Return the cumulative sum of the input array and report
integer overflow. Null handling and Start follow CumulativeOptions. For
chunked input, accumulation continues across all chunks and the result is
returned as a chunked array.`,
		ArgNames:    []string{"values"},
		OptionsType: "CumulativeOptions",
	}
)

type CumulativeOptions = kernels.CumulativeOptions

func safeCastScalar(ctx *exec.KernelCtx, start scalar.Scalar, typ arrow.DataType) (scalar.Scalar, error) {
	input := NewDatumWithoutOwning(start)
	result, err := CastDatum(ctx.Ctx, input, SafeCastOptions(typ))
	if err != nil {
		return nil, err
	}

	casted, ok := result.(*ScalarDatum)
	if !ok {
		result.Release()
		return nil, fmt.Errorf("%w: safe cast of cumulative sum start value returned %T", arrow.ErrInvalid, result)
	}

	value := casted.Value
	casted.Value = nil
	result.Release()
	return value, nil
}

func RegisterVectorCumulative(reg FunctionRegistry) {
	sum, checked := kernels.GetVectorCumulativeKernels(safeCastScalar)

	sumFn := NewVectorFunction("cumulative_sum", Unary(), cumulativeSumDoc)
	sumFn.SetDefaultOptions(&CumulativeOptions{})
	for _, k := range sum {
		if err := sumFn.AddKernel(k); err != nil {
			panic(err)
		}
	}
	reg.AddFunction(sumFn, false)

	checkedFn := NewVectorFunction("cumulative_sum_checked", Unary(), cumulativeSumCheckedDoc)
	checkedFn.SetDefaultOptions(&CumulativeOptions{})
	for _, k := range checked {
		if err := checkedFn.AddKernel(k); err != nil {
			panic(err)
		}
	}
	reg.AddFunction(checkedFn, false)
}

func CumulativeSum(ctx context.Context, opts CumulativeOptions, values Datum) (Datum, error) {
	return CallFunction(ctx, "cumulative_sum", &opts, values)
}

func CumulativeSumChecked(ctx context.Context, opts CumulativeOptions, values Datum) (Datum, error) {
	return CallFunction(ctx, "cumulative_sum_checked", &opts, values)
}
