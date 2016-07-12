// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal

import (
	"fmt"
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

func StartNode(ctx *context.T, params Params) (rpc.Server, *PubSub, func(), error) {
	ctx, cancel := context.WithCancel(ctx)

	ps := newPubSub(ctx)
	m := &Messenger{Params: params, Notifier: ps}

	var adId discovery.AdId

	var err error
	if params.AdvertisementID != "" {
		adId, err = discovery.ParseAdId(params.AdvertisementID)
	} else {
		adId, err = discovery.NewAdId()
	}
	if err != nil {
		return nil, nil, nil, err
	}

	ctx, server, err := v23.WithNewServer(ctx, "", ifc.MessengerRepositoryServer(m), security.AllowEveryone())
	if err != nil {
		return nil, nil, nil, err
	}

	if ls := v23.GetListenSpec(ctx); ls.Proxy != "" {
		// Wait for proxied address
		for {
			status := server.Status()
			err, ok := status.ProxyErrors[ls.Proxy]
			if !ok {
				<-status.Dirty
				continue
			}
			if err != nil {
				return nil, nil, nil, err
			}
			break
		}
	}

	ad := &discovery.Advertisement{
		Id:            adId,
		InterfaceName: ifcName,
	}
	for _, ep := range server.Status().Endpoints {
		ad.Addresses = append(ad.Addresses, naming.JoinAddressName(ep.String(), ""))
	}

	counters := NewCounters(adId.String())
	dones := []<-chan struct{}{}

	startDiscovery := func(disc discovery.T, err error) error {
		if err != nil {
			return err
		}
		done, err := disc.Advertise(ctx, ad, nil)
		if err != nil {
			return err
		}
		dones = append(dones, done)
		updateChan, err := disc.Scan(ctx, fmt.Sprintf(`v.InterfaceName="%s"`, ifcName))
		if err != nil {
			return err
		}
		done = startPeerManager(ctx, adId.String(), updateChan, ps, params.Store, params, counters)
		dones = append(dones, done)
		return nil
	}

	if params.EnableLocalDiscovery {
		if err := startDiscovery(v23.NewDiscovery(ctx)); err != nil {
			return nil, nil, nil, err
		}
	}

	for _, path := range params.GlobalDiscoveryPaths {
		if err := startDiscovery(global.NewWithTTL(ctx, path, params.MountTTL, params.ScanInterval)); err != nil {
			return nil, nil, nil, err
		}
	}

	return server, ps, func() {
		cancel()
		<-ps.done
		for _, done := range dones {
			<-done
		}
	}, nil
}
