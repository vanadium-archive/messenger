// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal

import (
	"io"

	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"

	"messenger/ifc"
)

// messenger implements the Messenger and MessageRepository interfaces.
type Messenger struct {
	Params   Params
	Notifier *PubSub
	Counters *Counters
}

func (m *Messenger) Diff(ctx *context.T, call ifc.MessengerDiffServerCall) error {
	ctx.Infof("Diff() called")
	if err := m.Params.RateAclIn.Authorize(ctx, call.Security()); err != nil {
		return err
	}
	for call.RecvStream().Advance() {
		ids := call.RecvStream().Value()
		responses := make([]bool, len(ids))
		for i, id := range ids {
			responses[i] = m.Params.Store.Exists(ctx, id)
		}
		if err := call.SendStream().Send(responses); err != nil {
			return err
		}
	}
	return nil
}

func (m *Messenger) Push(ctx *context.T, call ifc.MessengerPushServerCall, msg ifc.Message, offset int64) error {
	b, r := security.RemoteBlessingNames(ctx, call.Security())

	var cp security.CallParams
	cp.Copy(call.Security())
	cp.RemoteBlessings = msg.SenderBlessings
	cp.RemoteDischarges = msg.SenderDischarges
	senderCall := security.NewCall(&cp)

	sb, sr := security.RemoteBlessingNames(ctx, senderCall)
	ctx.Infof("Push() Caller Blessing Names %q, rejected %q.", b, r)
	ctx.Infof("       Sender Blessing Names %q, rejected %q.", sb, sr)

	if err := m.Params.RateAclIn.Authorize(ctx, call.Security()); err != nil {
		return err
	}
	if err := m.Params.RateAclSender.Authorize(ctx, senderCall); err != nil {
		return err
	}

	if err := msg.Validate(ctx); err != nil {
		return err
	}

	if m.Params.MaxHops > 0 && len(msg.Hops) > m.Params.MaxHops {
		return ifc.NewErrTooManyHops(ctx, int32(m.Params.MaxHops))
	}

	if m.Params.MaxMessageLength > 0 && msg.Length > m.Params.MaxMessageLength {
		return ifc.NewErrTooBig(ctx, m.Params.MaxMessageLength)
	}

	w, err := m.Params.Store.OpenWrite(ctx, msg, offset)
	if err != nil {
		return err
	}

	size := int64(0)
	for call.RecvStream().Advance() {
		packet := call.RecvStream().Value()
		if _, err := w.Write(packet); err != nil {
			w.Close()
			return err
		}
		size += int64(len(packet))
		m.Counters.numBytesReceived.Incr(int64(len(packet)))
	}
	if err := call.RecvStream().Err(); err != nil && size != msg.Length {
		// The stream was interrupted. We should keep the data that was
		// transferred to allow the transfer to be resumed later.
		w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	m.Counters.numMessagesReceived.Incr(1)
	if m.Notifier != nil {
		m.Notifier.Pub(msg)
	}
	return nil
}

func (m *Messenger) ResumeOffset(ctx *context.T, call rpc.ServerCall, msg ifc.Message) (offset int64, err error) {
	ctx.Infof("ResumeOffset() called")
	if err := m.Params.RateAclIn.Authorize(ctx, call.Security()); err != nil {
		return 0, err
	}
	return m.Params.Store.Offset(ctx, msg.Id)
}

func (m *Messenger) Manifest(ctx *context.T, call ifc.MessageRepositoryManifestServerCall) error {
	ctx.Infof("Manifest() called")
	if err := m.Params.RateAclIn.Authorize(ctx, call.Security()); err != nil {
		return err
	}
	ch, err := m.Params.Store.Manifest(ctx)
	if err != nil {
		return err
	}
	for msg := range ch {
		call.SendStream().Send(*msg)
	}
	return nil
}

func (m *Messenger) Pull(ctx *context.T, call ifc.MessageRepositoryPullServerCall, id string, offset int64) (ifc.Message, error) {
	ctx.Infof("Pull() called")
	if err := m.Params.RateAclIn.Authorize(ctx, call.Security()); err != nil {
		return ifc.Message{}, err
	}
	msg, r, err := m.Params.Store.OpenRead(ctx, id)
	if err != nil {
		return ifc.Message{}, err
	}
	if _, err := r.Seek(offset, 0); err != nil {
		return ifc.Message{}, err
	}
	buf := make([]byte, 2048)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if err := call.SendStream().Send(buf[:n]); err != nil {
				return ifc.Message{}, err
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return ifc.Message{}, err
		}
	}
	return msg, nil
}
