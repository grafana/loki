package main

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_calcSyncRanges(t *testing.T) {
	type args struct {
		from    int64
		to      int64
		shardBy int64
	}
	tests := []struct {
		name string
		args args
		want []*syncRange
	}{
		{
			name: "one range",
			args: args{
				from:    0,
				to:      10,
				shardBy: 10,
			},
			want: []*syncRange{
				{
					from:   0,
					to:     10,
					number: 0,
				},
			},
		},
		{
			name: "two ranges",
			args: args{
				from:    0,
				to:      20,
				shardBy: 10,
			},
			want: []*syncRange{
				{
					from:   0,
					to:     10,
					number: 0,
				},
				{
					from:   11,
					to:     20,
					number: 1,
				},
			},
		},
		{
			name: "three ranges",
			args: args{
				from:    0,
				to:      20,
				shardBy: 6,
			},
			want: []*syncRange{
				{
					from:   0,
					to:     6,
					number: 0,
				},
				{
					from:   7,
					to:     12,
					number: 1,
				},
				{
					from:   13,
					to:     18,
					number: 2,
				},
				{
					from:   19,
					to:     20,
					number: 3,
				},
			},
		},
		{
			name: "four ranges actual data",
			args: args{
				from:    1583798400000000000,
				to:      1583884800000000000,
				shardBy: 21600000000000,
			},
			want: []*syncRange{
				{
					from:   1583798400000000000,
					to:     1583820000000000000,
					number: 0,
				},
				{
					from:   1583820000000000001,
					to:     1583841600000000000,
					number: 1,
				},
				{
					from:   1583841600000000001,
					to:     1583863200000000000,
					number: 2,
				},
				{
					from:   1583863200000000001,
					to:     1583884800000000000,
					number: 3,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := calcSyncRanges(tt.args.from, tt.args.to, tt.args.shardBy); !reflect.DeepEqual(got, tt.want) {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}
