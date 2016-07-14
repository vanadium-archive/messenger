// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run $JIRI_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go . -help

package main

import (
	"flag"
	"os"
	"strings"
	"time"

	"v.io/v23/context"

	"v.io/x/lib/cmdline"
	"v.io/x/ref/lib/signals"
	"v.io/x/ref/lib/v23cmd"
	_ "v.io/x/ref/runtime/factories/roaming"

	"messenger/internal"
)

const defaultEncryptionKey = "This is not secure!!!"

var (
	cmdRoot = &cmdline.Command{
		Name:  "vmsg",
		Short: "Runs the vanadium messenger service",
		Long:  "Runs the vanadium messenger service.",
		Children: []*cmdline.Command{
			cmdNode, cmdChat, cmdRobot,
		},
	}
	cmdNode = &cmdline.Command{
		Runner: v23cmd.RunnerFunc(runNode),
		Name:   "node",
		Short:  "Run the standalone node",
		Long:   "Run the standalone node.",
	}

	advertisementId   string
	enableLocalDisc   bool
	globalDiscPaths   string
	storeDir          string
	maxActivePeers    int
	maxHops           int
	rateAclInJson     string
	rateAclOutJson    string
	rateAclSenderJson string
	encryptionKey     string

	incomingDir string
)

func main() {
	if f := flag.Lookup("alsologtostderr"); f != nil {
		f.Value.Set("false")
	}
	if f := flag.Lookup("stderrthreshold"); f != nil {
		f.Value.Set("FATAL")
	}
	cmdRoot.Flags.StringVar(&advertisementId, "advertisement-id", "", "The advertisement ID to use. If left empty, a random one is generated.")
	cmdRoot.Flags.BoolVar(&enableLocalDisc, "enable-local-discovery", true, "Whether local discovery, i.e. using mDNS and/or BLE, should be enabled.")
	cmdRoot.Flags.StringVar(&globalDiscPaths, "global-discovery-paths", "", "A comma-separated list of namespace paths to use for global discovery.")
	cmdRoot.Flags.StringVar(&storeDir, "store-dir", "", "The name of the local directory where to store the messages.")
	cmdRoot.Flags.IntVar(&maxActivePeers, "max-active-peers", 2, "The maximum number of peers to send updates to concurrently.")
	cmdRoot.Flags.IntVar(&maxHops, "max-hops", 50, "The maximum number of hops that a message can go through.")
	cmdRoot.Flags.StringVar(&rateAclInJson, "rate-acl-in", `[{"acl":{"In":["..."]},"limit":20}]`, "The RateAcl to authorize incoming RPCs, in JSON format")
	cmdRoot.Flags.StringVar(&rateAclOutJson, "rate-acl-out", `[{"acl":{"In":["..."]},"limit":100}]`, "The RateAcl to authorize outgoing RPCs, in JSON format")
	cmdRoot.Flags.StringVar(&rateAclSenderJson, "rate-acl-sender", `[{"acl":{"In":["..."]},"limit":100}]`, "The RateAcl to authorize the sender of incoming messages, in JSON format")
	cmdRoot.Flags.StringVar(&encryptionKey, "encryption-key", defaultEncryptionKey, "Messages are encrypted with AES256 using this key")

	cmdChat.Flags.StringVar(&incomingDir, "incoming-dir", os.TempDir(), "The directory where to save incoming files")

	cmdline.HideGlobalFlagsExcept()
	cmdline.Main(cmdRoot)
}

func paramsFromFlags(ctx *context.T, env *cmdline.Env) (params internal.Params, err error) {
	if storeDir == "" {
		err = env.UsageErrorf("--store-dir must be set")
		return
	}
	var paths []string
	if globalDiscPaths != "" {
		paths = strings.Split(globalDiscPaths, ",")
	}

	params.AdvertisementID = advertisementId
	params.EnableLocalDiscovery = enableLocalDisc
	params.GlobalDiscoveryPaths = paths
	params.MaxActivePeers = maxActivePeers
	params.MaxHops = maxHops
	params.MountTTL = 20 * time.Second
	params.ScanInterval = 10 * time.Second
	params.Store = internal.NewFileStorage(ctx, storeDir)

	if params.RateAclIn, err = internal.RateAclFromJSON([]byte(rateAclInJson)); err != nil {
		return
	}
	if params.RateAclOut, err = internal.RateAclFromJSON([]byte(rateAclOutJson)); err != nil {
		return
	}
	if params.RateAclSender, err = internal.RateAclFromJSON([]byte(rateAclSenderJson)); err != nil {
		return
	}
	return
}

func runNode(ctx *context.T, env *cmdline.Env, args []string) error {
	params, err := paramsFromFlags(ctx, env)
	if err != nil {
		return err
	}
	node, err := internal.StartNode(ctx, params)
	if err != nil {
		return err
	}
	defer node.Stop()
	ctx.Infof("Listening on: %v", node.Server().Status().Endpoints)
	<-signals.ShutdownOnSignals(ctx)
	return nil
}
