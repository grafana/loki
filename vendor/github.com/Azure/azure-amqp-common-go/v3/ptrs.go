package common

//	MIT License
//
//	Copyright (c) Microsoft Corporation. All rights reserved.
//
//	Permission is hereby granted, free of charge, to any person obtaining a copy
//	of this software and associated documentation files (the "Software"), to deal
//	in the Software without restriction, including without limitation the rights
//	to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
//	copies of the Software, and to permit persons to whom the Software is
//	furnished to do so, subject to the following conditions:
//
//	The above copyright notice and this permission notice shall be included in all
//	copies or substantial portions of the Software.
//
//	THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
//	IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
//	FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
//	AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
//	LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
//	OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
//	SOFTWARE

// PtrBool takes a boolean and returns a pointer to that bool. For use in literal pointers, ptrBool(true) -> *bool
func PtrBool(toPtr bool) *bool {
	return &toPtr
}

// PtrString takes a string and returns a pointer to that string. For use in literal pointers,
// PtrString(fmt.Sprintf("..", foo)) -> *string
func PtrString(toPtr string) *string {
	return &toPtr
}

// PtrInt32 takes a int32 and returns a pointer to that int32. For use in literal pointers, ptrInt32(1) -> *int32
func PtrInt32(number int32) *int32 {
	return &number
}

// PtrInt64 takes a int64 and returns a pointer to that int64. For use in literal pointers, ptrInt64(1) -> *int64
func PtrInt64(number int64) *int64 {
	return &number
}
