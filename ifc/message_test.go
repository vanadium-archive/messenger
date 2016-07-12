// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ifc_test

import (
	"encoding/hex"
	"testing"
	"time"

	"messenger/ifc"
)

func TestMessageHash(t *testing.T) {
	m := ifc.Message{}

	checkHash := func(expected string) {
		if got := hex.EncodeToString(m.Hash()); got != expected {
			t.Errorf("Unexpected hash. Got %q, expected %q", got, expected)
		}
	}

	checkHash("003e4139dde702ef8d664e1921c98a406dcb8bfa23e87de1403661f2366e8a79")

	m.Id = "foo"
	checkHash("9b7ca2206b221fd9f346e6597918001a927700a0a3df8b6566b3a363e8807e2b")

	m.Recipient = "bar"
	checkHash("13ded22d120212ada23409696ba95cf1325f2430a5dee1606a03647f61418cce")

	m.CreationTime = time.Date(2016, 6, 22, 13, 40, 12, 123, time.UTC)
	checkHash("00b86afc7142efd11aff7af1b913f529c9b02b2c720b5444f009b4953d457eec")
	m.CreationTime = time.Date(2016, 6, 22, 13, 40, 12, 124, time.UTC)
	checkHash("310e3b9044400d73f6e47c6b36f7445e62d6d7a8cca111b8eb193693ef8f4a5e")

	m.Lifespan = 24 * time.Hour
	checkHash("08a50c0c0a7b105d32959f11f61cb1e3442dcc0ce264fc86ea1fe7b0d53f5712")

	m.Length = 12345678
	checkHash("d78407ef1c30a7d061d9f572144dce6908d2d0162ea921d55ea905e84adbb83e")

	m.Sha256 = []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
	checkHash("84603388af27fdd2f11d936f194affb99847f52aa7837d48336ff5af888af229")
}
