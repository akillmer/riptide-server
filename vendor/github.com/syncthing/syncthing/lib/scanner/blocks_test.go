// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

package scanner

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	origAdler32 "hash/adler32"
	"testing"
	"testing/quick"

	rollingAdler32 "github.com/chmduquesne/rollinghash/adler32"
	"github.com/syncthing/syncthing/lib/protocol"
)

var blocksTestData = []struct {
	data      []byte
	blocksize int
	hash      []string
	weakhash  []uint32
}{
	{[]byte(""), 1024, []string{
		"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		[]uint32{0},
	},
	{[]byte("contents"), 1024, []string{
		"d1b2a59fbea7e20077af9f91b27e95e865061b270be03ff539ab3b73587882e8"},
		[]uint32{0x0f3a036f},
	},
	{[]byte("contents"), 9, []string{
		"d1b2a59fbea7e20077af9f91b27e95e865061b270be03ff539ab3b73587882e8"},
		[]uint32{0x0f3a036f},
	},
	{[]byte("contents"), 8, []string{
		"d1b2a59fbea7e20077af9f91b27e95e865061b270be03ff539ab3b73587882e8"},
		[]uint32{0x0f3a036f},
	},
	{[]byte("contents"), 7, []string{
		"ed7002b439e9ac845f22357d822bac1444730fbdb6016d3ec9432297b9ec9f73",
		"043a718774c572bd8a25adbeb1bfcd5c0256ae11cecf9f9c3f925d0e52beaf89"},
		[]uint32{0x0bcb02fc, 0x00740074},
	},
	{[]byte("contents"), 3, []string{
		"1143da2bc54c495c4be31d3868785d39ffdfd56df5668f0645d8f14d47647952",
		"e4432baa90819aaef51d2a7f8e148bf7e679610f3173752fabb4dcb2d0f418d3",
		"44ad63f60af0f6db6fdde6d5186ef78176367df261fa06be3079b6c80c8adba4"},
		[]uint32{0x02780141, 0x02970148, 0x015d00e8},
	},
	{[]byte("conconts"), 3, []string{
		"1143da2bc54c495c4be31d3868785d39ffdfd56df5668f0645d8f14d47647952",
		"1143da2bc54c495c4be31d3868785d39ffdfd56df5668f0645d8f14d47647952",
		"44ad63f60af0f6db6fdde6d5186ef78176367df261fa06be3079b6c80c8adba4"},
		[]uint32{0x02780141, 0x02780141, 0x015d00e8},
	},
	{[]byte("contenten"), 3, []string{
		"1143da2bc54c495c4be31d3868785d39ffdfd56df5668f0645d8f14d47647952",
		"e4432baa90819aaef51d2a7f8e148bf7e679610f3173752fabb4dcb2d0f418d3",
		"e4432baa90819aaef51d2a7f8e148bf7e679610f3173752fabb4dcb2d0f418d3"},
		[]uint32{0x02780141, 0x02970148, 0x02970148},
	},
}

func TestBlocks(t *testing.T) {
	for testNo, test := range blocksTestData {
		buf := bytes.NewBuffer(test.data)
		blocks, err := Blocks(context.TODO(), buf, test.blocksize, -1, nil, true)

		if err != nil {
			t.Fatal(err)
		}

		if l := len(blocks); l != len(test.hash) {
			t.Fatalf("%d: Incorrect number of blocks %d != %d", testNo, l, len(test.hash))
		} else {
			i := 0
			for off := int64(0); off < int64(len(test.data)); off += int64(test.blocksize) {
				if blocks[i].Offset != off {
					t.Errorf("%d/%d: Incorrect offset %d != %d", testNo, i, blocks[i].Offset, off)
				}

				bs := test.blocksize
				if rem := len(test.data) - int(off); bs > rem {
					bs = rem
				}
				if int(blocks[i].Size) != bs {
					t.Errorf("%d/%d: Incorrect length %d != %d", testNo, i, blocks[i].Size, bs)
				}
				if h := fmt.Sprintf("%x", blocks[i].Hash); h != test.hash[i] {
					t.Errorf("%d/%d: Incorrect block hash %q != %q", testNo, i, h, test.hash[i])
				}
				if h := blocks[i].WeakHash; h != test.weakhash[i] {
					t.Errorf("%d/%d: Incorrect block weakhash 0x%08x != 0x%08x", testNo, i, h, test.weakhash[i])
				}

				i++
			}
		}
	}
}

var diffTestData = []struct {
	a string
	b string
	s int
	d []protocol.BlockInfo
}{
	{"contents", "contents", 1024, []protocol.BlockInfo{}},
	{"", "", 1024, []protocol.BlockInfo{}},
	{"contents", "contents", 3, []protocol.BlockInfo{}},
	{"contents", "cantents", 3, []protocol.BlockInfo{{Offset: 0, Size: 3}}},
	{"contents", "contants", 3, []protocol.BlockInfo{{Offset: 3, Size: 3}}},
	{"contents", "cantants", 3, []protocol.BlockInfo{{Offset: 0, Size: 3}, {Offset: 3, Size: 3}}},
	{"contents", "", 3, []protocol.BlockInfo{{Offset: 0, Size: 0}}},
	{"", "contents", 3, []protocol.BlockInfo{{Offset: 0, Size: 3}, {Offset: 3, Size: 3}, {Offset: 6, Size: 2}}},
	{"con", "contents", 3, []protocol.BlockInfo{{Offset: 3, Size: 3}, {Offset: 6, Size: 2}}},
	{"contents", "con", 3, nil},
	{"contents", "cont", 3, []protocol.BlockInfo{{Offset: 3, Size: 1}}},
	{"cont", "contents", 3, []protocol.BlockInfo{{Offset: 3, Size: 3}, {Offset: 6, Size: 2}}},
}

func TestDiff(t *testing.T) {
	for i, test := range diffTestData {
		a, _ := Blocks(context.TODO(), bytes.NewBufferString(test.a), test.s, -1, nil, false)
		b, _ := Blocks(context.TODO(), bytes.NewBufferString(test.b), test.s, -1, nil, false)
		_, d := BlockDiff(a, b)
		if len(d) != len(test.d) {
			t.Fatalf("Incorrect length for diff %d; %d != %d", i, len(d), len(test.d))
		} else {
			for j := range test.d {
				if d[j].Offset != test.d[j].Offset {
					t.Errorf("Incorrect offset for diff %d block %d; %d != %d", i, j, d[j].Offset, test.d[j].Offset)
				}
				if d[j].Size != test.d[j].Size {
					t.Errorf("Incorrect length for diff %d block %d; %d != %d", i, j, d[j].Size, test.d[j].Size)
				}
			}
		}
	}
}

func TestDiffEmpty(t *testing.T) {
	emptyCases := []struct {
		a    []protocol.BlockInfo
		b    []protocol.BlockInfo
		need int
		have int
	}{
		{nil, nil, 0, 0},
		{[]protocol.BlockInfo{{Offset: 3, Size: 1}}, nil, 0, 0},
		{nil, []protocol.BlockInfo{{Offset: 3, Size: 1}}, 1, 0},
	}
	for _, emptyCase := range emptyCases {
		h, n := BlockDiff(emptyCase.a, emptyCase.b)
		if len(h) != emptyCase.have {
			t.Errorf("incorrect have: %d != %d", len(h), emptyCase.have)
		}
		if len(n) != emptyCase.need {
			t.Errorf("incorrect have: %d != %d", len(h), emptyCase.have)
		}
	}
}

func TestAdler32Variants(t *testing.T) {
	// Verify that the two adler32 functions give matching results for a few
	// different blocks of data.

	hf1 := origAdler32.New()
	hf2 := rollingAdler32.New()

	checkFn := func(data []byte) bool {
		hf1.Write(data)
		sum1 := hf1.Sum32()

		hf2.Write(data)
		sum2 := hf2.Sum32()

		hf1.Reset()
		hf2.Reset()

		return sum1 == sum2
	}

	// protocol block sized data
	data := make([]byte, protocol.BlockSize)
	for i := 0; i < 5; i++ {
		rand.Read(data)
		if !checkFn(data) {
			t.Errorf("Hash mismatch on block sized data")
		}
	}

	// random small blocks
	if err := quick.Check(checkFn, nil); err != nil {
		t.Error(err)
	}

	// rolling should have the same result as the individual blocks
	// themselves. Which is not the same as the original non-rollind adler32
	// blocks.

	windowSize := 128

	hf2.Reset()

	hf3 := rollingAdler32.New()
	hf3.Write(data[:windowSize])

	for i := windowSize; i < len(data); i++ {
		if i%windowSize == 0 {
			// let the reference function catch up
			hf2.Write(data[i-windowSize : i])

			// verify that they are in sync with the rolling function
			sum2 := hf2.Sum32()
			sum3 := hf3.Sum32()
			t.Logf("At i=%d, sum2=%08x, sum3=%08x", i, sum2, sum3)
			if sum2 != sum3 {
				t.Errorf("Mismatch after roll; i=%d, sum2=%08x, sum3=%08x", i, sum2, sum3)
				break
			}
		}
		hf3.Roll(data[i])
	}
}
