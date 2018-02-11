// Copyright (C) 2015 The Protocol Authors.

package protocol

import "testing"

func TestWinsConflict(t *testing.T) {
	testcases := [][2]FileInfo{
		// The first should always win over the second
		{{ModifiedS: 42}, {ModifiedS: 41}},
		{{ModifiedS: 41}, {ModifiedS: 42, Deleted: true}},
		{{ModifiedS: 41, Version: Vector{[]Counter{{42, 2}, {43, 1}}}}, {ModifiedS: 41, Version: Vector{[]Counter{{42, 1}, {43, 2}}}}},
	}

	for _, tc := range testcases {
		if !tc[0].WinsConflict(tc[1]) {
			t.Errorf("%v should win over %v", tc[0], tc[1])
		}
		if tc[1].WinsConflict(tc[0]) {
			t.Errorf("%v should not win over %v", tc[1], tc[0])
		}
	}
}
