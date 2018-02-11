package database

import (
	"bytes"
	"testing"

	"github.com/anacrolix/torrent/metainfo"
)

func TestAssertHash(t *testing.T) {
	hash := metainfo.NewHashFromHex("e510401e6242521ed180843ef71308927be8165f")
	v := assertInterface(hash)

	if v == nil {
		t.Fatalf("hash should not be asserted as nil")
	}

	if len(v) != metainfo.HashSize {
		t.Fatalf("asserted hash should have length %d, got %d", metainfo.HashSize, len(v))
	}

	if bytes.Equal(hash.Bytes(), v) == false {
		t.Fatalf("original hash and asserted hash should equal")
	}
}
