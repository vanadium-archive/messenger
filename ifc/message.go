// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ifc

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"time"

	"v.io/v23/context"
)

// Hash returns the sha256 hash of the Message.
func (msg *Message) Hash() []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%s\n", msg.Id)
	fmt.Fprintf(&buf, "%s\n", msg.Recipient)
	fmt.Fprintf(&buf, "%s\n", msg.CreationTime.UTC().Format("20060102150405.999999999"))
	fmt.Fprintf(&buf, "%s\n", msg.Lifespan.Nanoseconds())
	fmt.Fprintf(&buf, "%s\n", msg.Length)
	buf.Write(msg.Sha256)
	h := sha256.Sum256(buf.Bytes())
	return h[:]
}

// Validate verifies that the Message is not expired and that its Signature is
// valid.
func (msg *Message) Validate(ctx *context.T) error {
	if !msg.Signature.Verify(msg.SenderBlessings.PublicKey(), msg.Hash()) {
		return NewErrInvalidSignature(ctx)
	}
	if err := msg.Expired(ctx); err != nil {
		return err
	}
	return nil
}

// Expired verifies that the message is not expired.
func (msg *Message) Expired(ctx *context.T) error {
	if now := time.Now(); msg.CreationTime.Add(msg.Lifespan).Before(now) {
		return NewErrExpired(ctx, msg.CreationTime, msg.Lifespan, now)
	}
	return nil
}
