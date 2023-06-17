package muon

import (
	"fmt"

	"github.com/google/go-cmp/cmp"
)

type LRU struct {
	cap   int
	deque []any
}

func NewLRU(capacity int) *LRU {
	return &LRU{capacity, make([]any, 0, capacity)}
}

func (lru *LRU) Append(val any) {
	if len(lru.deque)+1 > lru.cap {
		lru.truncate((1))
	}
	lru.deque = append(lru.deque, val)
}

func (lru *LRU) Extend(list []any) {
	for _, v := range list {
		lru.Append(v)
	}
}

// truncate removes n values from the beginning of the LRU
func (lru *LRU) truncate(n int) {
	if n > len(lru.deque) {
		lru.deque = lru.deque[:0]
		return
	}
	lru.deque = lru.deque[n:]
}

func (lru *LRU) Get(idx int) any {
	if idx <= 0 {
		lastIdx := len(lru.deque) - 1
		return lru.deque[lastIdx+idx]
	}
	return lru.deque[idx]
}

func (lru *LRU) FindIndex(val any) int {
	for i, v := range lru.deque {
		if cmp.Equal(v, val) {
			return i
		}
	}
	return -1
}

func (lru *LRU) Contains(val any) bool {
	for _, v := range lru.deque {
		if cmp.Equal(v, val) {
			return true
		}
	}
	return false
}

func (lru *LRU) Remove(val any) {
	idx := lru.FindIndex(val)
	if idx >= 0 {
		if idx < len(lru.deque)-1 {
			lru.deque = append(lru.deque[:idx], lru.deque[idx+1:])
		} else {
			lru.deque = lru.deque[:idx]
		}
	} else {
		panic(fmt.Errorf("val %v not in LRU", val))
	}
}
