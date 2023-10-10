package main

import (
	"time"
)

// Pretty much copied verbatim from
// https://golang.org/pkg/container/heap/#example__priorityQueue
// Just a few name changes

// An Item is something we manage in a priority queue.
type Item struct {
	value    statTimeSet // The value of the item; arbitrary.
	priority time.Time   // The priority of the item in the queue.
	// The index is needed by update and is maintained by the heap.Interface methods.
	index int // The index of the item in the heap.
}

// A PriorityQueue implements heap.Interface and holds Items.
type PriorityQueue []*Item

func (pq PriorityQueue) Len() int { return len(pq) }

func (pq PriorityQueue) Less(i, j int) bool {
	// We want the earliest (smallest) time so use Before
	return pq[i].priority.Before(pq[j].priority)
}

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

/*
// update modifies the priority and value of an Item in the queue.
func (pq *PriorityQueue) update(item *Item, value statTimeSet, priority time.Time) {
	item.value = value
	item.priority = priority
	heap.Fix(pq, item.index)
}
*/
