package muon

import "container/list"

/*
Methods:

extend(array)
- appends an array to the end of the lru

contains() bool
- reports if a value is in the lru

index(val)
- returns the index of a value in the lru

len(lru)
- should have a length

append(val)
- appends one value to end of lru

remove(val)
- remove a value from lru

lru[n]
- access the element at n

*/

type LRU struct {
	cap   int
	list  *list.List
	cache map[string]list.Element
}

func NewLRU(capacity int) *LRU {
	l := list.New()
	m := make(map[string]list.Element)
	return &LRU{capacity, l, m}
}

func (lru *LRU) Put(key string, val list.Element) {
}

func (lru *LRU) Get(key string) list.Element {
	return *lru.list.Front()
}

func (lru *LRU) FindIndex(element any) int {
	index := 0
	for e := lru.list.Front(); e != nil; e = e.Next() {
		if e.Value == element {
			return index
		}
		index++
	}
	return -1 // Element not found
}

// func (lru *LRU) extend(arr []any) {

// }
