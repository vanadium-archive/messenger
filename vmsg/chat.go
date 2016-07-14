// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run $JIRI_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go . -help

package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/kr/text"
	"github.com/nlacasse/gocui"

	"v.io/v23"
	"v.io/v23/context"

	"v.io/x/lib/cmdline"
	lsec "v.io/x/ref/lib/security"
	"v.io/x/ref/lib/v23cmd"

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

	g := gocui.NewGui()
	if err := g.Init(); err != nil {
		return err
	}
	defer g.Close()
	g.ShowCursor = true
	g.SetLayout(func(g *gocui.Gui) error {
		maxX, maxY := g.Size()
		messageInputViewHeight := 3
		if _, err := g.SetView("history", -1, -1, maxX, maxY-messageInputViewHeight); err != nil {
			if err != gocui.ErrorUnkView {
				return err
			}
		}
		if messageInputView, err := g.SetView("messageInput", -1, maxY-messageInputViewHeight, maxX, maxY-1); err != nil {
			if err != gocui.ErrorUnkView {
				return err
			}
			messageInputView.Editable = true
		}
		if err := g.SetCurrentView("messageInput"); err != nil {
			return err
		}
		return nil
	})
	g.Flush()

	historyView, err := g.View("history")
	if err != nil {
		return err
	}

	scrollDown := func() {
		_, height := historyView.Size()
		numLines := historyView.NumberOfLines()
		if numLines > height {
			historyView.SetOrigin(0, numLines-height)
		}
		g.Flush()
	}

	help := func() {
		historyView.Write([]byte("*** Welcome to Vanadium Peer to Peer Chat ***"))
		historyView.Write([]byte("***"))
		historyView.Write([]byte(color.RedString("*** This is a demo application.")))
		historyView.Write([]byte("***"))
		if encryptionKey == defaultEncryptionKey {
			historyView.Write([]byte(color.RedString("*** Messages are encrypted with the default key. They are NOT private.")))
		}
		historyView.Write([]byte("***"))
		historyView.Write([]byte("*** Messages are stored and relayed peer-to-peer for 15 minutes after they are"))
		historyView.Write([]byte("*** created. New peers will see up to 15 minutes of history when they join."))
		historyView.Write([]byte("***"))
		historyView.Write([]byte("*** Available commands are:"))
		historyView.Write([]byte("***   /help to see this help message"))
		historyView.Write([]byte("***   /ping to send a ping"))
		historyView.Write([]byte("***   /quit to exit"))
		historyView.Write([]byte("***   /send <filename> to send a file"))
		historyView.Write([]byte("***"))
		historyView.Write([]byte("*** Type /quit or press Ctrl-C to exit."))
		historyView.Write([]byte("***"))
		scrollDown()
	}

	if err := g.SetKeybinding("", gocui.KeyCtrlC, 0,
		func(g *gocui.Gui, v *gocui.View) error {
			return gocui.Quit
		},
	); err != nil {
		return err
	}
	if err := g.SetKeybinding("messageInput", gocui.KeyEnter, 0,
		func(g *gocui.Gui, v *gocui.View) error {
			defer scrollDown()
			mtxt := strings.TrimSpace(v.Buffer())
			v.Clear()
			if mtxt == "" {
				return nil
			}
			fname := ""
			switch {
			case mtxt == "/clear":
				historyView.Clear()
				historyView.SetOrigin(0, 0)
				return nil
			case mtxt == "/help":
				help()
				return nil
			case mtxt == "/ping":
				mtxt = fmt.Sprintf("\x01PING %d", time.Now().UnixNano())
			case mtxt == "/quit":
				return gocui.Quit
			case strings.HasPrefix(mtxt, "/send"):
				fname = strings.TrimSpace(mtxt[5:])
				mtxt = ""
			case strings.HasPrefix(mtxt, "/"):
				fmt.Fprintf(historyView, "### Unknown command %s", mtxt)
				return nil
			}
			if err := sendMessage(ctx, node.PubSub(), params.Store, mtxt, fname); err != nil {
				fmt.Fprintf(historyView, "## sendMessage failed: %v\n", err)
			}
			return nil
		},
	); err != nil {
		return err
	}

	help()

	go func() {
		for msg := range node.PubSub().Sub() {
			_, r, err := params.Store.OpenRead(ctx, msg.Id)
			if err != nil {
				continue
			}
			msgText, filename, err := decryptChatMessage(msg.Id, r, incomingDir)
			r.Close()
			if err != nil {
				fmt.Fprintf(historyView, "## decryptChatMessage failed: %v\n", err)
				g.Flush()
				continue
			}

			delta := time.Since(msg.CreationTime).Seconds()
			hops := len(msg.Hops)
			var buf bytes.Buffer
			fmt.Fprintf(&buf, "%s %2d %5.2fs ", msg.CreationTime.Local().Format("15:04:05"), hops, delta)
			if msgText != "" {
				if strings.HasPrefix(msgText, "\x01PING") {
					fmt.Fprintf(&buf, "PING from %s", msg.SenderBlessings)
					reply := "\x01PONG" + msgText[5:]
					if err := sendMessage(ctx, node.PubSub(), params.Store, reply, ""); err != nil {
						ctx.Errorf("sendMessage failed: %v", err)
					}
				} else if strings.HasPrefix(msgText, "\x01PONG ") {
					if i, err := strconv.ParseInt(msgText[6:], 10, 64); err == nil {
						t := time.Unix(0, i)
						fmt.Fprintf(&buf, "PING reply from %s: %s", msg.SenderBlessings, time.Since(t))
					}
				} else {
					fmt.Fprintf(&buf, "<%s> %s", msg.SenderBlessings, msgText)
				}
			}
			if filename != "" {
				fmt.Fprintf(&buf, "Received file from %s: %s", msg.SenderBlessings, filename)
			}

			width, _ := historyView.Size()
			historyView.Write(text.WrapBytes(buf.Bytes(), width))
			scrollDown()
		}
	}()

	if err := g.MainLoop(); err != nil && err != gocui.Quit {
		return err
	}
	return nil
}

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
