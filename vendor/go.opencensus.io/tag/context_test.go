// Copyright 2018, OpenCensus Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package tag

import (
	"context"
	"testing"
)

func TestExtractTagsAttachment(t *testing.T) {
	// We can't depend on the stats of view package without creating a
	// dependency cycle.

	var m map[string]string
	ctx := context.Background()

	res := extractTagsAttachments(ctx, m)
	if res != nil {
		t.Fatalf("res = %v; want nil", res)
	}

	k, _ := NewKey("test")
	ctx, _ = New(ctx, Insert(k, "test123"))
	res = extractTagsAttachments(ctx, m)
	if res == nil {
		t.Fatal("res = nil")
	}
	if got, want := res["tag:test"], "test123"; got != want {
		t.Fatalf("res[Tags:test] = %v; want %v", got, want)
	}
}
