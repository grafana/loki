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

/*
Package memory provides support for allocating and manipulating memory at a low level.

The build tag 'mallocator' will switch the default allocator to one backed by libc malloc. This also requires CGO.

# Alignment

[GoAllocator] (the [DefaultAllocator] unless the 'mallocator' build tag is set) returns
buffers whose start is rounded up to a multiple of 64 bytes, and the mallocator package's
[github.com/apache/arrow-go/v18/arrow/memory/mallocator.NewMallocator] does the same for
libc memory. Neither promises more than 64. A consumer that needs a larger alignment (a
page-aligned buffer for a GPU runtime, mmap or DMA, for example) should allocate with
[github.com/apache/arrow-go/v18/arrow/memory/mallocator.NewMallocatorWithAlignment],
which takes any power of two.

# Sharing buffers with C

Memory from [GoAllocator] lives on the Go heap. Under the cgo pointer rules
(https://pkg.go.dev/cmd/cgo#hdr-Passing_pointers) C code may use such a pointer only
for the duration of the cgo call it was passed to; it must not keep it, because the
garbage collector may move or free the memory. Buffers that C will hold on to, such as
arrays exported over the C Data Interface with the cdata package and retained by the
consumer, should therefore be allocated with an allocator that returns C memory:
[github.com/apache/arrow-go/v18/arrow/memory/mallocator.Mallocator] (libc malloc, no
C++ dependency, requires cgo) or, when the Arrow C++ library is linked, the
CgoArrowAllocator behind the 'ccalloc' build tag.
*/
package memory
