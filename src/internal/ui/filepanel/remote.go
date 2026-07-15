package filepanel

import (
	"os"
	"sort"

	"github.com/yorukot/superfile/src/internal/ui/sortmodel"
	"github.com/yorukot/superfile/src/pkg/backend"
)

// fileInfoDirEntry adapts os.FileInfo to the os.DirEntry interface.
type fileInfoDirEntry struct {
	fi os.FileInfo
}

func (e *fileInfoDirEntry) Name() string               { return e.fi.Name() }
func (e *fileInfoDirEntry) IsDir() bool                { return e.fi.IsDir() }
func (e *fileInfoDirEntry) Type() os.FileMode          { return e.fi.Mode().Type() }
func (e *fileInfoDirEntry) Info() (os.FileInfo, error) { return e.fi, nil }

// sortRemoteFileElement sorts file info entries from a remote FS into Elements.
func sortRemoteFileElement(sortKind sortmodel.SortKind, reversed bool, infos []os.FileInfo, location string, fs backend.FileSystem) []Element {
	elements := make([]Element, 0, len(infos))
	for _, info := range infos {
		isDir := info.IsDir()
		// Check for symlink to directory on remote FS
		if info.Mode()&os.ModeSymlink != 0 {
			if targetInfo, err := fs.Stat(fs.Join(location, info.Name())); err == nil && targetInfo.IsDir() {
				isDir = true
			}
		}

		elements = append(elements, Element{
			Name:      info.Name(),
			Directory: isDir,
			Location:  fs.Join(location, info.Name()),
			Info:      info,
		})
	}

	sort.Slice(elements, getOrderingFunc(elements, reversed, sortKind))

	return elements
}
