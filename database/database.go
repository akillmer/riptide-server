package database

import (
	"encoding/binary"
	"encoding/json"
	"errors"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/boltdb/bolt"
)

var (
	db *bolt.DB
	// BucketQueued key, holds Torrent hash keys by auto ID that are queued for activity
	BucketQueued = []byte("Queued")
	// BucketTorrents key, holds Torrents by hash key, contains static info and magnet URLs
	BucketTorrents = []byte("Torrents")
	// BucketLabels key, holds user created Labels by unique short id
	BucketLabels = []byte("Labels")
	// ErrKeyNotValid if it's not metainfo.Hash, byte slice, string, struct pointer, GetFirstKey or GetLastKey
	ErrKeyNotValid = errors.New("key does not satisfy interface requirements")
	// ErrValueNotValid if it's not metainfo.Hash, byte slice, string, struct pointer, or AutoIncrement
	ErrValueNotValid = errors.New("value does not satisfy interface requirements")
	// ErrNoSuchKey if an object doesn't exist with the specified key
	ErrNoSuchKey = errors.New("no such key exists")
)

const (
	// AutoIncrement sets the object's key as an auto incrementing ID
	AutoIncrement = iota + 1
	// GetFirstKey gets the first object in a bucket
	GetFirstKey
	// GetLastKey gets the last object in a bucket
	GetLastKey
)

// Open and initialize the database
func Open(dbFile string) error {
	boltdb, err := bolt.Open(dbFile, 0644, nil)
	if err != nil {
		return err
	}
	db = boltdb

	err = db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(BucketQueued); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(BucketTorrents); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(BucketLabels); err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		return err
	}

	return nil
}

// Close the database
func Close() {
	db.Close()
}

// determine the type of interface that v is; can be metainfo.Hash, byte slice,
// string, or a JSON friendly struct)
func assertInterface(v interface{}) []byte {
	if hash, ok := v.(metainfo.Hash); ok {
		return hash.Bytes()
	} else if b, ok := v.([]byte); ok {
		return b
	} else if str, ok := v.(string); ok {
		return []byte(str)
	} else if buf, err := json.Marshal(v); err == nil {
		return buf
	}
	return nil
}

// assert the key type and return the key/value with the provided bucket. key can be
// GetFirstKey, GetLastKey, or valid through assertInterface(...)
func assertGetByKey(key interface{}, b *bolt.Bucket) ([]byte, []byte, error) {
	var k, v []byte

	if get, ok := key.(int); ok {
		if get == GetFirstKey {
			k, v = b.Cursor().First()
		} else if get == GetLastKey {
			k, v = b.Cursor().Last()
		}
	} else if asserted := assertInterface(key); key != nil {
		k = asserted
		v = b.Get(k)
	} else {
		return nil, nil, ErrKeyNotValid
	}

	if v == nil {
		return nil, nil, ErrNoSuchKey
	}

	return k, v, nil
}

func itob(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

// Put stores the value by its provided key and bucket. Key must be a type of
// metainfo.Hash, byte slice, string, or AutoIncrement. Val must be a type of
// metainfo.Hash, byte slice, string, or a struct that can be marshaled into
// JSON format.
func Put(bucket []byte, key, val interface{}) error {
	buf := assertInterface(val)
	if buf == nil {
		return ErrValueNotValid
	}

	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		var k []byte

		if v, ok := key.(int); ok && v == AutoIncrement {
			id, _ := b.NextSequence()
			k = itob(id)
		} else if k = assertInterface(key); k == nil {
			return ErrKeyNotValid
		}

		return b.Put(k, buf)
	})
}

// Get retrieves a value by the provided key interface. Key must be a type
// of metainfo.Hash, byte slice, string, GetFirstKey, or GetLastKey.
func Get(bucket []byte, key interface{}) ([]byte, error) {
	var buf []byte

	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)

		_, v, err := assertGetByKey(key, b)
		if err != nil {
			return err
		}

		buf = make([]byte, len(v))
		copy(buf, v)

		return nil
	})

	if err != nil {
		return nil, err
	}

	return buf, nil
}

// All returns all stored objects as a slice within the provided bucket. If there
// are no objects then nil is returned.
func All(bucket []byte) [][]byte {
	all := [][]byte{}

	db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		return b.ForEach(func(k, v []byte) error {
			buf := make([]byte, len(v))
			copy(buf, v)
			all = append(all, buf)
			return nil
		})
	})

	if len(all) == 0 {
		return nil
	}

	return all
}

// Delete returns the object held by key within the provided bucket. Key must be a type
// of metainfo.Hash, byte slice, string, GetFirstKey, or GetLastKey.
func Delete(bucket []byte, key interface{}) error {
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		k, _, err := assertGetByKey(key, b)
		if err != nil {
			return err
		}
		return b.Delete(k)
	})
}

// View provides a wrapper for the underlying Bolt DB
func View(fn func(tx *bolt.Tx) error) error {
	return db.View(fn)
}

// Update provides a wrapper for the underlying Bolt DB
func Update(fn func(tx *bolt.Tx) error) error {
	return db.Update(fn)
}
