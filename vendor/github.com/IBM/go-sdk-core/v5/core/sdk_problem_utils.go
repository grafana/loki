package core

// (C) Copyright IBM Corp. 2024.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

import (
	"runtime"
	"strings"
)

// computeFunctionName investigates the program counter at a fixed
// skip number (aka point in the stack) of 2, which gives us the
// information about the function the problem was created in, and
// returns the name of the function.
func computeFunctionName(componentName string) string {
	if pc, _, _, ok := runtime.Caller(2); ok {
		// The function names will have the component name as a prefix.
		// To avoid redundancy, since we are including the component name
		// with the problem, trim that prefix here.
		return strings.TrimPrefix(runtime.FuncForPC(pc).Name(), componentName+"/")
	}

	return ""
}

// sdkStackFrame is a convenience struct for formatting
// frame data to be printed as YAML.
type sdkStackFrame struct {
	Function string
	File     string
	Line     int
}

// getStackInfo invokes helper methods to curate a limited, formatted
// version of the stack trace with only the component-scoped function
// invocations that lead to the creation of the problem.
func getStackInfo(componentName string) []sdkStackFrame {
	if frames, ok := makeFrames(); ok {
		return formatFrames(frames, componentName)
	}

	return nil
}

// makeFrames populates a program counter list with data at a
// fixed skip number (4), which gives us the stack information
// at the point in the program that the problem was created. This
// function adjusts the list as needed, since the necessary
// list size is not known at first.
func makeFrames() ([]uintptr, bool) {
	pcs := make([]uintptr, 10)
	for {
		n := runtime.Callers(4, pcs)
		if n == 0 {
			return pcs, false
		}
		if n < len(pcs) {
			return pcs[:n], true
		}
		pcs = make([]uintptr, 2*len(pcs))
	}
}

// formatFrames takes a program counter list and formats them
// into a readable format for including in debug messages.
func formatFrames(pcs []uintptr, componentName string) []sdkStackFrame {
	result := make([]sdkStackFrame, 0)

	if len(pcs) == 0 {
		return result
	}

	// Loop to get frames.
	// A fixed number of PCs can expand to an indefinite number of Frames.
	frames := runtime.CallersFrames(pcs)
	for {
		frame, more := frames.Next()

		// Only the frames in the same component as the problem are relevant.
		if strings.HasPrefix(frame.Function, componentName) {
			stackFrame := sdkStackFrame{
				Function: frame.Function,
				File:     frame.File,
				Line:     frame.Line,
			}

			result = append(result, stackFrame)
		}

		// Check whether there are more frames to process after this one.
		if !more {
			break
		}
	}

	return result
}

type sparseSDKProblem struct {
	ID       string
	Function string
}

func newSparseSDKProblem(prob *SDKProblem) *sparseSDKProblem {
	return &sparseSDKProblem{
		ID:       prob.GetID(),
		Function: prob.Function,
	}
}

func isCoreProblem(prob *SDKProblem) bool {
	return prob.Component != nil && prob.Component.Name == MODULE_NAME
}
