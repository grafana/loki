// Copyright 2017, OpenCensus Authors
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
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestContext(t *testing.T) {
	k1, _ := NewKey("k1")
	k2, _ := NewKey("k2")

	ctx := context.Background()
	ctx, _ = New(ctx,
		Insert(k1, "v1"),
		Insert(k2, "v2"),
	)
	got := FromContext(ctx)
	want := newMap()
	want.insert(k1, "v1")
	want.insert(k2, "v2")

	if !reflect.DeepEqual(got, want) {
		t.Errorf("Map = %#v; want %#v", got, want)
	}
}

func TestDo(t *testing.T) {
	k1, _ := NewKey("k1")
	k2, _ := NewKey("k2")
	ctx := context.Background()
	ctx, _ = New(ctx,
		Insert(k1, "v1"),
		Insert(k2, "v2"),
	)
	got := FromContext(ctx)
	want := newMap()
	want.insert(k1, "v1")
	want.insert(k2, "v2")
	Do(ctx, func(ctx context.Context) {
		got = FromContext(ctx)
	})
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Map = %#v; want %#v", got, want)
	}
}

func TestNewMap(t *testing.T) {
	k1, _ := NewKey("k1")
	k2, _ := NewKey("k2")
	k3, _ := NewKey("k3")
	k4, _ := NewKey("k4")
	k5, _ := NewKey("k5")

	initial := makeTestTagMap(5)

	tests := []struct {
		name    string
		initial *Map
		mods    []Mutator
		want    *Map
	}{
		{
			name:    "from empty; insert",
			initial: nil,
			mods: []Mutator{
				Insert(k5, "v5"),
			},
			want: makeTestTagMap(2, 4, 5),
		},
		{
			name:    "from empty; insert existing",
			initial: nil,
			mods: []Mutator{
				Insert(k1, "v1"),
			},
			want: makeTestTagMap(1, 2, 4),
		},
		{
			name:    "from empty; update",
			initial: nil,
			mods: []Mutator{
				Update(k1, "v1"),
			},
			want: makeTestTagMap(2, 4),
		},
		{
			name:    "from empty; update unexisting",
			initial: nil,
			mods: []Mutator{
				Update(k5, "v5"),
			},
			want: makeTestTagMap(2, 4),
		},
		{
			name:    "from existing; upsert",
			initial: initial,
			mods: []Mutator{
				Upsert(k5, "v5"),
			},
			want: makeTestTagMap(2, 4, 5),
		},
		{
			name:    "from existing; delete",
			initial: initial,
			mods: []Mutator{
				Delete(k2),
			},
			want: makeTestTagMap(4, 5),
		},
		{
			name:    "from empty; invalid",
			initial: nil,
			mods: []Mutator{
				Insert(k5, "v\x19"),
				Upsert(k5, "v\x19"),
				Update(k5, "v\x19"),
			},
			want: nil,
		},
		{
			name:    "from empty; no partial",
			initial: nil,
			mods: []Mutator{
				Insert(k5, "v1"),
				Update(k5, "v\x19"),
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		mods := []Mutator{
			Insert(k1, "v1"),
			Insert(k2, "v2"),
			Update(k3, "v3"),
			Upsert(k4, "v4"),
			Insert(k2, "v2"),
			Delete(k1),
		}
		mods = append(mods, tt.mods...)
		ctx := NewContext(context.Background(), tt.initial)
		ctx, err := New(ctx, mods...)
		if tt.want != nil && err != nil {
			t.Errorf("%v: New = %v", tt.name, err)
		}

		if got, want := FromContext(ctx), tt.want; !reflect.DeepEqual(got, want) {
			t.Errorf("%v: got %v; want %v", tt.name, got, want)
		}
	}
}

func TestNewValidation(t *testing.T) {
	tests := []struct {
		err  string
		seed *Map
	}{
		// Key name validation in seed
		{err: "invalid key", seed: &Map{m: map[Key]string{{name: ""}: "foo"}}},
		{err: "", seed: &Map{m: map[Key]string{{name: "key"}: "foo"}}},
		{err: "", seed: &Map{m: map[Key]string{{name: strings.Repeat("a", 255)}: "census"}}},
		{err: "invalid key", seed: &Map{m: map[Key]string{{name: strings.Repeat("a", 256)}: "census"}}},
		{err: "invalid key", seed: &Map{m: map[Key]string{{name: "Приве́т"}: "census"}}},

		// Value validation
		{err: "", seed: &Map{m: map[Key]string{{name: "key"}: ""}}},
		{err: "", seed: &Map{m: map[Key]string{{name: "key"}: strings.Repeat("a", 255)}}},
		{err: "invalid value", seed: &Map{m: map[Key]string{{name: "key"}: "Приве́т"}}},
		{err: "invalid value", seed: &Map{m: map[Key]string{{name: "key"}: strings.Repeat("a", 256)}}},
	}

	for i, tt := range tests {
		ctx := NewContext(context.Background(), tt.seed)
		ctx, err := New(ctx)

		if tt.err != "" {
			if err == nil {
				t.Errorf("#%d: got nil error; want %q", i, tt.err)
				continue
			} else if s, substr := err.Error(), tt.err; !strings.Contains(s, substr) {
				t.Errorf("#%d:\ngot %q\nwant %q", i, s, substr)
			}
			continue
		}
		if err != nil {
			t.Errorf("#%d: got %q want nil", i, err)
			continue
		}
		m := FromContext(ctx)
		if m == nil {
			t.Errorf("#%d: got nil map", i)
			continue
		}
	}
}

func makeTestTagMap(ids ...int) *Map {
	m := newMap()
	for _, v := range ids {
		k, _ := NewKey(fmt.Sprintf("k%d", v))
		m.m[k] = fmt.Sprintf("v%d", v)
	}
	return m
}
