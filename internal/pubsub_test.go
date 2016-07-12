// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal

import (
	"sync"
	"testing"

	"v.io/x/ref/test"

	"messenger/ifc"
)

func TestPubSub(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	ps := newPubSub(ctx)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(ch <-chan *ifc.Message) {
			<-ch
			wg.Done()
		}(ps.Sub())
	}

	ps.Pub(ifc.Message{})
	wg.Wait()
}
