package queue

import (
	"sync"
	"time"

	db "github.com/akillmer/riptide/database"
	"github.com/boltdb/bolt"
)

// The Queue package is essentially a stack that is backed by the database.
// Hashes that haven been provided by Add() are stored in activeHashes,
// Done(hash) removes them and allows the queue to continue.

var (
	activeHashes  = sync.Map{}
	cForce, cNext chan string
	cDone         chan struct{}
)

func init() {
	cDone = make(chan struct{})
	cNext = make(chan string)
	cForce = make(chan string)
}

// Add a torrent by its hash to the queue
func Add(hash string) error {
	return db.Put(db.BucketQueued, db.AutoIncrement, hash)
}

// ForceNext a hash to the front of the queue. Since this immediately means
// the torrent becomes active it is not stored within the database.
func ForceNext(hash string) {
	// dont block the caller
	go func() {
		cForce <- hash
	}()
}

// Next returns the next queued hash (blocking)
func Next() string {
	hash := <-cNext
	activeHashes.Store(hash, nil)
	return hash
}

// Done indicates that the hash is complete, and Queue can start the next hash.
// If and only Queue is currently holding the passed hash.
func Done(hash string) {
	if _, ok := activeHashes.Load(hash); ok {
		cDone <- struct{}{}
		activeHashes.Delete(hash)
	}
}

// Run polls the database, the forced hash or oldest hash is the first to go.
func Run(maxActive int) {
	ticker := time.NewTicker(time.Second / 2)
	numActive := 0

	for {
		select {
		case <-cDone:
			numActive--
			if numActive < 0 {
				numActive = 0
			}
		case hash := <-cForce:
			numActive++
			Remove(hash)
			cNext <- hash
		case <-ticker.C:
			break
		}

		if numActive < maxActive {
			// going to ignore the error here, since we may not always get a value
			buf, _ := db.Get(db.BucketQueued, db.GetFirstKey)
			if buf != nil {
				numActive++
				cNext <- string(buf)
				db.Delete(db.BucketQueued, db.GetFirstKey)
			}
		}
	}
}

// Remove a hash from the queue
func Remove(hash string) error {
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(db.BucketQueued)
		b.ForEach(func(k, v []byte) error {
			if string(v) == hash {
				b.Delete(k)
			}
			return nil
		})
		return nil
	})
}
