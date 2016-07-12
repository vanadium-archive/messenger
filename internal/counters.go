// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal

import (
	"v.io/v23/naming"

	"v.io/x/ref/lib/stats"
	"v.io/x/ref/lib/stats/counter"
)

func NewCounters(id string) *Counters {
	base := naming.Join("messenger", id)
	return &Counters{
		numPeers:        stats.NewCounter(naming.Join(base, "num-peers")),
		numMessagesSent: stats.NewCounter(naming.Join(base, "num-peer-messages-sent")),
		numBytesSent:    stats.NewCounter(naming.Join(base, "num-peer-bytes-sent")),
	}
}

type Counters struct {
	numPeers        *counter.Counter
	numMessagesSent *counter.Counter
	numBytesSent    *counter.Counter
}
