// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"io"
)

func aesEncoder(key string, w io.Writer) (io.Writer, error) {
	block, err := aes.NewCipher(sha256hash(key))
	if err != nil {
		return nil, err
	}
	var iv [aes.BlockSize]byte
	if _, err := io.ReadFull(rand.Reader, iv[:]); err != nil {
		return nil, err
	}
	stream := cipher.NewOFB(block, iv[:])
	if _, err := w.Write(iv[:]); err != nil {
		return nil, err
	}
	return &cipher.StreamWriter{S: stream, W: w}, nil
}

func aesDecoder(key string, r io.Reader) (io.Reader, error) {
	block, err := aes.NewCipher(sha256hash(key))
	if err != nil {
		return nil, err
	}
	var iv [aes.BlockSize]byte
	if _, err := io.ReadFull(r, iv[:]); err != nil {
		return nil, err
	}
	stream := cipher.NewOFB(block, iv[:])
	return &cipher.StreamReader{S: stream, R: r}, nil
}

func sha256hash(s string) []byte {
	h := sha256.New()
	h.Write([]byte(s))
	return h.Sum(nil)
}
