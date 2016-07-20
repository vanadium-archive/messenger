// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal

import (
	"crypto/sha256"
	"io"
	"os"
	"time"

	"github.com/pborman/uuid"

	"messenger/ifc"
)

// NewMessageFromFile creates a new Message for the content of the given file.
// The Message's Id, CreationTime, Length, and Sha256 fields are populated.
func NewMessageFromFile(file string) (ifc.Message, error) {
	f, err := os.Open(file)
	if err != nil {
		return ifc.Message{}, err
	}
	defer f.Close()
	h := sha256.New()
	size := int64(0)
	buf := make([]byte, 2048)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			size += int64(n)
			h.Write(buf[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return ifc.Message{}, err
		}
	}
	return ifc.Message{
		Id:           uuid.New(),
		CreationTime: time.Now(),
		Length:       size,
		Sha256:       h.Sum(nil),
	}, nil
}

// NewMessageFromBytes creates a new Message for the given content.
// The Message's Id, CreationTime, Length, and Sha256 fields are populated.
func NewMessageFromBytes(b []byte) ifc.Message {
	h := sha256.New()
	h.Write(b)
	return ifc.Message{
		Id:           uuid.New(),
		CreationTime: time.Now(),
		Length:       int64(len(b)),
		Sha256:       h.Sum(nil),
	}
}
