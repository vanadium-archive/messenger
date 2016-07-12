// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal

import (
	"sync"

	"v.io/v23/context"

	"messenger/ifc"
)

// newPubSub returns a new PubSub object that exists until the given context
// is canceled.
func newPubSub(ctx *context.T) *PubSub {
	ps := &PubSub{
		ctx:  ctx,
		in:   make(chan ifc.Message),
		out:  make(map[chan *ifc.Message]struct{}),
		done: make(chan struct{}),
	}
	go ps.readLoop()
	return ps
}

type PubSub struct {
	ctx  *context.T
	in   chan ifc.Message
	mu   sync.Mutex
	out  map[chan *ifc.Message]struct{}
	done chan struct{}
}

// Pub sends the given message id to all the subscribers.
func (ps *PubSub) Pub(msg ifc.Message) {
	select {
	case <-ps.ctx.Done():
	case ps.in <- msg:
	}
}

// Sub registers a new subscriber that will receive all the new message ids
// on the returned channel.
func (ps *PubSub) Sub() chan *ifc.Message {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ch := make(chan *ifc.Message)
	ps.out[ch] = struct{}{}
	return ch
}

// Unsub unregisters the subscriber with the given channel.
func (ps *PubSub) Unsub(ch chan *ifc.Message) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if _, exists := ps.out[ch]; exists {
		delete(ps.out, ch)
		close(ch)
		for range ch {
		}
	}
}

func (ps *PubSub) readLoop() {
L:
	for {
		select {
		case <-ps.ctx.Done():
			break L
		case msg, ok := <-ps.in:
			if !ok {
				break L
			}
			ps.mu.Lock()
			var pos uint
			for ch := range ps.out {
				select {
				case <-ps.ctx.Done():
				case ch <- &msg:
					pos++
				}
			}
			ps.mu.Unlock()
		}
	}
	ps.mu.Lock()
	for ch := range ps.out {
		delete(ps.out, ch)
		close(ch)
	}
	close(ps.done)
	ps.mu.Unlock()
}
