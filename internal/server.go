// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/discovery"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"

	"v.io/x/ref/lib/discovery/global"

	"messenger/ifc"
)

const (
	ifcName = "messenger/ifc.Messenger"
)

type Params struct {
	AdvertisementID      string
	EnableLocalDiscovery bool
	GlobalDiscoveryPaths []string
	MaxActivePeers       int
	MaxHops              int
	MaxMessageLength     int64
	MountTTL             time.Duration
	RateAclIn            RateAcl
	RateAclOut           RateAcl
	RateAclSender        RateAcl
	ScanInterval         time.Duration
	Store                MessengerStorage
}

type Node struct {
	id       string
	server   rpc.Server
	ps       *PubSub
	pms      []*peerManager
	counters *Counters
	cancel   func()
}

func (n *Node) Id() string {
	return n.id
}

func (n *Node) Server() rpc.Server {
	return n.server
}

func (n *Node) PubSub() *PubSub {
	return n.ps
}

func (n *Node) Stop() {
	n.cancel()
}

func (n *Node) DebugString() string {
	s := []string{}
	for _, pm := range n.pms {
		s = append(s, pm.debugString())
	}
	s = append(s, fmt.Sprintf("Counters: num-peers:%d num-messages-sent:%d num-bytes-sent:%d num-messages-received:%d num-bytes-received:%d",
		n.counters.numPeers.Value(),
		n.counters.numMessagesSent.Value(),
		n.counters.numBytesSent.Value(),
		n.counters.numMessagesReceived.Value(),
		n.counters.numBytesReceived.Value(),
	))

	return strings.Join(s, ", ")
}

func StartNode(ctx *context.T, params Params) (*Node, error) {
	ctx, cancel := context.WithCancel(ctx)

	ps := newPubSub(ctx)

	var adId discovery.AdId

	var err error
	if params.AdvertisementID != "" {
		adId, err = discovery.ParseAdId(params.AdvertisementID)
	} else {
		adId, err = discovery.NewAdId()
	}
	if err != nil {
		return nil, err
	}
	counters := NewCounters(adId.String())
	m := &Messenger{Params: params, Notifier: ps, Counters: counters}

	ctx, server, err := v23.WithNewServer(ctx, "", ifc.MessengerRepositoryServer(m), security.AllowEveryone())
	if err != nil {
		return nil, err
	}

	dones := []<-chan struct{}{}
	pms := []*peerManager{}

	// advertise restarts the advertisement when the server's endpoints
	// change.
	advertise := func(ctx *context.T, disc discovery.T) <-chan struct{} {
		done := make(chan struct{})
		go func() {
			defer close(done)
			status := server.Status()
			ad := &discovery.Advertisement{
				Id:            adId,
				InterfaceName: ifcName,
			}
			for _, ep := range status.Endpoints {
				ad.Addresses = append(ad.Addresses, naming.JoinAddressName(ep.String(), ""))
			}
			sort.Strings(ad.Addresses)
			ctx.Infof("Starting advertisement: %v", ad)

			for exit := false; !exit; {
				adCtx, adCancel := context.WithCancel(ctx)
				adDone, err := disc.Advertise(adCtx, ad, nil)
				if err != nil {
					ctx.Errorf("disc.Advertise failed: %v", err)
					adCancel()
					select {
					case <-ctx.Done():
						exit = true
					case <-time.After(10 * time.Second):
					}
					continue
				}
			change:
				for {
					select {
					case <-ctx.Done():
						exit = true
						break change
					case <-status.Dirty:
						status = server.Status()
						addr := []string{}
						for _, ep := range status.Endpoints {
							addr = append(addr, naming.JoinAddressName(ep.String(), ""))
						}
						sort.Strings(addr)
						if !reflect.DeepEqual(ad.Addresses, addr) {
							ad.Addresses = addr
							ctx.Infof("Restarting advertisement: %v", ad)
							break change
						}
					}
				}
				adCancel()
				<-adDone
			}
		}()
		return done
	}

	startDiscovery := func(disc discovery.T, err error) error {
		if err != nil {
			return err
		}
		dones = append(dones, advertise(ctx, disc))
		updateChan, err := disc.Scan(ctx, fmt.Sprintf(`v.InterfaceName="%s"`, ifcName))
		if err != nil {
			return err
		}
		pm := startPeerManager(ctx, adId.String(), updateChan, ps, params.Store, params, counters)
		dones = append(dones, pm.done)
		pms = append(pms, pm)
		return nil
	}

	if params.EnableLocalDiscovery {
		if err := startDiscovery(v23.NewDiscovery(ctx)); err != nil {
			return nil, err
		}
	}

	for _, path := range params.GlobalDiscoveryPaths {
		if err := startDiscovery(global.NewWithTTL(ctx, path, params.MountTTL, params.ScanInterval)); err != nil {
			return nil, err
		}
	}

	return &Node{adId.String(), server, ps, pms, counters, func() {
		cancel()
		<-ps.done
		for _, done := range dones {
			<-done
		}
	}}, nil
}
