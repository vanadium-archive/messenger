// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal

import (
	"io"
	"math/rand"
	"sort"
	"sync"
	"time"

	"v.io/v23/context"
	"v.io/v23/discovery"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/verror"

	"messenger/ifc"
)

func startPeerManager(ctx *context.T, id string, updateChan <-chan discovery.Update, ps *PubSub, store MessengerStorage, params Params, counters *Counters) <-chan struct{} {
	pm := &peerManager{
		id:       id,
		store:    store,
		params:   params,
		peers:    make(map[string]*peer),
		ps:       ps,
		counters: counters,
		done:     make(chan struct{}),
	}
	go pm.loop(ctx, updateChan)
	return pm.done
}

type peerManager struct {
	id       string
	store    MessengerStorage
	params   Params
	peers    map[string]*peer
	ps       *PubSub
	counters *Counters
	done     chan struct{}
}

func (pm *peerManager) loop(ctx *context.T, updateChan <-chan discovery.Update) {
	var changed bool
	for {
		select {
		case update, ok := <-updateChan:
			if !ok {
				pm.removePeer(ctx, "")
				close(pm.done)
				return
			}
			pm.processDiscoveryUpdate(ctx, update)
			changed = true
		case <-time.After(time.Second):
			// Updates tend to arrive in batches. Calling
			// checkActivePeers on a timer makes its cost
			// independent of the number of updates received in
			// the interval.
			if changed {
				pm.checkActivePeers(ctx)
				changed = false
			}
		}
	}
}

// processDiscoveryUpdate adds and removes peers as reported in the discovery
// update.
func (pm *peerManager) processDiscoveryUpdate(ctx *context.T, update discovery.Update) {
	id := update.Id().String()
	if update.IsLost() {
		ctx.Infof("%s lost peer %s", pm.id, id)
		pm.removePeer(ctx, id)
		return
	}
	p, exists := pm.peers[id]
	if !exists {
		p = &peer{
			peerId:   id,
			localId:  pm.id,
			store:    pm.store,
			params:   pm.params,
			counters: pm.counters,
		}
		pm.peers[id] = p
		pm.counters.numPeers.Incr(1)
		ctx.Infof("%s has new peer %s", pm.id, id)
	}
	me := &naming.MountEntry{}
	for _, a := range update.Addresses() {
		me.Servers = append(me.Servers, naming.MountedServer{Server: a})
	}
	p.setMountEntry(me)
}

// checkActivePeers decides which peers this node should talk to.
//
// The MaxActivePeers parameter controls how many peers this node should talk
// to. If MaxActivePeers >= the number of peers, this node can talk to all of
// them.
//
// When MaxActivePeers is less than the number of peers, we need to choose which
// peers to talk to such that message propagation to all the peers is likely.
//
// Each node has a unique ID, which is known by all of its peers. So peers can
// be sorted and collectively treated as a ring.
//
// Each node then picks N peers to talk to, where N <= MaxActivePeers. These
// N peers are always the first N power of 2 neighbors to the right, i.e.
//
// Ring: A->B->C->D->E->F->G->H->I->J->K->L->M (->A)
//
// If N=1, Node C talks to D (distance 1)
// If N=2, Node C talks to D, and E (distance 2)
// If N=3, Node C talks to D, E, and G (distance 4)
// If N=4, Node C talks to D, E, G, and K (distance 8)
// etc.
// When a power of 2 falls on a neighbor that was already selected, the next
// unselected neighbor is selected instead.
func (pm *peerManager) checkActivePeers(ctx *context.T) {
	if pm.params.MaxActivePeers == 0 || len(pm.peers) < pm.params.MaxActivePeers {
		for _, p := range pm.peers {
			pm.startPeer(ctx, p)
		}
		return
	}

	// Calculate which neighboring nodes to talk to.
	active := make([]bool, len(pm.peers))
	for i := 0; i < len(active) && i < pm.params.MaxActivePeers; i++ {
		idx := (1<<uint(i%32) - 1) % len(active)
		for active[idx] {
			idx = (idx + 1) % len(active)
		}
		active[idx] = true
	}

	sortedPeers := make([]string, 0, len(pm.peers))
	for id := range pm.peers {
		sortedPeers = append(sortedPeers, id)
	}
	sort.Strings(sortedPeers)

	// Find this node's (would-be) position in the list.
	offset := 0
	for ; offset < len(sortedPeers); offset++ {
		if pm.id < sortedPeers[offset] {
			break
		}
	}

	for i := 0; i < len(sortedPeers); i++ {
		id := sortedPeers[(offset+i)%len(sortedPeers)]
		p := pm.peers[id]
		if active[i] {
			pm.startPeer(ctx, p)
		} else {
			pm.stopPeer(ctx, p)
		}
	}
}

// startPeer starts sending messages to the peer.
func (pm *peerManager) startPeer(ctx *context.T, p *peer) {
	if p.ctx == nil {
		ctx.Infof("start peer %s", p.peerId)
		p.ctx, p.cancel = context.WithCancel(ctx)
		p.queue = newMsgQueue(p.ctx)
		p.psChan = pm.ps.Sub()
		p.done = make(chan struct{})
		go p.subLoop()
		go p.msgLoop()
	}
}

// stopPeer stops sending messages to the peer.
func (pm *peerManager) stopPeer(ctx *context.T, p *peer) {
	if p.ctx != nil {
		ctx.Infof("stop peer %s", p.peerId)
		pm.ps.Unsub(p.psChan)
		p.cancel()
		<-p.done
		p.ctx = nil
		p.cancel = nil
		p.psChan = nil
		p.queue = nil
		p.done = nil
	}
}

// removePeer removes the peer from the peer list, after stopping it.
func (pm *peerManager) removePeer(ctx *context.T, id string) {
	for _, p := range pm.peers {
		delete(pm.peers, id)
		pm.stopPeer(ctx, p)
		pm.counters.numPeers.Incr(-1)
	}
}

type peer struct {
	ctx      *context.T
	cancel   func()
	localId  string
	peerId   string
	store    MessengerStorage
	params   Params
	queue    *MsgQueue
	counters *Counters
	psChan   chan *ifc.Message
	done     chan struct{}
	// mu guards me
	mu sync.Mutex
	me *naming.MountEntry
}

func (p *peer) setMountEntry(me *naming.MountEntry) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.me = me
}

func (p *peer) mountEntry() *naming.MountEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	me := *p.me
	return &me
}

func (p *peer) checkHops(ctx *context.T, msg *ifc.Message) error {
	for _, h := range msg.Hops {
		if h == p.peerId {
			return ifc.NewErrAlreadySeen(ctx, msg.Id)
		}
	}
	if p.params.MaxHops > 0 && len(msg.Hops) >= p.params.MaxHops {
		return ifc.NewErrTooManyHops(ctx, int32(p.params.MaxHops))
	}
	return nil
}

func (p *peer) subLoop() {
	ctx := p.ctx
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-p.psChan:
			if msg == nil {
				return
			}
			if p.checkHops(ctx, msg) != nil {
				continue
			}
			p.queue.Insert(msg.CreationTime, msg.Id)
		}
	}
}

func (p *peer) msgLoop() {
	ctx := p.ctx
	for {
		if err := backoff(p.ctx, func() error { return p.diffAndEnqueue(p.ctx) }); err != nil {
			select {
			case <-p.ctx.Done():
				close(p.done)
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}
		break
	}
	for {
		var msgId string

		select {
		case <-ctx.Done():
			close(p.done)
			return
		case msgId = <-p.queue.Pop():
		}

		pi := p.store.PeerInfo(ctx, msgId, p.peerId)
		if pi.Sent || pi.FatalError {
			continue
		}
		if time.Now().Before(pi.NextAttempt) {
			p.queue.Insert(pi.NextAttempt, msgId)
			continue
		}
		pi.NumAttempts++

		switch err := p.pushMessage(ctx, msgId); {
		case err == nil:
			pi.Sent = true
			p.counters.numMessagesSent.Incr(1)
		case verror.ErrorID(err) == ifc.ErrAlreadySeen.ID:
			pi.Sent = true
		case verror.ErrorID(err) == ifc.ErrContentMismatch.ID:
			pi.FatalError = true
		case verror.ErrorID(err) == ifc.ErrExpired.ID:
			pi.FatalError = true
		case verror.ErrorID(err) == ifc.ErrInvalidSignature.ID:
			pi.FatalError = true
		case verror.ErrorID(err) == ifc.ErrMessageIdCollision.ID:
			pi.FatalError = true
		case verror.ErrorID(err) == ifc.ErrNoRoute.ID:
			pi.FatalError = true
		case verror.ErrorID(err) == ifc.ErrTooManyHops.ID:
			pi.FatalError = true
		case verror.ErrorID(err) == ifc.ErrTooBig.ID:
			pi.FatalError = true
		case verror.ErrorID(err) == verror.ErrUnknownMethod.ID:
			pi.FatalError = true
		case verror.ErrorID(err) == verror.ErrNoAccess.ID:
			pi.FatalError = true
		case verror.ErrorID(err) == verror.ErrNotTrusted.ID:
			pi.FatalError = true
		case verror.ErrorID(err) == verror.ErrNoExist.ID:
			pi.FatalError = true
		default:
			const maxBackoff = 5 * time.Minute
			n := uint(pi.NumAttempts)
			// This is (10 to 20) * 2^n sec.
			backoff := time.Duration((10+rand.Intn(10))<<n) * time.Second
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			pi.NextAttempt = time.Now().Add(backoff)
			p.queue.Insert(pi.NextAttempt, msgId)
		}
		if err := p.store.SetPeerInfo(ctx, msgId, p.peerId, pi); err != nil {
			ctx.Infof("store.SetPeerInfo failed: %v", err)
		}
	}
}

func (p *peer) pushMessage(ctx *context.T, msgId string) error {
	msg, r, err := p.store.OpenRead(ctx, msgId)
	if err != nil {
		return err
	}
	defer r.Close()

	if err := msg.Expired(ctx); err != nil {
		return err
	}
	msg.Hops = append(msg.Hops, p.localId)

	opts := []rpc.CallOpt{
		options.Preresolved{p.mountEntry()},
		options.ServerAuthorizer{p.params.RateAclOut},
	}

	offset := int64(0)
	fPush := func() error {
		return p.doPush(ctx, msg, offset, r, opts)
	}
push:
	if err = backoff(ctx, fPush); verror.ErrorID(err) == ifc.ErrIncorrectOffset.ID {
		fOffset := func() error {
			var err error
			offset, err = ifc.MessengerClient("").ResumeOffset(ctx, msg, opts...)
			return err
		}
		if err = backoff(ctx, fOffset); err != nil {
			return err
		}
		if _, err := r.Seek(offset, 0); err != nil {
			return err
		}
		goto push
	}
	return err
}

func backoff(ctx *context.T, f func() error) (err error) {
	for n := uint(0); ; n++ {
		if err = f(); verror.Action(err) == verror.RetryBackoff {
			const maxBackoff = time.Minute
			// This is (100 to 200) * 2^n ms.
			backoff := time.Duration((100+rand.Intn(100))<<n) * time.Millisecond
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			select {
			case <-ctx.Done():
			case <-time.After(backoff):
			}
			continue
		}
		return
	}
}

func (p *peer) doPush(ctx *context.T, msg ifc.Message, offset int64, r io.Reader, opts []rpc.CallOpt) error {
	call, err := ifc.MessengerClient("").Push(ctx, msg, offset, opts...)
	if err != nil {
		return err
	}
	buf := make([]byte, 2048)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if err := call.SendStream().Send(buf[:n]); err != nil {
				return err
			}
			p.counters.numBytesSent.Incr(int64(n))
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return call.Finish()
}

func (p *peer) diffAndEnqueue(ctx *context.T) error {
	opts := []rpc.CallOpt{
		options.Preresolved{p.mountEntry()},
		options.ServerAuthorizer{p.params.RateAclOut},
	}
	mctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ch, err := p.store.Manifest(mctx)
	if err != nil {
		return err
	}

	const batchSize = 256
	type batchItem struct {
		id string
		pi PeerInfo
	}
	var call ifc.MessengerDiffClientCall
	for {
		batch := make([]string, 0, batchSize)
		batchInfo := make([]batchItem, 0, cap(batch))
		for i := 0; i < cap(batch); i++ {
			var msg *ifc.Message
			select {
			case <-ctx.Done():
				return verror.NewErrCanceled(ctx)
			case msg = <-ch:
			}
			if msg == nil {
				break
			}
			pi := p.store.PeerInfo(ctx, msg.Id, p.peerId)
			if pi.Sent || pi.FatalError {
				continue
			}
			if p.checkHops(ctx, msg) != nil {
				continue
			}
			batch = append(batch, msg.Id)
			if pi.NextAttempt.IsZero() {
				// Make an effort to deliver the messages in the
				// order they were created.
				pi.NextAttempt = msg.CreationTime
			}
			batchInfo = append(batchInfo, batchItem{msg.Id, pi})
		}
		if len(batch) == 0 {
			break
		}
		if call == nil {
			if call, err = ifc.MessengerClient("").Diff(ctx, opts...); err != nil {
				return err
			}
		}
		if err := call.SendStream().Send(batch); err != nil {
			return err
		}
		if !call.RecvStream().Advance() {
			return verror.New(verror.ErrBadProtocol, ctx)
		}
		responses := call.RecvStream().Value()
		if len(batch) != len(responses) {
			return verror.New(verror.ErrBadProtocol, ctx)
		}
		for i, hasIt := range responses {
			id, pi := batchInfo[i].id, batchInfo[i].pi
			if hasIt {
				pi.Sent = true
				if err := p.store.SetPeerInfo(ctx, id, p.peerId, pi); err != nil {
					ctx.Infof("store.SetPeerInfo failed: %v", err)
				}
				continue
			}
			p.queue.Insert(pi.NextAttempt, id)
		}
	}
	if call == nil {
		return nil
	}
	if err := call.SendStream().Close(); err != nil {
		return err
	}
	return call.Finish()
}
