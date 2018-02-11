// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

package rand

import "testing"

func TestSeedFromBytes(t *testing.T) {
	// should always return the same seed for the same bytes
	tcs := []struct {
		bs []byte
		v  int64
	}{
		{[]byte("hello world"), -3639725434188061933},
		{[]byte("hello worlx"), -2539100776074091088},
	}

	for _, tc := range tcs {
		if v := SeedFromBytes(tc.bs); v != tc.v {
			t.Errorf("Unexpected seed value %d != %d", v, tc.v)
		}
	}
}

func TestRandomString(t *testing.T) {
	for _, l := range []int{0, 1, 2, 3, 4, 8, 42} {
		s := String(l)
		if len(s) != l {
			t.Errorf("Incorrect length %d != %d", len(s), l)
		}
	}

	strings := make([]string, 1000)
	for i := range strings {
		strings[i] = String(8)
		for j := range strings {
			if i == j {
				continue
			}
			if strings[i] == strings[j] {
				t.Errorf("Repeated random string %q", strings[i])
			}
		}
	}
}

func TestRandomInt64(t *testing.T) {
	ints := make([]int64, 1000)
	for i := range ints {
		ints[i] = Int64()
		for j := range ints {
			if i == j {
				continue
			}
			if ints[i] == ints[j] {
				t.Errorf("Repeated random int64 %d", ints[i])
			}
		}
	}
}
