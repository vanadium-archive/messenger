// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal_test

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/discovery"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/v23/services/stats"
	"v.io/v23/vdl"

	_ "v.io/x/ref/runtime/factories/roaming"
	"v.io/x/ref/test"
	"v.io/x/ref/test/testutil"

	"messenger/ifc"
	"messenger/internal"
)

func startServer(ctx *context.T, t *testing.T, params internal.Params) (string, string, func()) {
	ctx = v23.WithListenSpec(ctx, rpc.ListenSpec{
		Addrs: rpc.ListenAddrs{{"tcp", "127.0.0.1:0"}},
	})
	if params.AdvertisementID == "" {
		id, _ := discovery.NewAdId()
		params.AdvertisementID = id.String()
	}
	rateacl := internal.RateAcl{
		{Acl: access.AccessList{In: []security.BlessingPattern{security.AllPrincipals}}, Limit: 100.0},
	}
	rateacl.Init()
	params.RateAclIn = rateacl
	params.RateAclOut = rateacl
	params.RateAclSender = rateacl

	server, _, stop, err := internal.StartNode(ctx, params)
	if err != nil {
		t.Fatalf("internal.StartNode failed: %v", err)
		return "", "", nil
	}
	ep := server.Status().Endpoints[0].String()
	return naming.JoinAddressName(ep, ""), params.AdvertisementID, stop
}

func TestPush(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	tmpdir, err := ioutil.TempDir("", "TestPush-")
	if err != nil {
		t.Fatalf("ioutil.TempDir failed: %v", err)
	}
	defer os.RemoveAll(tmpdir)

	serverp := testutil.NewPrincipal()
	alicep := testutil.NewPrincipal()
	bobp := testutil.NewPrincipal()
	idp := testutil.NewIDProvider("root")
	idp.Bless(serverp, "server")
	idp.Bless(alicep, "alice")
	idp.Bless(bobp, "bob")

	aliceCtx, err := v23.WithPrincipal(ctx, alicep)
	if err != nil {
		t.Fatalf("failed to set alice principal: %v", err)
	}
	bobCtx, err := v23.WithPrincipal(ctx, bobp)
	if err != nil {
		t.Fatalf("failed to set bob principal: %v", err)
	}

	// Start server as 'server'
	serverCtx, err := v23.WithPrincipal(ctx, serverp)
	if err != nil {
		t.Fatalf("failed to set server principal: %v", err)
	}
	params := internal.Params{
		Store: internal.NewFileStorage(ctx, tmpdir),
	}
	addr, _, stop := startServer(serverCtx, t, params)
	defer stop()

	msgTxt := []byte("Hello World\n")

	// Create Message as 'alice'
	msg, err := createMessage(aliceCtx, msgTxt)
	if err != nil {
		t.Fatalf("createMessage failed: %v", err)
	}

	// Push Message as 'bob'
	if err := push(bobCtx, addr, msg, msgTxt); err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	// Pull Message
	m, content, err := pull(bobCtx, addr, msg.Id)
	if err != nil {
		t.Fatalf("Pull failed: %v", err)
	}
	if !bytes.Equal(msgTxt, content) {
		t.Errorf("unexpected content. Got %q, expected %q", content, msgTxt)
	}
	if !bytes.Equal(m.Hash(), msg.Hash()) {
		t.Errorf("unexpected message hash. Got %q, expected %q", m.Hash(), msg.Hash())
	}
}

func TestDiscovery(t *testing.T) {
	ctx, shutdown := test.V23InitWithMounttable()
	defer shutdown()

	tmpdir, err := ioutil.TempDir("", "TestDiscovery-")
	if err != nil {
		t.Fatalf("ioutil.TempDir failed: %v", err)
	}
	defer os.RemoveAll(tmpdir)

	const N = 32
	nodes := make([]struct {
		id   string
		name string
		addr string
		dir  string
	}, N)

	for i := 0; i < N; i++ {
		nodes[i].name = fmt.Sprintf("node%03d", i)
		nodes[i].dir = filepath.Join(tmpdir, nodes[i].name)

		params := internal.Params{
			AdvertisementID:      fmt.Sprintf("%032x", i),
			GlobalDiscoveryPaths: []string{"disc"},
			MaxActivePeers:       1,
			MaxHops:              N,
			ScanInterval:         2 * time.Second,
			Store:                internal.NewFileStorage(ctx, nodes[i].dir),
		}
		addr, id, stop := startServer(ctx, t, params)
		nodes[i].id = id
		nodes[i].addr = addr
		defer stop()
	}

	msgTxt := []byte("Hello World\n")

	// Create Message
	msg, err := createMessage(ctx, msgTxt)
	if err != nil {
		t.Fatalf("createMessage failed: %v", err)
	}

	// Push the message to the first node.
	if err := push(ctx, nodes[0].addr, msg, msgTxt); err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	// Look for the message in all the node stores.
	for {
		allThere := true
		for i := 0; i < N; i++ {
			doneFile := filepath.Join(nodes[i].dir, base64.URLEncoding.EncodeToString([]byte(msg.Id)), "done")
			if _, err := os.Stat(doneFile); err != nil {
				allThere = false
				break
			}
		}
		if allThere {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Pull message from each node.
	for i := 0; i < N; i++ {
		// Pull Message
		m, content, err := pull(ctx, nodes[i].addr, msg.Id)
		if err != nil {
			t.Fatalf("pull failed: %v", err)
		}
		if !bytes.Equal(msgTxt, content) {
			t.Errorf("unexpected content. Got %q, expected %q", content, msgTxt)
		}
		if !bytes.Equal(m.Hash(), msg.Hash()) {
			t.Errorf("unexpected message hash. Got %q, expected %q", m.Hash(), msg.Hash())
		}
		t.Logf("Hops: %q", m.Hops)
	}

	// Get counters from each node.
	var sum int64
	for i := 0; i < N; i++ {
		n := "num-peer-messages-sent"
		name := naming.Join(nodes[i].addr, "__debug/stats/messenger", nodes[i].id, n)
		v := getCounter(t, ctx, name)
		sum += v
		t.Logf("%s[%d]: %d (sum %d)", n, i, v, sum)
	}
}

func TestMultiZoneDiscovery(t *testing.T) {
	ctx, shutdown := test.V23InitWithMounttable()
	defer shutdown()

	tmpdir, err := ioutil.TempDir("", "TestMultiZoneDiscovery-")
	if err != nil {
		t.Fatalf("ioutil.TempDir failed: %v", err)
	}
	defer os.RemoveAll(tmpdir)

	const N = 25
	nodes := make([]struct {
		id   string
		name string
		addr string
		dir  string
	}, N)

	for i := 0; i < N; i++ {
		nodes[i].name = fmt.Sprintf("node%03d", i)
		nodes[i].dir = filepath.Join(tmpdir, nodes[i].name)

		// Each node is in 2 discovery zones.
		// Node  Zone-0  Zone-1  Zone-2  Zone-3  Zone-4  Zone-5
		//   0     X       X
		//   1             X       X
		//   2                     X      X
		//   3                            X       X
		//   4                                    X       X
		// etc.
		zones := []string{
			fmt.Sprintf("zone-%d", i%10),
			fmt.Sprintf("zone-%d", (i+1)%10),
		}
		params := internal.Params{
			AdvertisementID:      fmt.Sprintf("%032x", i),
			GlobalDiscoveryPaths: zones,
			MaxActivePeers:       2,
			ScanInterval:         2 * time.Second,
			Store:                internal.NewFileStorage(ctx, nodes[i].dir),
		}
		addr, id, stop := startServer(ctx, t, params)
		nodes[i].id = id
		nodes[i].addr = addr
		defer stop()
	}

	msgTxt := []byte("Hello World\n")

	// Create Message
	msg, err := createMessage(ctx, msgTxt)
	if err != nil {
		t.Fatalf("createMessage failed: %v", err)
	}

	// Push the message to the first node.
	if err := push(ctx, nodes[0].addr, msg, msgTxt); err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	// Look for the message in all the node stores.
	for {
		allThere := true
		for i := 0; i < N; i++ {
			doneFile := filepath.Join(nodes[i].dir, base64.URLEncoding.EncodeToString([]byte(msg.Id)), "done")
			if _, err := os.Stat(doneFile); err != nil {
				allThere = false
				break
			}
		}
		if allThere {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Pull message from each node.
	for i := 0; i < N; i++ {
		// Pull Message
		m, content, err := pull(ctx, nodes[i].addr, msg.Id)
		if err != nil {
			t.Fatalf("pull failed: %v", err)
		}
		if !bytes.Equal(msgTxt, content) {
			t.Errorf("unexpected content. Got %q, expected %q", content, msgTxt)
		}
		if !bytes.Equal(m.Hash(), msg.Hash()) {
			t.Errorf("unexpected message hash. Got %q, expected %q", m.Hash(), msg.Hash())
		}
		t.Logf("Hops: %q", m.Hops)
	}

	// Get counters from each node.
	var sum int64
	for i := 0; i < N; i++ {
		n := "num-peer-messages-sent"
		name := naming.Join(nodes[i].addr, "__debug/stats/messenger", nodes[i].id, n)
		v := getCounter(t, ctx, name)
		sum += v
		t.Logf("%s[%d]: %d (sum %d)", n, i, v, sum)
	}
}

func createMessage(ctx *context.T, content []byte) (ifc.Message, error) {
	p := v23.GetPrincipal(ctx)

	msg := internal.NewMessageFromBytes(content)
	msg.SenderBlessings, _ = p.BlessingStore().Default()
	msg.Lifespan = time.Hour
	msg.Recipient = "recipient"
	var err error
	msg.Signature, err = p.Sign(msg.Hash())
	if err != nil {
		return ifc.Message{}, err
	}
	return msg, nil
}

func preresolved(addr string) rpc.CallOpt {
	me := &naming.MountEntry{
		Servers: []naming.MountedServer{{Server: addr}},
	}
	return options.Preresolved{me}
}

func push(ctx *context.T, addr string, msg ifc.Message, content []byte) error {
	msg.Hops = append(msg.Hops, "sender")
	call, err := ifc.MessengerClient("").Push(ctx, msg, 0, preresolved(addr))
	if err != nil {
		return err
	}
	if err := call.SendStream().Send(content); err != nil {
		return err
	}
	return call.Finish()
}

func pull(ctx *context.T, addr, id string) (ifc.Message, []byte, error) {
	call, err := ifc.MessengerRepositoryClient("").Pull(ctx, id, 0, preresolved(addr))
	if err != nil {
		return ifc.Message{}, nil, err
	}
	var content []byte
	for call.RecvStream().Advance() {
		content = append(content, call.RecvStream().Value()...)
	}
	msg, err := call.Finish()
	return msg, content, err
}

func getCounter(t *testing.T, ctx *context.T, name string) int64 {
	st := stats.StatsClient(name)
	v, err := st.Value(ctx)
	if err != nil {
		t.Fatalf("Failed to get %q: %v", name, err)
		return -1
	}
	var value int64
	if err := vdl.Convert(&value, v); err != nil {
		t.Fatalf("Unexpected value type for %q: %v", name, err)
	}
	return value
}
