package backend

import (
	"io"
	"os"
	"path/filepath"
)

// localFS implements FileSystem for the local OS filesystem.
// It wraps os.* and filepath.* calls directly.
type localFS struct{}

// NewLocalFileSystem creates a new local filesystem implementation.
func NewLocalFileSystem() FileSystem {
	return &localFS{}
}

func (fs *localFS) ReadDir(path string) ([]os.FileInfo, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	infos := make([]os.FileInfo, len(entries))
	for i, e := range entries {
		infos[i], err = e.Info()
		if err != nil {
			return nil, err
		}
	}
	return infos, nil
}

func (fs *localFS) Stat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

func (fs *localFS) Lstat(path string) (os.FileInfo, error) {
	return os.Lstat(path)
}

func (fs *localFS) Open(path string) (io.ReadCloser, error) {
	return os.Open(path)
}

func (fs *localFS) Create(path string) (io.WriteCloser, error) {
	return os.Create(path)
}

func (fs *localFS) Remove(path string) error {
	return os.Remove(path)
}

func (fs *localFS) RemoveAll(path string) error {
	return os.RemoveAll(path)
}

func (fs *localFS) Rename(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}

func (fs *localFS) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (fs *localFS) ReadLink(path string) (string, error) {
	return os.Readlink(path)
}

func (fs *localFS) Walk(root string, walkFn filepath.WalkFunc) error {
	return filepath.Walk(root, walkFn)
}

func (fs *localFS) Close() error {
	return nil
}

func (fs *localFS) Join(elem ...string) string {
	return filepath.Join(elem...)
}

func (fs *localFS) Dir(path string) string {
	return filepath.Dir(path)
}

func (fs *localFS) Base(path string) string {
	return filepath.Base(path)
}

func (fs *localFS) Abs(path string) (string, error) {
	return filepath.Abs(path)
}

func (fs *localFS) Name() string {
	return "local"
}

func (fs *localFS) IsLocal() bool {
	return true
}
