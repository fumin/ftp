// Package ftp exposes a ftp server as an io/fs.FS filesystem.
package ftp

import (
	"fmt"
	"io"
	"io/fs"
	"log"
	"path"
	"sort"
	"time"

	jlaftp "github.com/jlaffaye/ftp"
	"github.com/pkg/errors"
)

type fileinfo struct {
	e jlaftp.Entry
}

func (info fileinfo) Name() string {
	return info.e.Name
}

func (info fileinfo) Size() int64 {
	return int64(info.e.Size)
}

func (info fileinfo) Mode() fs.FileMode {
	switch info.e.Type {
	case jlaftp.EntryTypeFile:
		return 0
	case jlaftp.EntryTypeFolder:
		return fs.ModeDir
	case jlaftp.EntryTypeLink:
		return fs.ModeSymlink
	default:
		panic(fmt.Sprintf("%+v", info))
	}
}

func (info fileinfo) ModTime() time.Time {
	return info.e.Time
}

func (info fileinfo) IsDir() bool {
	return info.e.Type == jlaftp.EntryTypeFolder
}

func (info fileinfo) Sys() any {
	return info.e
}

// File is a io/fs.File.
type File struct {
	info fileinfo
	resp *jlaftp.Response
}

// Stat returns the file info.
func (f *File) Stat() (fs.FileInfo, error) {
	return f.info, nil
}

// Stat reads the file.
func (f *File) Read(b []byte) (int, error) {
	n, err := f.resp.Read(b)
	if err == io.EOF {
		return n, err
	}
	if err != nil {
		return n, errors.Wrap(err, "")
	}
	return n, nil
}

// Close closes the file.
func (f *File) Close() error {
	// Read to the end because of a bug in jlaffaye/ftp.
	// https://github.com/jlaffaye/ftp/issues/214.
	if _, err := io.Copy(io.Discard, f.resp); err != nil {
		return errors.Wrap(err, "")
	}

	if err := f.resp.Close(); err != nil {
		log.Printf("%+v", err)
		return errors.Wrap(err, "")
	}
	return nil
}

// FS is an io/fs.ReadDirFS and io/fs.StatFS.
type FS struct {
	c *jlaftp.ServerConn
}

// NewFS returns a file system from a ftp connection.
func NewFS(c *jlaftp.ServerConn) *FS {
	fs := &FS{c: c}
	return fs
}

// Open opens a file.
func (fs *FS) Open(name string) (fs.File, error) {
	entry, err := fs.getEntry(name)
	if err != nil {
		return nil, errors.Wrap(err, "")
	}
	resp, err := fs.c.Retr(name)
	if err != nil {
		return nil, errors.Wrap(err, "")
	}
	f := &File{info: fileinfo{e: *entry}, resp: resp}
	return f, nil
}

// Stat returns the information of a file.
func (fs *FS) Stat(name string) (fs.FileInfo, error) {
	entry, err := fs.getEntry(name)
	if err != nil {
		return nil, errors.Wrap(err, "")
	}
	return fileinfo{e: *entry}, nil
}

// ReadDir reads a directory.
func (fsys *FS) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := fsys.c.List(name)
	if err != nil {
		return nil, errors.Wrap(err, "")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	ds := make([]fs.DirEntry, 0, len(entries))
	for _, e := range entries {
		switch e.Name {
		case ".":
			continue
		case "..":
			continue
		}
		ds = append(ds, fs.FileInfoToDirEntry(fileinfo{e: *e}))
	}
	return ds, nil
}

func (fs *FS) getEntry(name string) (*jlaftp.Entry, error) {
	parent := path.Dir(name)
	entries, err := fs.c.List(parent)
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("%s", parent))
	}
	base := path.Base(name)
	var entry *jlaftp.Entry
	for _, e := range entries {
		if e.Name == base {
			entry = e
			break
		}
	}
	if entry == nil {
		derefed := make([]jlaftp.Entry, 0, len(entries))
		for _, e := range entries {
			derefed = append(derefed, *e)
		}
		return nil, errors.Errorf("%s %+v", base, derefed)
	}
	return entry, nil
}
