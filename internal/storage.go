// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"hash"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"

	"v.io/v23/context"
	"v.io/v23/verror"
	"v.io/v23/vom"

	"messenger/ifc"
)

type MessengerStorage interface {
	// OpenWrite is used to store a message that doesn't already exist,
	// or to resume storing a message that is imcomplete.
	// A message cannot be opened for writing concurrently.
	OpenWrite(ctx *context.T, msg ifc.Message, offset int64) (io.WriteCloser, error)
	// OpenRead is used to read a message and its content. The message has
	// to be complete before it can be read.
	// A message cannot be opened for reading until it was completely
	// written and its content has been verified.
	OpenRead(ctx *context.T, id string) (ifc.Message, ReadSeekCloser, error)
	// Offset returns the offset from which the Write operation can be
	// resumed. If the message is complete, it returns an error.
	Offset(ctx *context.T, id string) (int64, error)
	// Manifest returns a channel on which all the stored messages can be
	// read (not their content).
	Manifest(ctx *context.T) (<-chan *ifc.Message, error)
	// PeerInfo returns the message delivery information for the given
	// message and peer.
	PeerInfo(ctx *context.T, id, peer string) PeerInfo
	// SetPeerInfo records the message delivery information for the given
	// message and peer.
	SetPeerInfo(ctx *context.T, id, peer string, pi PeerInfo) error
}

type ReadSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}

const (
	messageFileName = "message"
	contentFileName = "content"
	doneFileName    = "done"
)

func NewFileStorage(ctx *context.T, dir string) *FileStorage {
	fs := &FileStorage{dir: dir}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Minute):
			}
			d, err := os.Open(dir)
			if err != nil {
				continue
			}
			for {
				names, err := d.Readdirnames(10)
				for _, n := range names {
					b, err := base64.URLEncoding.DecodeString(n)
					if err != nil {
						continue
					}
					id := string(b)
					msg, err := fs.readMessage(id)
					if err != nil {
						continue
					}
					if err := msg.Expired(ctx); err != nil {
						d := filepath.Join(dir, n)
						if err := os.RemoveAll(d); err != nil {
							ctx.Errorf("os.RemoveAll(%q) failed: %v", d, err)
						}
					}
				}
				if err != nil {
					break
				}
			}
			d.Close()
		}
	}()
	return fs
}

// FileStorage implements the MessengerStorage interface using a directory on
// the local filesystem.
type FileStorage struct {
	dir  string
	mu   sync.Mutex
	busy map[string]bool
}

func (s *FileStorage) OpenWrite(ctx *context.T, msg ifc.Message, offset int64) (io.WriteCloser, error) {
	if err := s.lock(ctx, msg.Id); err != nil {
		return nil, err
	}
	if s.isComplete(msg.Id) {
		s.unlock(msg.Id)
		return nil, ifc.NewErrAlreadySeen(ctx, msg.Id)
	}

	// If the message was already written, make sure that it is indeed the
	// same message.
	if m, err := s.readMessage(msg.Id); err == nil {
		if !bytes.Equal(m.Hash(), msg.Hash()) {
			// Message ID collision.
			s.unlock(msg.Id)
			return nil, ifc.NewErrMessageIdCollision(ctx, msg.Id)
		}
	} else if err := s.writeMessage(msg); err != nil {
		s.unlock(msg.Id)
		return nil, err
	}

	f, err := os.OpenFile(filepath.Join(s.messageDir(msg.Id), contentFileName), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		s.unlock(msg.Id)
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		s.unlock(msg.Id)
		return nil, err
	}
	if fi.Size() != offset {
		f.Close()
		s.unlock(msg.Id)
		return nil, ifc.NewErrIncorrectOffset(ctx, offset, fi.Size())
	}
	h := sha256.New()
	buf := make([]byte, 2048)
	for {
		n, err := f.Read(buf)
		h.Write(buf[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			f.Close()
			s.unlock(msg.Id)
			return nil, err
		}
	}
	f.Seek(offset, 0)
	return &fileWrapper{ctx, s, f, h, msg}, nil
}

type fileWrapper struct {
	ctx *context.T
	s   *FileStorage
	f   *os.File
	h   hash.Hash
	msg ifc.Message
}

func (w *fileWrapper) Write(p []byte) (n int, err error) {
	w.h.Write(p)
	return w.f.Write(p)
}

func (w *fileWrapper) Close() error {
	defer w.s.unlock(w.msg.Id)
	if err := w.f.Sync(); err != nil {
		return err
	}
	fi, err := w.f.Stat()
	if err != nil {
		return err
	}
	if err := w.f.Close(); err != nil {
		return err
	}
	if fi.Size() >= w.msg.Length {
		if fi.Size() > w.msg.Length || !bytes.Equal(w.msg.Sha256, w.h.Sum(nil)) {
			os.RemoveAll(w.s.messageDir(w.msg.Id))
			return ifc.NewErrContentMismatch(w.ctx)
		}
		return ioutil.WriteFile(filepath.Join(w.s.messageDir(w.msg.Id), doneFileName), []byte("done"), 0600)
	}
	return nil
}

func (s *FileStorage) OpenRead(ctx *context.T, id string) (ifc.Message, ReadSeekCloser, error) {
	if !s.isComplete(id) {
		return ifc.Message{}, nil, verror.New(verror.ErrNoExist, ctx, id)
	}
	msg, err := s.readMessage(id)
	if err != nil {
		return ifc.Message{}, nil, err
	}
	f, err := os.OpenFile(filepath.Join(s.messageDir(msg.Id), contentFileName), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return ifc.Message{}, nil, err
	}
	return msg, f, nil
}

func (s *FileStorage) Offset(ctx *context.T, id string) (int64, error) {
	if err := s.lock(ctx, id); err != nil {
		return 0, err
	}
	defer s.unlock(id)
	if s.isComplete(id) {
		return 0, ifc.NewErrAlreadySeen(ctx, id)
	}

	fi, err := os.Stat(filepath.Join(s.messageDir(id), contentFileName))
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

func (s *FileStorage) Manifest(ctx *context.T) (<-chan *ifc.Message, error) {
	d, err := os.Open(s.dir)
	if os.IsNotExist(err) {
		ch := make(chan *ifc.Message)
		close(ch)
		return ch, nil
	}
	if err != nil {
		return nil, err
	}
	ch := make(chan *ifc.Message)
	go func() {
		defer d.Close()
		defer close(ch)
		for {
			names, err := d.Readdirnames(100)
			for _, n := range names {
				b, err := base64.URLEncoding.DecodeString(n)
				if err != nil {
					continue
				}
				id := string(b)
				if !s.isComplete(id) {
					continue
				}
				msg, err := s.readMessage(id)
				if err != nil {
					continue
				}
				if err := msg.Expired(ctx); err != nil {
					continue
				}
				select {
				case ch <- &msg:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	return ch, nil
}

func (s *FileStorage) SetPeerInfo(ctx *context.T, id, peer string, pi PeerInfo) error {
	f, err := os.OpenFile(s.peerFile(id, peer), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	return vom.NewEncoder(f).Encode(pi)
}

func (s *FileStorage) PeerInfo(ctx *context.T, id, peer string) (pi PeerInfo) {
	f, err := os.Open(s.peerFile(id, peer))
	if err != nil {
		return
	}
	vom.NewDecoder(f).Decode(&pi)
	return
}

func (s *FileStorage) messageDir(id string) string {
	return filepath.Join(s.dir, base64.URLEncoding.EncodeToString([]byte(id)))
}

func (s *FileStorage) peerFile(id, peer string) string {
	return filepath.Join(s.messageDir(id), "peer-"+base64.URLEncoding.EncodeToString([]byte(peer)))
}

func (s *FileStorage) lock(ctx *context.T, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy == nil {
		s.busy = make(map[string]bool)
	}
	if s.busy[id] {
		return ifc.NewErrBusy(ctx, id)
	}
	s.busy[id] = true
	return nil
}

func (s *FileStorage) unlock(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy == nil {
		return
	}
	delete(s.busy, id)
}

func (s *FileStorage) isComplete(id string) bool {
	_, err := os.Stat(filepath.Join(s.messageDir(id), doneFileName))
	return err == nil
}

func (s *FileStorage) readMessage(id string) (msg ifc.Message, err error) {
	var f *os.File
	if f, err = os.Open(filepath.Join(s.messageDir(id), messageFileName)); err != nil {
		return
	}
	defer f.Close()
	err = vom.NewDecoder(f).Decode(&msg)
	return
}

func (s *FileStorage) writeMessage(msg ifc.Message) error {
	msgDir := s.messageDir(msg.Id)
	if err := os.MkdirAll(msgDir, 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(msgDir, messageFileName), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	return vom.NewEncoder(f).Encode(msg)
}
