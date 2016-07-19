// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run $JIRI_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go . -help

package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/jroimartin/gocui"

	"v.io/v23"
	"v.io/v23/context"

	"v.io/x/lib/cmdline"
	lsec "v.io/x/ref/lib/security"
	"v.io/x/ref/lib/v23cmd"

	"messenger/ifc"
	"messenger/internal"
)

var cmdChat = &cmdline.Command{
	Runner: v23cmd.RunnerFunc(runChat),
	Name:   "chat",
	Short:  "Run chat demo application",
	Long: `
Run chat demo application.

The messages are encrypted with AES256 and the key passed with the
--encryption-key flag.

The messages are sent to all the messenger nodes in the same discovery
domain(s). Anyone who has the same encryption key will be able to read them.

When a new node is discovered, all the messages that are not expired are shared
with it. Messages expire after 15 minutes.
`,
}

func runChat(ctx *context.T, env *cmdline.Env, args []string) error {
	params, err := paramsFromFlags(ctx, env)
	if err != nil {
		return err
	}

	node, err := internal.StartNode(ctx, params)
	if err != nil {
		return err
	}
	defer node.Stop()

	return runChatApp(ctx, node, params.Store)
}

func runChatApp(ctx *context.T, node *internal.Node, store internal.MessengerStorage) error {
	g := gocui.NewGui()
	if err := g.Init(); err != nil {
		return err
	}
	defer g.Close()
	g.Cursor = true
	g.BgColor = gocui.ColorBlue
	g.FgColor = gocui.ColorWhite
	app := app{ctx, g, node, store}

	g.SetLayout(func(g *gocui.Gui) error {
		maxX, maxY := g.Size()
		commandInputViewHeight := 3
		if v, err := g.SetView("history", -1, 0, maxX, maxY-commandInputViewHeight); err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			v.Title = " Vanadium Peer to Peer Chat "
			v.BgColor = gocui.ColorBlack
			v.Autoscroll = true
			v.Wrap = true
		}
		if v, err := g.SetView("commandInput", 0, maxY-commandInputViewHeight, maxX-1, maxY-1); err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			v.Title = " Enter text or command. Type /help for help "
			v.BgColor = gocui.ColorBlue
			v.FgColor = gocui.ColorYellow
			v.Editable = true
		}
		if err := g.SetCurrentView("commandInput"); err != nil {
			return err
		}
		return nil
	})

	if err := g.SetKeybinding("", gocui.KeyCtrlC, 0,
		func(g *gocui.Gui, v *gocui.View) error {
			return gocui.ErrQuit
		},
	); err != nil {
		return err
	}

	if err := g.SetKeybinding("commandInput", gocui.KeyEnter, 0,
		func(g *gocui.Gui, v *gocui.View) error {
			txt := strings.TrimSpace(v.Buffer())
			v.Clear()
			v.SetOrigin(0, 0)
			return app.handleInput(txt)
		},
	); err != nil {
		return err
	}

	go app.loop()

	if err := g.MainLoop(); err != nil && err != gocui.ErrQuit {
		return err
	}
	return nil
}

type app struct {
	ctx   *context.T
	g     *gocui.Gui
	node  *internal.Node
	store internal.MessengerStorage
}

func (a *app) flush() {
	// AFAICT, this is the only way to update the UI.
	a.g.Execute(func(g *gocui.Gui) error { return nil })
}

func (a *app) print(s string) {
	if view, err := a.g.View("history"); err == nil {
		view.Write([]byte(s))
	}
	a.flush()
}

func (a *app) printf(format string, args ...interface{}) {
	a.print(fmt.Sprintf(format, args...))
}

func (a *app) printErrorf(format string, args ...interface{}) {
	a.print(color.RedString(fmt.Sprintf(format, args...)))
}

func (a *app) clear() {
	view, err := a.g.View("history")
	if err != nil {
		return
	}
	view.Clear()
	view.SetOrigin(0, 0)
	a.flush()
}

func (a *app) debug() {
	a.printf("*** %s\n", a.node.DebugString())
}

func (a *app) help() {
	a.print("***\n")
	a.print("*** Welcome to Vanadium Peer to Peer Chat\n")
	a.print("*** https://github.com/vanadium/messenger\n")
	a.print("***\n")
	a.printf("*** %s\n", color.RedString("This is a demo application."))
	a.print("***\n")
	if encryptionKey == defaultEncryptionKey {
		a.printf("*** %s\n", color.RedString("Messages are encrypted with the default key. They are NOT private."))
	}
	a.print("***\n")
	a.print("*** Messages are stored and relayed peer-to-peer for 15 minutes after they are\n")
	a.print("*** created. New peers will see up to 15 minutes of history when they join.\n")
	a.print("***\n")
	a.print("*** Available commands are:\n")
	a.print("***   /clear to clear this window\n")
	a.print("***   /debug to show the local node's debug information\n")
	a.print("***   /help to show this help message\n")
	a.print("***   /history to show the message history\n")
	a.print("***   /ping to send a ping\n")
	a.print("***   /quit to exit\n")
	a.print("***   /share <filename> to share a file\n")
	a.print("***\n")
	a.print("*** Type /quit or press Ctrl-C to exit.\n")
	a.print("***\n")
}

func (a *app) history() {
	ch, err := a.store.Manifest(a.ctx)
	if err != nil {
		return
	}
	msgs := messages{}
	for msg := range ch {
		msgs = append(msgs, msg)
	}
	sort.Sort(msgs)
	for _, msg := range msgs {
		a.handleMessage(msg)
	}
}

func (a *app) handleInput(m string) error {
	if m == "" {
		return nil
	}
	fname := ""
	switch {
	case m == "/debug":
		a.debug()
		return nil
	case m == "/clear":
		a.clear()
		return nil
	case m == "/help":
		a.help()
		return nil
	case m == "/history":
		a.history()
		return nil
	case m == "/ping":
		m = fmt.Sprintf("\x01PING %s %d", a.node.Id(), time.Now().UnixNano())
	case m == "/quit":
		return gocui.ErrQuit
	case strings.HasPrefix(m, "/share"):
		fname = strings.TrimSpace(m[6:])
		a.printf("*** Sharing %s\n", fname)
		m = ""
	case strings.HasPrefix(m, "/"):
		a.printErrorf("*** Unknown command %s\n", m)
		return nil
	}
	if err := sendMessage(a.ctx, a.node.PubSub(), a.store, m, fname); err != nil {
		a.printErrorf("*** Unable to send message: %v\n", err)
	}
	return nil
}

func (a *app) handleMessage(msg *ifc.Message) {
	_, r, err := a.store.OpenRead(a.ctx, msg.Id)
	if err != nil {
		return
	}
	msgText, filename, err := decryptChatMessage(msg.Id, r, incomingDir)
	r.Close()
	if err != nil {
		a.printErrorf("*** Unable to handle message: %v\n", err)
		return
	}

	sender := color.GreenString("%s", msg.SenderBlessings)
	prefix := color.WhiteString("%s %2d %5.2fs",
		msg.CreationTime.Local().Format("15:04:05"),
		len(msg.Hops),
		time.Since(msg.CreationTime).Seconds())
	if msgText != "" {
		ping := color.GreenString("PING")
		switch {
		case strings.HasPrefix(msgText, "\x01PING"):
			a.printf("%s %s from %s\n", prefix, ping, sender)
			reply := "\x01PONG" + msgText[5:]
			if err := sendMessage(a.ctx, a.node.PubSub(), a.store, reply, ""); err != nil {
				a.printErrorf("*** Unable to send ping reply: %v", err)
			}
		case strings.HasPrefix(msgText, "\x01PONG "):
			p := strings.Split(msgText, " ")
			if len(p) != 3 || p[1] != a.node.Id() {
				return
			}
			if i, err := strconv.ParseInt(p[2], 10, 64); err == nil {
				t := time.Unix(0, i)
				a.printf("%s %s reply from %s: %.2fs\n", prefix, ping, sender, time.Since(t).Seconds())
			}
		default:
			a.printf("%s <%s> %s\n", prefix, sender, color.YellowString(msgText))
		}
	}
	if filename != "" {
		a.printf("%s Received shared file from %s: %s\n", prefix, sender, color.YellowString(filename))
	}
}

func (a *app) loop() {
	a.help()
	ch := a.node.PubSub().Sub()
	a.history()
	for msg := range ch {
		a.handleMessage(msg)
	}
}

type messages []*ifc.Message

func (m messages) Len() int           { return len(m) }
func (m messages) Less(i, j int) bool { return m[i].CreationTime.Before(m[j].CreationTime) }
func (m messages) Swap(i, j int)      { m[i], m[j] = m[j], m[i] }

func sendMessage(ctx *context.T, ps *internal.PubSub, store internal.MessengerStorage, txt, fname string) error {
	msgId := internal.NewMessageId()
	encryptedFile, err := encryptChatMessage(msgId, txt, fname)
	if err != nil {
		return err
	}
	defer os.Remove(encryptedFile)

	p := v23.GetPrincipal(ctx)
	msg, err := internal.NewMessageFromFile(encryptedFile)
	if err != nil {
		return err
	}
	msg.Id = msgId
	msg.SenderBlessings, _ = p.BlessingStore().Default()
	msg.Lifespan = 15 * time.Minute
	var expiry time.Time
	if msg.SenderDischarges, expiry = lsec.PrepareDischarges(ctx, msg.SenderBlessings, nil, "", nil); !expiry.IsZero() {
		if d := expiry.Sub(time.Now()); d < msg.Lifespan {
			msg.Lifespan = d
		}
	}
	msg.Signature, err = p.Sign(msg.Hash())
	if err != nil {
		return err
	}
	w, err := store.OpenWrite(ctx, msg, 0)
	if err != nil {
		return err
	}
	in, err := os.Open(encryptedFile)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, in); err != nil {
		return err
	}
	if err := in.Close(); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	ctx.Infof("New message id %s stored", msg.Id)
	ps.Pub(msg)
	return nil
}

func encryptChatMessage(id, text, attachment string) (string, error) {
	tmpfile, err := ioutil.TempFile("", "vmsg-encrypt-")
	if err != nil {
		return "", err
	}
	defer tmpfile.Close()
	enc, err := aesEncoder(encryptionKey, tmpfile)
	if err != nil {
		return "", err
	}
	w := multipart.NewWriter(enc)
	if err := w.SetBoundary(id); err != nil {
		return "", err
	}

	// Write text field.
	if err := w.WriteField("text", text); err != nil {
		return "", err
	}
	// Write attachment, if provided.
	if attachment != "" {
		aw, err := w.CreateFormFile("attachment", filepath.Base(attachment))
		if err != nil {
			return "", err
		}
		ar, err := os.Open(attachment)
		if err != nil {
			return "", err
		}
		defer ar.Close()
		if _, err := io.Copy(aw, ar); err != nil {
			return "", err
		}
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	return tmpfile.Name(), nil
}

func decryptChatMessage(id string, msgReader io.Reader, dir string) (text, filename string, err error) {
	dec, err := aesDecoder(encryptionKey, msgReader)
	if err != nil {
		return "", "", err
	}
	r := multipart.NewReader(dec, id)
	form, err := r.ReadForm(1 << 20)
	if err != nil {
		return "", "", err
	}
	defer form.RemoveAll()
	if t := form.Value["text"]; len(t) == 1 {
		text = t[0]
	}
	if a := form.File["attachment"]; len(a) == 1 {
		fh := a[0]
		in, err := fh.Open()
		if err != nil {
			return "", "", err
		}
		defer in.Close()
		filename = filepath.Join(dir, filepath.Base(fh.Filename))
		out, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return "", "", err
		}
		defer out.Close()
		if _, err := io.Copy(out, in); err != nil {
			return "", "", err
		}
	}
	return text, filename, nil
}
