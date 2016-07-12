// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal_test

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"v.io/x/ref/test"

	"messenger/internal"
)

func TestFileStorage(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	tmpdir, err := ioutil.TempDir("", "TestFileStorage-")
	if err != nil {
		t.Fatalf("ioutil.TempDir failed: %v", err)
	}
	defer os.RemoveAll(tmpdir)

	msgTxt := []byte("Hello World!")
	msg := internal.NewMessageFromBytes(msgTxt)

	fs := internal.NewFileStorage(ctx, tmpdir)

	// Open, Write whole content, Close
	w, err := fs.OpenWrite(ctx, msg, 0)
	if err != nil {
		t.Fatalf("fs.OpenWrite failed: %v", err)
	}
	if _, err := fs.OpenWrite(ctx, msg, 0); err == nil {
		t.Errorf("Unexpected concurrent fs.OpenWrite success")
	}
	if _, err := w.Write(msgTxt); err != nil {
		t.Fatalf("w.Write failed: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("w.Close failed: %v", err)
	}

	// Re-opening the file for write failed after the file is complete.
	if _, err := fs.OpenWrite(ctx, msg, 0); err == nil {
		t.Errorf("Unexpected fs.OpenWrite success after complete write")
	}
	if _, err := fs.Offset(ctx, msg.Id); err == nil {
		t.Errorf("Unexpected fs.Offset success after complete write")
	}

	// Open, Read whole content, Close
	m, r, err := fs.OpenRead(ctx, msg.Id)
	if err != nil {
		t.Fatalf("fs.OpenRead failed: %v", err)
	}
	if !bytes.Equal(m.Hash(), msg.Hash()) {
		t.Errorf("Unexpected message hash. Got %v, expected %v", m.Hash(), msg.Hash())
	}
	b, err := ioutil.ReadAll(r)
	if err != nil {
		t.Fatalf("ioutil.ReadAll failed: %v", err)
	}
	if !bytes.Equal(b, msgTxt) {
		t.Errorf("Unexpected content. Got %v, expected %v", b, msgTxt)
	}

	pi := internal.PeerInfo{
		Sent:        false,
		NumAttempts: 1,
		NextAttempt: time.Now().UTC(),
	}
	if err := fs.SetPeerInfo(ctx, msg.Id, "foo", pi); err != nil {
		t.Errorf("fs.SetPeerInfo failed: %v", err)
	}
	pi2 := fs.PeerInfo(ctx, msg.Id, "foo")
	if !reflect.DeepEqual(pi, pi2) {
		t.Errorf("Unexpected PeerInfo. Got %v, expected %v", pi2, pi)
	}
}

func TestFileStorageResume(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	tmpdir, err := ioutil.TempDir("", "TestFileStorageResume-")
	if err != nil {
		t.Fatalf("ioutil.TempDir failed: %v", err)
	}
	defer os.RemoveAll(tmpdir)

	msgTxt := []byte("Hello World\n")
	msg := internal.NewMessageFromBytes(msgTxt)

	fs := internal.NewFileStorage(ctx, tmpdir)

	// Open, Write 5 bytes, Close
	w, err := fs.OpenWrite(ctx, msg, 0)
	if err != nil {
		t.Fatalf("fs.OpenWrite failed: %v", err)
	}
	if _, err := w.Write(msgTxt[:5]); err != nil {
		t.Fatalf("w.Write failed: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("w.Close failed: %v", err)
	}
	if offset, err := fs.Offset(ctx, msg.Id); err != nil {
		t.Errorf("fs.Offset failed: %v", err)
	} else if expected := int64(5); offset != expected {
		t.Errorf("unexpected offset. Got %d, expected %d", offset, expected)
	}

	// Open, Write remaining bytes, Close
	if w, err = fs.OpenWrite(ctx, msg, 5); err != nil {
		t.Fatalf("fs.OpenWrite failed: %v", err)
	}
	if _, err := w.Write(msgTxt[5:]); err != nil {
		t.Fatalf("w.Write failed: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("w.Close failed: %v", err)
	}

	if _, err := fs.OpenWrite(ctx, msg, 0); err == nil {
		t.Errorf("Unexpected fs.OpenWrite success after complete write")
	}
	if _, err := fs.Offset(ctx, msg.Id); err == nil {
		t.Errorf("Unexpected fs.Offset success after complete write")
	}

	// Open, Read 8 bytes, Close
	_, r, err := fs.OpenRead(ctx, msg.Id)
	if err != nil {
		t.Fatalf("fs.OpenRead failed: %v", err)
	}
	b8 := make([]byte, 8)
	if n, err := r.Read(b8); err != nil || n != 8 {
		t.Errorf("Unexpected read. Got (%d, %v), expected (8, nil)", n, err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("r.Close failed: %v", err)
	}

	// Open, Read remaining bytes, Close
	if _, r, err = fs.OpenRead(ctx, msg.Id); err != nil {
		t.Fatalf("fs.OpenRead failed: %v", err)
	}
	r.Seek(8, 0)
	b, err := ioutil.ReadAll(r)
	if err != nil {
		t.Fatalf("ioutil.ReadAll failed: %v", err)
	}
	concat := append(b8, b...)
	if !bytes.Equal(concat, msgTxt) {
		t.Errorf("Unexpected content. Got %v, expected %v", concat, msgTxt)
	}
}

func TestFileStorageManifest(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	tmpdir, err := ioutil.TempDir("", "TestFileStorageManifest-")
	if err != nil {
		t.Fatalf("ioutil.TempDir failed: %v", err)
	}
	defer os.RemoveAll(tmpdir)

	fs := internal.NewFileStorage(ctx, tmpdir)

	const N = 200
	// Create N messages.
	for i := 0; i < N; i++ {
		name := fmt.Sprintf("file-%03d", i)
		content := strings.Repeat(name, i)
		msg := internal.NewMessageFromBytes([]byte(content))
		msg.Id = name
		msg.Lifespan = time.Hour
		// Open, Write whole content, Close
		w, err := fs.OpenWrite(ctx, msg, 0)
		if err != nil {
			t.Fatalf("fs.OpenWrite failed: %v", err)
		}
		if _, err := fs.OpenWrite(ctx, msg, 0); err == nil {
			t.Errorf("Unexpected concurrent fs.OpenWrite success")
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("w.Write failed: %v", err)
		}
		if err := w.Close(); err != nil {
			t.Fatalf("w.Close failed: %v", err)
		}
	}

	ch, err := fs.Manifest(ctx)
	if err != nil {
		t.Fatalf("fs.Manifest failed: %v", err)
	}
	count := 0
	for range ch {
		count++
	}
	if expected := N; count != expected {
		t.Errorf("unexpected number of messages. Got %d, expected %d", count, expected)
	}
}
