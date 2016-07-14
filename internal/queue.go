// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal

import (
	"container/list"
	"sync"
	"time"

	"v.io/v23/context"
)

// newMsgQueue returns a new message ID queue. The message IDs can be scheduled
// for arbitrary times with Insert(). They are then read back at the correct
// time from the channel returned by Pop().
func newMsgQueue(ctx *context.T) *MsgQueue {
	q := &MsgQueue{
		ctx:         ctx,
		l:           list.New(),
		recheckChan: make(chan struct{}, 1),
	}
	return q
}

type MsgQueue struct {
	ctx         *context.T
	mu          sync.Mutex
	l           *list.List
	recheckChan chan struct{}
	popChan     chan string
}

type msgQueueElement struct {
	t     time.Time
	msgId string
}

func (q *MsgQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.l.Len()
}

// Empty returns whether this queue is empty.
func (q *MsgQueue) Empty() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.l.Front() == nil
}

// Pop returns a channel from which the message IDs can be read when their
// scheduled time has come.
func (q *MsgQueue) Pop() <-chan string {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.popChan == nil {
		q.popChan = make(chan string)
		go func() {
			for {
				delay := time.Minute
				q.mu.Lock()
				if e := q.l.Front(); e != nil {
					delay = e.Value.(*msgQueueElement).t.Sub(time.Now())
				}
				q.mu.Unlock()

				select {
				case <-q.ctx.Done():
					return
				case <-time.After(delay):
				case <-q.recheckChan:
				}

				removed := false
				q.mu.Lock()
				e := q.l.Front()
				if e != nil && time.Now().After(e.Value.(*msgQueueElement).t) {
					q.l.Remove(e)
					removed = true
				}
				q.mu.Unlock()
				if removed {
					select {
					case <-q.ctx.Done():
					case q.popChan <- e.Value.(*msgQueueElement).msgId:
					}
				}
			}
		}()
	}
	return q.popChan
}

// Insert inserts a message ID at an arbitrary time. The message ID will be
// sent to the pop channel at that time.
func (q *MsgQueue) Insert(t time.Time, msgId string) {
	defer func() {
		select {
		case q.recheckChan <- struct{}{}:
		default:
		}
	}()
	q.mu.Lock()
	defer q.mu.Unlock()
	qe := &msgQueueElement{t, msgId}
	for e := q.l.Front(); e != nil; e = e.Next() {
		if e.Value.(*msgQueueElement).t.After(t) {
			q.l.InsertBefore(qe, e)
			return
		}
	}
	q.l.PushBack(qe)
}
