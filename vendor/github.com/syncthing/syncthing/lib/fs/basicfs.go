// Copyright (C) 2016 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

package fs

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/calmh/du"
)

var (
	ErrInvalidFilename = errors.New("filename is invalid")
	ErrNotRelative     = errors.New("not a relative path")
)

// The BasicFilesystem implements all aspects by delegating to package os.
// All paths are relative to the root and cannot (should not) escape the root directory.
type BasicFilesystem struct {
	root string
}

func newBasicFilesystem(root string) *BasicFilesystem {
	// The reason it's done like this:
	// C:          ->  C:\            ->  C:\        (issue that this is trying to fix)
	// C:\somedir  ->  C:\somedir\    ->  C:\somedir
	// C:\somedir\ ->  C:\somedir\\   ->  C:\somedir
	// This way in the tests, we get away without OS specific separators
	// in the test configs.
	root = filepath.Dir(root + string(filepath.Separator))

	// Attempt tilde expansion; leave unchanged in case of error
	if path, err := ExpandTilde(root); err == nil {
		root = path
	}

	// Attempt absolutification; leave unchanged in case of error
	if !filepath.IsAbs(root) {
		// Abs() looks like a fairly expensive syscall on Windows, while
		// IsAbs() is a whole bunch of string mangling. I think IsAbs() may be
		// somewhat faster in the general case, hence the outer if...
		if path, err := filepath.Abs(root); err == nil {
			root = path
		}
	}

	// Attempt to enable long filename support on Windows. We may still not
	// have an absolute path here if the previous steps failed.
	if runtime.GOOS == "windows" {
		if filepath.IsAbs(root) && !strings.HasPrefix(root, `\\`) {
			root = `\\?\` + root
		}
		// If we're not on Windows, we want the path to end with a slash to
		// penetrate symlinks. On Windows, paths must not end with a slash.
	} else if root[len(root)-1] != filepath.Separator {
		root = root + string(filepath.Separator)
	}

	return &BasicFilesystem{
		root: root,
	}
}

// rooted expands the relative path to the full path that is then used with os
// package. If the relative path somehow causes the final path to escape the root
// directory, this returns an error, to prevent accessing files that are not in the
// shared directory.
func (f *BasicFilesystem) rooted(rel string) (string, error) {
	// The root must not be empty.
	if f.root == "" {
		return "", ErrInvalidFilename
	}

	pathSep := string(PathSeparator)

	// The expected prefix for the resulting path is the root, with a path
	// separator at the end.
	expectedPrefix := filepath.FromSlash(f.root)
	if !strings.HasSuffix(expectedPrefix, pathSep) {
		expectedPrefix += pathSep
	}

	// The relative path should be clean from internal dotdots and similar
	// funkyness.
	rel = filepath.FromSlash(rel)
	if filepath.Clean(rel) != rel {
		return "", ErrInvalidFilename
	}

	// It is not acceptable to attempt to traverse upwards.
	switch rel {
	case "..", pathSep:
		return "", ErrNotRelative
	}
	if strings.HasPrefix(rel, ".."+pathSep) {
		return "", ErrNotRelative
	}

	if strings.HasPrefix(rel, pathSep+pathSep) {
		// The relative path may pretend to be an absolute path within the
		// root, but the double path separator on Windows implies something
		// else. It would get cleaned by the Join below, but it's out of
		// spec anyway.
		return "", ErrNotRelative
	}

	// The supposedly correct path is the one filepath.Join will return, as
	// it does cleaning and so on. Check that one first to make sure no
	// obvious escape attempts have been made.
	joined := filepath.Join(f.root, rel)
	if rel == "." && !strings.HasSuffix(joined, pathSep) {
		joined += pathSep
	}
	if !strings.HasPrefix(joined, expectedPrefix) {
		return "", ErrNotRelative
	}

	return joined, nil
}

func (f *BasicFilesystem) unrooted(path string) string {
	return strings.TrimPrefix(strings.TrimPrefix(path, f.root), string(PathSeparator))
}

func (f *BasicFilesystem) Chmod(name string, mode FileMode) error {
	name, err := f.rooted(name)
	if err != nil {
		return err
	}
	return os.Chmod(name, os.FileMode(mode))
}

func (f *BasicFilesystem) Chtimes(name string, atime time.Time, mtime time.Time) error {
	name, err := f.rooted(name)
	if err != nil {
		return err
	}
	return os.Chtimes(name, atime, mtime)
}

func (f *BasicFilesystem) Mkdir(name string, perm FileMode) error {
	name, err := f.rooted(name)
	if err != nil {
		return err
	}
	return os.Mkdir(name, os.FileMode(perm))
}

func (f *BasicFilesystem) Lstat(name string) (FileInfo, error) {
	name, err := f.rooted(name)
	if err != nil {
		return nil, err
	}
	fi, err := underlyingLstat(name)
	if err != nil {
		return nil, err
	}
	return fsFileInfo{fi}, err
}

func (f *BasicFilesystem) Remove(name string) error {
	name, err := f.rooted(name)
	if err != nil {
		return err
	}
	return os.Remove(name)
}

func (f *BasicFilesystem) RemoveAll(name string) error {
	name, err := f.rooted(name)
	if err != nil {
		return err
	}
	return os.RemoveAll(name)
}

func (f *BasicFilesystem) Rename(oldpath, newpath string) error {
	oldpath, err := f.rooted(oldpath)
	if err != nil {
		return err
	}
	newpath, err = f.rooted(newpath)
	if err != nil {
		return err
	}
	return os.Rename(oldpath, newpath)
}

func (f *BasicFilesystem) Stat(name string) (FileInfo, error) {
	name, err := f.rooted(name)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(name)
	if err != nil {
		return nil, err
	}
	return fsFileInfo{fi}, err
}

func (f *BasicFilesystem) DirNames(name string) ([]string, error) {
	name, err := f.rooted(name)
	if err != nil {
		return nil, err
	}
	fd, err := os.OpenFile(name, OptReadOnly, 0777)
	if err != nil {
		return nil, err
	}
	defer fd.Close()

	names, err := fd.Readdirnames(-1)
	if err != nil {
		return nil, err
	}

	return names, nil
}

func (f *BasicFilesystem) Open(name string) (File, error) {
	rootedName, err := f.rooted(name)
	if err != nil {
		return nil, err
	}
	fd, err := os.Open(rootedName)
	if err != nil {
		return nil, err
	}
	return fsFile{fd, name}, err
}

func (f *BasicFilesystem) OpenFile(name string, flags int, mode FileMode) (File, error) {
	rootedName, err := f.rooted(name)
	if err != nil {
		return nil, err
	}
	fd, err := os.OpenFile(rootedName, flags, os.FileMode(mode))
	if err != nil {
		return nil, err
	}
	return fsFile{fd, name}, err
}

func (f *BasicFilesystem) Create(name string) (File, error) {
	rootedName, err := f.rooted(name)
	if err != nil {
		return nil, err
	}
	fd, err := os.Create(rootedName)
	if err != nil {
		return nil, err
	}
	return fsFile{fd, name}, err
}

func (f *BasicFilesystem) Walk(root string, walkFn WalkFunc) error {
	// implemented in WalkFilesystem
	return errors.New("not implemented")
}

func (f *BasicFilesystem) Glob(pattern string) ([]string, error) {
	pattern, err := f.rooted(pattern)
	if err != nil {
		return nil, err
	}
	files, err := filepath.Glob(pattern)
	unrooted := make([]string, len(files))
	for i := range files {
		unrooted[i] = f.unrooted(files[i])
	}
	return unrooted, err
}

func (f *BasicFilesystem) Usage(name string) (Usage, error) {
	name, err := f.rooted(name)
	if err != nil {
		return Usage{}, err
	}
	u, err := du.Get(name)
	return Usage{
		Free:  u.FreeBytes,
		Total: u.TotalBytes,
	}, err
}

func (f *BasicFilesystem) Type() FilesystemType {
	return FilesystemTypeBasic
}

func (f *BasicFilesystem) URI() string {
	return strings.TrimPrefix(f.root, `\\?\`)
}

// fsFile implements the fs.File interface on top of an os.File
type fsFile struct {
	*os.File
	name string
}

func (f fsFile) Name() string {
	return f.name
}

func (f fsFile) Stat() (FileInfo, error) {
	info, err := f.File.Stat()
	if err != nil {
		return nil, err
	}
	return fsFileInfo{info}, nil
}

// fsFileInfo implements the fs.FileInfo interface on top of an os.FileInfo.
type fsFileInfo struct {
	os.FileInfo
}

func (e fsFileInfo) IsSymlink() bool {
	// Must use fsFileInfo.Mode() because it may apply magic.
	return e.Mode()&ModeSymlink != 0
}

func (e fsFileInfo) IsRegular() bool {
	// Must use fsFileInfo.Mode() because it may apply magic.
	return e.Mode()&ModeType == 0
}
