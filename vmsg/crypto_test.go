// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"testing"
)

func TestCrypto(t *testing.T) {
	var buf bytes.Buffer
	key := "hello world!"
	txt := "This is my encrypted message"

	enc, err := aesEncoder(key, &buf)
	if err != nil {
		t.Fatalf("aesEncoder failed: %v", err)
	}
	fmt.Fprint(enc, txt)

	dec, err := aesDecoder(key, &buf)
	if err != nil {
		t.Fatalf("aesDecoder failed: %v", err)
	}
	b, err := ioutil.ReadAll(dec)
	if err != nil {
		t.Fatalf("ioutil.ReadAll failed: %v", err)
	}

	if got := string(b); got != txt {
		t.Errorf("Unexpected message. Got %q, expected %q", got, txt)
	}
}
