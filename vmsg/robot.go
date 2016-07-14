// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run $JIRI_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go . -help

package main

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"v.io/v23/context"

	"v.io/x/lib/cmdline"
	"v.io/x/ref/lib/signals"
	"v.io/x/ref/lib/v23cmd"

	"messenger/internal"
)

var cmdRobot = &cmdline.Command{
	Runner: v23cmd.RunnerFunc(runRobot),
	Name:   "robot",
	Short:  "Run a robot node that sends random messages every few seconds",
	Long: `
Run a robot node.

The robot understands the following commands:

!talk   The robot starts sending random message every few seconds.
!hush   The robot stops talking.
!debug  The robot sends its node debug information.
`,
}

func runRobot(ctx *context.T, env *cmdline.Env, args []string) error {
	params, err := paramsFromFlags(ctx, env)
	if err != nil {
		return err
	}
	node, err := internal.StartNode(ctx, params)
	if err != nil {
		return err
	}
	defer node.Stop()

	var talkChan chan struct{}
	talk := func() {
		if talkChan != nil {
			return
		}
		talkChan = make(chan struct{})
		go func() {
			for {
				n := rand.Intn(30) + 15
				select {
				case <-talkChan:
					return
				case <-ctx.Done():
					return
				case <-time.After(time.Duration(n) * time.Second):
					mtxt := fmt.Sprintf("Your lucky number is %d", n)
					if err := sendMessage(ctx, node.PubSub(), params.Store, mtxt, ""); err != nil {
						ctx.Errorf("sendMessage failed: %v", err)
					}
				}
			}
		}()
	}
	hush := func() {
		if talkChan == nil {
			return
		}
		close(talkChan)
		talkChan = nil
	}

	go func() {
		for msg := range node.PubSub().Sub() {
			_, r, err := params.Store.OpenRead(ctx, msg.Id)
			if err != nil {
				continue
			}
			msgText, filename, err := decryptChatMessage(msg.Id, r, incomingDir)
			r.Close()
			if err != nil {
				ctx.Infof("decryptChatMessage failed: %v", err)
				continue
			}
			ctx.Infof("Incoming message from %s %q %q", msg.SenderBlessings, msgText, filename)
			var reply string
			switch {
			case strings.HasPrefix(msgText, "\x01PING"):
				reply = "\x01PONG" + msgText[5:]
			case msgText == "!debug":
				reply = node.DebugString()
			case msgText == "!talk":
				talk()
				reply = "Now we're talking!"
			case msgText == "!hush":
				hush()
				reply = "Shhhhh..."
			}
			if reply != "" {
				if err := sendMessage(ctx, node.PubSub(), params.Store, reply, ""); err != nil {
					ctx.Errorf("sendMessage failed: %v", err)
				}
			}
		}
		hush()
	}()

	<-signals.ShutdownOnSignals(ctx)
	return nil
}
