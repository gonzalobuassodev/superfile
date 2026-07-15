// Package backend provides an abstraction over filesystem operations,
// enabling superfile to work with both local and remote filesystems
// (e.g., SFTP) through a common interface.
package backend

import (
	"io"
	"os"
	"path/filepath"
)

// FileSystem defines the interface for filesystem operations.
// A nil FileSystem reference means the local OS — existing code paths
// that do not set this field continue to use os.* calls unchanged.
type FileSystem interface {
	// ReadDir reads the directory named by dirname and returns a list of
	// directory entries sorted by filename.
	ReadDir(path string) ([]os.FileInfo, error)

	// Stat returns a FileInfo describing the named file.
	Stat(path string) (os.FileInfo, error)

	// Lstat returns a FileInfo describing the named file.
	// If the file is a symbolic link, the returned FileInfo describes
	// the symbolic link. Lstat makes no attempt to follow the link.
	Lstat(path string) (os.FileInfo, error)

	// Open opens the named file for reading.
	Open(path string) (io.ReadCloser, error)

	// Create creates or truncates the named file and returns a writer.
	Create(path string) (io.WriteCloser, error)

	// Remove removes the named file or empty directory.
	Remove(path string) error

	// RemoveAll removes path and any children it contains.
	RemoveAll(path string) error

	// Rename renames (moves) oldPath to newPath.
	Rename(oldPath, newPath string) error

	// MkdirAll creates a directory named path, along with any necessary
	// parents, with the specified permission bits.
	MkdirAll(path string, perm os.FileMode) error

	// ReadLink returns the destination of the named symbolic link.
	ReadLink(path string) (string, error)

	// Walk walks the file tree rooted at root, calling walkFn for
	// each file or directory in the tree, including root.
	Walk(root string, walkFn filepath.WalkFunc) error

	// Close closes the filesystem and releases any resources.
	Close() error

	// Join joins any number of path elements into a single path.
	Join(elem ...string) string

	// Dir returns all but the last element of path, typically the
	// path's directory.
	Dir(path string) string

	// Base returns the last element of path.
	Base(path string) string

	// Abs returns an absolute representation of path.
	Abs(path string) (string, error)

	// Name returns a display name for this filesystem (e.g., "local", "myserver").
	Name() string

	// IsLocal returns true if this is the local OS filesystem.
	IsLocal() bool
}
