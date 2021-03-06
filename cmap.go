// Package cmap implements a yet another concurrent map.
package cmap

import (
	"sync"
	"sync/atomic"

	"github.com/OneOfOne/cmap/hashers"
	"github.com/decillion/go-cmap/hmap"
)

const (
	iniCapacity   = 1 << 4
	minLoadFactor = 2
	midLoadFactor = 4
	maxLoadFactor = 6
	maxBucketSize = 18
	minMapSize    = iniCapacity * midLoadFactor
)

// Map is a concurrent map.
type Map struct {
	mu       sync.Mutex
	hm       atomic.Value // *hmap.Map
	inResize int32
	hasher   func(key interface{}) uint32
}

// DefaultHasher is a hash function for a value of an arbitrary type. It is not
// encouraged to use this function to values of composit types, because it is
// slow on such values.
func DefaultHasher(key interface{}) uint32 {
	return hashers.TypeHasher32(key)
}

// NewMap returns an empty hash map whose keys are hashed by the given function.
func NewMap(hasher func(key interface{}) uint32) (m *Map) {
	m = &Map{hasher: hasher}
	m.hm.Store(hmap.NewMap(iniCapacity, hasher))
	return
}

// Load returns the value associated with the given key and true if the key
// exists. Otherwise, it returns nil and false.
func (m *Map) Load(key interface{}) (value interface{}, ok bool) {
	hm := m.hm.Load().(*hmap.Map)
	value, ok = hm.Load(key)
	return
}

// Store sets the given value to the given key.
func (m *Map) Store(key, value interface{}) {
	m.mu.Lock()
	hm := m.hm.Load().(*hmap.Map)
	hm.Store(key, value)
	m.resizeIfNeeded()
	m.mu.Unlock()
}

// Delete logically removes the given key and its associated value.
func (m *Map) Delete(key interface{}) {
	m.mu.Lock()
	hm := m.hm.Load().(*hmap.Map)
	hm.Delete(key)
	m.resizeIfNeeded()
	m.mu.Unlock()
}

// Range iteratively applies the given function to each key-value pair until
// the function returns false.
func (m *Map) Range(f func(key, value interface{}) bool) {
	m.mu.Lock() // To ensure that no other process concurrently resizes the map.
	atomic.AddInt32(&m.inResize, 1)
	m.mu.Unlock()

	hm := m.hm.Load().(*hmap.Map)
	hm.Range(f)

	atomic.AddInt32(&m.inResize, -1)
}

// This method can only be issued inside the critical section.
func (m *Map) resizeIfNeeded() {
	inResize := atomic.LoadInt32(&m.inResize)
	if inResize != 0 {
		return
	}

	h := m.hm.Load().(*hmap.Map)
	entries, deleted := h.StatEntries()
	buckets, largest := h.StatBuckets()
	if entries < minMapSize {
		return
	}
	LoadFactor := float32(entries) / float32(buckets)
	tooSmallBuckets := LoadFactor > maxLoadFactor
	tooManyDeleted := entries < 5*deleted
	bucketOverflow := largest > maxBucketSize

	var newCapacity uint
	if tooSmallBuckets || bucketOverflow {
		newCapacity = 2*buckets - 1
	} else if tooManyDeleted {
		newCapacity = (entries - deleted) / minLoadFactor
	} else {
		return
	}
	newMap := hmap.NewMap(newCapacity, m.hasher)
	oldMap := m.hm.Load().(*hmap.Map)
	oldMap.Range(func(k, v interface{}) bool {
		newMap.Store(k, v)
		return true
	})
	m.hm.Store(newMap)
}
