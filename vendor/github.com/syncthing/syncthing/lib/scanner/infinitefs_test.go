// Copyright (C) 2017 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

package scanner

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/syncthing/syncthing/lib/fs"
)

type infiniteFS struct {
	fs.Filesystem
	width    int   // number of files and directories per level
	depth    int   // number of tree levels to simulate
	filesize int64 // size of each file in bytes
}

var errNotSupp = errors.New("not supported")

func (i infiniteFS) Lstat(name string) (fs.FileInfo, error) {
	return fakeInfo{name, i.filesize}, nil
}

func (i infiniteFS) DirNames(name string) ([]string, error) {
	// Returns a list of fake files and directories. Names are such that
	// files appear before directories - this makes it so the scanner will
	// actually see a few files without having to reach the max depth.
	var names []string
	for j := 0; j < i.width; j++ {
		names = append(names, fmt.Sprintf("aa-file-%d", j))
	}
	if len(strings.Split(name, string(os.PathSeparator))) < i.depth {
		for j := 0; j < i.width; j++ {
			names = append(names, fmt.Sprintf("zz-dir-%d", j))
		}
	}
	return names, nil
}

func (i infiniteFS) Open(name string) (fs.File, error) {
	return &fakeFile{name, i.filesize, 0}, nil
}

type fakeInfo struct {
	name string
	size int64
}

func (f fakeInfo) Name() string       { return f.name }
func (f fakeInfo) Mode() fs.FileMode  { return 0755 }
func (f fakeInfo) Size() int64        { return f.size }
func (f fakeInfo) ModTime() time.Time { return time.Unix(1234567890, 0) }
func (f fakeInfo) IsDir() bool        { return strings.Contains(filepath.Base(f.name), "dir") || f.name == "." }
func (f fakeInfo) IsRegular() bool    { return !f.IsDir() }
func (f fakeInfo) IsSymlink() bool    { return false }

type fakeFile struct {
	name       string
	size       int64
	readOffset int64
}

func (f *fakeFile) Name() string {
	return f.name
}

func (f *fakeFile) Read(bs []byte) (int, error) {
	remaining := f.size - f.readOffset
	if remaining == 0 {
		return 0, io.EOF
	}
	if remaining < int64(len(bs)) {
		f.readOffset = f.size
		return int(remaining), nil
	}
	f.readOffset += int64(len(bs))
	return len(bs), nil
}

func (f *fakeFile) Stat() (fs.FileInfo, error) {
	return fakeInfo{f.name, f.size}, nil
}

func (f *fakeFile) Write([]byte) (int, error)          { return 0, errNotSupp }
func (f *fakeFile) WriteAt([]byte, int64) (int, error) { return 0, errNotSupp }
func (f *fakeFile) Close() error                       { return nil }
func (f *fakeFile) Truncate(size int64) error          { return errNotSupp }
func (f *fakeFile) ReadAt([]byte, int64) (int, error)  { return 0, errNotSupp }
func (f *fakeFile) Seek(int64, int) (int64, error)     { return 0, errNotSupp }
func (f *fakeFile) Sync() error                        { return nil }
