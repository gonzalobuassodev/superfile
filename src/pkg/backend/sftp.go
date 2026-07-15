package backend

import (
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/pkg/sftp"
)

// sftpFS implements FileSystem over an SFTP client connection.
type sftpFS struct {
	client *sftp.Client
	name   string // display name for this connection
}

// NewSFTPFileSystem creates a new SFTP-backed FileSystem from an existing
// *sftp.Client. The name parameter is a human-readable identifier shown in
// the UI (e.g., the connection name from config).
func NewSFTPFileSystem(client *sftp.Client, name string) FileSystem {
	return &sftpFS{
		client: client,
		name:   name,
	}
}

func (fs *sftpFS) ReadDir(path string) ([]os.FileInfo, error) {
	if fs.client == nil {
		return nil, errors.New("sftpFS: client is nil")
	}
	return fs.client.ReadDir(path)
}

func (fs *sftpFS) Stat(path string) (os.FileInfo, error) {
	if fs.client == nil {
		return nil, errors.New("sftpFS: client is nil")
	}
	return fs.client.Stat(path)
}

func (fs *sftpFS) Lstat(path string) (os.FileInfo, error) {
	if fs.client == nil {
		return nil, errors.New("sftpFS: client is nil")
	}
	return fs.client.Lstat(path)
}

func (fs *sftpFS) Open(path string) (io.ReadCloser, error) {
	if fs.client == nil {
		return nil, errors.New("sftpFS: client is nil")
	}
	return fs.client.Open(path)
}

func (fs *sftpFS) Create(path string) (io.WriteCloser, error) {
	if fs.client == nil {
		return nil, errors.New("sftpFS: client is nil")
	}
	return fs.client.Create(path)
}

func (fs *sftpFS) Remove(path string) error {
	if fs.client == nil {
		return errors.New("sftpFS: client is nil")
	}
	return fs.client.Remove(path)
}

func (fs *sftpFS) RemoveAll(path string) error {
	if fs.client == nil {
		return errors.New("sftpFS: client is nil")
	}
	// Walk the tree bottom-up so directories are empty when removed
	var entries []struct {
		path string
		dir  bool
	}
	walker := fs.client.Walk(path)
	for walker.Step() {
		if walker.Err() != nil {
			continue
		}
		entries = append(entries, struct {
			path string
			dir  bool
		}{walker.Path(), walker.Stat().IsDir()})
	}
	// Remove in reverse order (files first, then directories bottom-up)
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].dir {
			if err := fs.client.RemoveDirectory(entries[i].path); err != nil {
				return err
			}
		} else {
			if err := fs.client.Remove(entries[i].path); err != nil {
				return err
			}
		}
	}
	return nil
}

func (fs *sftpFS) Rename(oldPath, newPath string) error {
	if fs.client == nil {
		return errors.New("sftpFS: client is nil")
	}
	return fs.client.Rename(oldPath, newPath)
}

func (fs *sftpFS) MkdirAll(path string, perm os.FileMode) error {
	if fs.client == nil {
		return errors.New("sftpFS: client is nil")
	}
	return fs.client.MkdirAll(path)
}

func (fs *sftpFS) ReadLink(path string) (string, error) {
	if fs.client == nil {
		return "", errors.New("sftpFS: client is nil")
	}
	return fs.client.ReadLink(path)
}

// Walk implements filepath.Walk using the SFTP Walker.
func (fs *sftpFS) Walk(root string, walkFn filepath.WalkFunc) error {
	if fs.client == nil {
		return errors.New("sftpFS: client is nil")
	}
	walker := fs.client.Walk(root)
	for walker.Step() {
		if err := walkFn(walker.Path(), walker.Stat(), walker.Err()); err != nil {
			return err
		}
	}
	return nil
}

func (fs *sftpFS) Close() error {
	if fs.client == nil {
		return errors.New("sftpFS: client is nil")
	}
	return fs.client.Close()
}

func (fs *sftpFS) Join(elem ...string) string {
	return path.Join(elem...)
}

func (fs *sftpFS) Dir(p string) string {
	return path.Dir(p)
}

func (fs *sftpFS) Base(p string) string {
	return path.Base(p)
}

func (fs *sftpFS) Abs(p string) (string, error) {
	absPath := path.Clean(p)
	if path.IsAbs(absPath) {
		return absPath, nil
	}
	// For remote FS, treat relative paths as rooted at "/"
	return path.Clean("/" + p), nil
}

func (fs *sftpFS) Name() string {
	return fs.name
}

func (fs *sftpFS) IsLocal() bool {
	return false
}
