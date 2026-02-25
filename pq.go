package main

import (
	"time"
)

// Priority Queue implementation for statTimeSet
// Pretty much copied verbatim from
// https://golang.org/pkg/container/heap/#example__priorityQueue
// Just a few name changes

// StatType identifies the kind of stat stored in a priority queue item.
type StatType int

// Stat type constants for use with the priority queue.
const (
	StatTypeRegularStat StatType = iota
	StatTypeSummaryStatProtocol
	StatTypeSummaryStatClient
)

// PqValue is the value stored in the priority queue
// it must be able to hold either regular stat info or summary stat info
// so we use a StatType to indicate which it is
type PqValue struct {
	stattype StatType
	sts      *statTimeSet
}

// An Item is something we manage in a priority queue.
type Item struct {
	value    PqValue   // The value of the item; arbitrary.
	priority time.Time // The priority of the item in the queue.
	// The index is needed by update and is maintained by the heap.Interface methods.
	index int // The index of the item in the heap.
}

// A PriorityQueue implements heap.Interface and holds Items.
type PriorityQueue []*Item

// Len is the number of elements in the collection.
func (pq PriorityQueue) Len() int { return len(pq) }

// Less reports whether the element with index i should sort before the element with index j.
func (pq PriorityQueue) Less(i, j int) bool {
	// We want the earliest (smallest) time so use Before
	return pq[i].priority.Before(pq[j].priority)
}

// Swap swaps the elements with indexes i and j.
func (pq PriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

// Push adds an element to the end of the priority queue
func (pq *PriorityQueue) Push(x any) {
	n := len(*pq)
	item := x.(*Item)
	item.index = n
	*pq = append(*pq, item)
}

// Pop removes the first element from priority queue
func (pq *PriorityQueue) Pop() any {
	old := *pq
	n := len(old)
	item := old[n-1]
	item.index = -1 // for safety
	*pq = old[0 : n-1]
	return item
}

