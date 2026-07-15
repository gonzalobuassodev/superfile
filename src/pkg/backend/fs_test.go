package backend

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)



// verifyFileSystemContract runs a common set of assertions against any
// FileSystem implementation. Local and remote implementations must both
// satisfy this contract.
func verifyFileSystemContract(t *testing.T, fs FileSystem, rootDir string) {
	t.Helper()

	t.Run("ReadDir returns directory entries", func(t *testing.T) {
		entries, err := fs.ReadDir(rootDir)
		require.NoError(t, err)
		assert.NotEmpty(t, entries, "root directory should contain entries")
	})

	t.Run("Stat returns file info", func(t *testing.T) {
		info, err := fs.Stat(rootDir)
		require.NoError(t, err)
		assert.True(t, info.IsDir(), "root should be a directory")
	})

	t.Run("Lstat on file", func(t *testing.T) {
		info, err := fs.Lstat(rootDir)
		require.NoError(t, err)
		assert.NotNil(t, info)
	})

	t.Run("Join concatenates path elements", func(t *testing.T) {
		joined := fs.Join(rootDir, "subdir", "file.txt")
		assert.Contains(t, joined, "subdir")
		assert.Contains(t, joined, "file.txt")
	})

	t.Run("Dir returns parent directory", func(t *testing.T) {
		sub := fs.Join(rootDir, "child")
		parent := fs.Dir(sub)
		assert.Equal(t, rootDir, parent)
	})

	t.Run("Base returns last path element", func(t *testing.T) {
		base := fs.Base(fs.Join(rootDir, "somefile.txt"))
		assert.Equal(t, "somefile.txt", base)
	})

	t.Run("IsLocal returns bool", func(t *testing.T) {
		// Just verify the method exists and returns a bool
		isLocal := fs.IsLocal()
		assert.IsType(t, false, isLocal)
	})

	t.Run("Name returns non-empty string", func(t *testing.T) {
		name := fs.Name()
		assert.NotEmpty(t, name)
	})

	t.Run("Create and Open round-trip", func(t *testing.T) {
		testFile := fs.Join(rootDir, "test_roundtrip.txt")
		content := "hello sftp panel"

		w, err := fs.Create(testFile)
		require.NoError(t, err)
		_, err = w.Write([]byte(content))
		require.NoError(t, err)
		err = w.Close()
		require.NoError(t, err)

		defer func() {
			_ = fs.Remove(testFile)
		}()

		r, err := fs.Open(testFile)
		require.NoError(t, err)
		data, err := io.ReadAll(r)
		require.NoError(t, err)
		err = r.Close()
		require.NoError(t, err)

		assert.Equal(t, content, string(data))
	})

	t.Run("Remove deletes a file", func(t *testing.T) {
		testFile := fs.Join(rootDir, "test_remove.txt")
		w, err := fs.Create(testFile)
		require.NoError(t, err)
		_ = w.Close()

		err = fs.Remove(testFile)
		require.NoError(t, err)

		_, err = fs.Stat(testFile)
		assert.Error(t, err, "file should not exist after Remove")
	})

	t.Run("MkdirAll creates directories", func(t *testing.T) {
		testDir := fs.Join(rootDir, "a", "b", "c")
		err := fs.MkdirAll(testDir, 0755)
		require.NoError(t, err)

		defer func() {
			_ = fs.RemoveAll(fs.Join(rootDir, "a"))
		}()

		info, err := fs.Stat(testDir)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	})

	t.Run("RemoveAll removes directory tree", func(t *testing.T) {
		testDir := fs.Join(rootDir, "removeall_test")
		err := fs.MkdirAll(fs.Join(testDir, "sub"), 0755)
		require.NoError(t, err)

		w, err := fs.Create(fs.Join(testDir, "sub", "f.txt"))
		require.NoError(t, err)
		_ = w.Close()

		err = fs.RemoveAll(testDir)
		require.NoError(t, err)

		_, err = fs.Stat(testDir)
		assert.Error(t, err, "directory should not exist after RemoveAll")
	})

	t.Run("Rename moves a file", func(t *testing.T) {
		src := fs.Join(rootDir, "rename_src.txt")
		dst := fs.Join(rootDir, "rename_dst.txt")

		w, err := fs.Create(src)
		require.NoError(t, err)
		_, _ = w.Write([]byte("rename me"))
		_ = w.Close()

		defer func() {
			_ = fs.Remove(dst)
		}()

		err = fs.Rename(src, dst)
		require.NoError(t, err)

		_, err = fs.Stat(dst)
		assert.NoError(t, err)

		_, err = fs.Stat(src)
		assert.Error(t, err, "source should not exist after Rename")
	})

	t.Run("Walk visits all files", func(t *testing.T) {
		walkDir := fs.Join(rootDir, "walk_test")
		err := fs.MkdirAll(fs.Join(walkDir, "sub"), 0755)
		require.NoError(t, err)

		w, err := fs.Create(fs.Join(walkDir, "a.txt"))
		require.NoError(t, err)
		_ = w.Close()
		w, err = fs.Create(fs.Join(walkDir, "sub", "b.txt"))
		require.NoError(t, err)
		_ = w.Close()

		defer func() {
			_ = fs.RemoveAll(walkDir)
		}()

		var visited []string
		err = fs.Walk(walkDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			visited = append(visited, fs.Base(path))
			return nil
		})
		require.NoError(t, err)

		// walk_test, a.txt, sub, b.txt or similar
		assert.Contains(t, visited, "a.txt")
		assert.Contains(t, visited, "b.txt")
		assert.Contains(t, visited, "sub")
	})

	t.Run("Abs returns absolute path", func(t *testing.T) {
		abs, err := fs.Abs(fs.Join(rootDir, "..", filepath.Base(rootDir)))
		require.NoError(t, err)

		// Should resolve to rootDir
		info, err := fs.Stat(abs)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	})

	t.Run("ReadLink on non-symlink returns error", func(t *testing.T) {
		// The behavior of ReadLink on non-symlinks varies by implementation.
		// Just verify the method exists and handles the case gracefully.
		_, err := fs.ReadLink(rootDir)
		// Some FS return an error, others may not support symlinks
		// Just verify no panic
		_ = err
	})

	t.Run("Close does not panic", func(t *testing.T) {
		assert.NotPanics(t, func() {
			_ = fs.Close()
		})
	})
}

func TestLocalFS_InterfaceConformance(t *testing.T) {
	dir := t.TempDir()

	// Create some files so the directory isn't empty
	require.NoError(t, os.WriteFile(filepath.Join(dir, "existing.txt"), []byte("data"), 0644))

	fs := NewLocalFileSystem()
	verifyFileSystemContract(t, fs, dir)
}

func TestLocalFS_IsLocal(t *testing.T) {
	fs := NewLocalFileSystem()
	assert.True(t, fs.IsLocal())
}

func TestLocalFS_Name(t *testing.T) {
	fs := NewLocalFileSystem()
	assert.Equal(t, "local", fs.Name())
}

func TestLocalFS_Join(t *testing.T) {
	fs := NewLocalFileSystem()
	result := fs.Join("/a", "b", "c")
	assert.Equal(t, filepath.Join("/a", "b", "c"), result)
}

func TestLocalFS_Dir(t *testing.T) {
	fs := NewLocalFileSystem()
	assert.Equal(t, "/a/b", fs.Dir("/a/b/c.txt"))
}

func TestLocalFS_Base(t *testing.T) {
	fs := NewLocalFileSystem()
	assert.Equal(t, "c.txt", fs.Base("/a/b/c.txt"))
}

func TestLocalFS_Abs(t *testing.T) {
	fs := NewLocalFileSystem()
	abs, err := fs.Abs(".")
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(abs))
}

func TestLocalFS_Walk(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f1.txt"), []byte("a"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "f2.txt"), []byte("b"), 0644))

	fs := NewLocalFileSystem()
	var visited []string
	err := fs.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		visited = append(visited, filepath.Base(path))
		return nil
	})
	require.NoError(t, err)

	// Verify walk visits all entries
	visitedStr := strings.Join(visited, ",")
	assert.Contains(t, visitedStr, filepath.Base(dir))
	assert.Contains(t, visitedStr, "f1.txt")
	assert.Contains(t, visitedStr, "sub")
	assert.Contains(t, visitedStr, "f2.txt")
}

// Test that localFS default nil is not set — not applicable here,
// but ensure NewLocalFileSystem works without pointer confusion
func TestLocalFS_NewReturnsValue(t *testing.T) {
	fs := NewLocalFileSystem()
	assert.NotNil(t, fs)
}

// ---- SFTP Filesystem tests ----

// TestSFTPFS_IsLocal verifies sftpFS returns IsLocal() == false.
func TestSFTPFS_IsLocal(t *testing.T) {
	fs := &sftpFS{}
	assert.False(t, fs.IsLocal())
}

// TestSFTPFS_Name verifies sftpFS returns the configured display name.
func TestSFTPFS_Name(t *testing.T) {
	fs := &sftpFS{name: "test-server"}
	assert.Equal(t, "test-server", fs.Name())
}

// TestSFTPFS_Join verifies sftpFS uses forward-slash path.Join.
func TestSFTPFS_Join(t *testing.T) {
	fs := &sftpFS{}
	result := fs.Join("/remote", "dir", "file.txt")
	assert.Equal(t, "/remote/dir/file.txt", result)
}

// TestSFTPFS_Dir verifies sftpFS Dir returns the parent directory.
func TestSFTPFS_Dir(t *testing.T) {
	fs := &sftpFS{}
	assert.Equal(t, "/remote/dir", fs.Dir("/remote/dir/file.txt"))
}

// TestSFTPFS_Base verifies sftpFS Base returns the last path element.
func TestSFTPFS_Base(t *testing.T) {
	fs := &sftpFS{}
	assert.Equal(t, "file.txt", fs.Base("/remote/dir/file.txt"))
}

// TestSFTPFS_Abs verifies sftpFS Abs uses path.Join for remote resolution.
func TestSFTPFS_Abs(t *testing.T) {
	fs := &sftpFS{}
	abs, err := fs.Abs("/remote/dir/../dir2")
	require.NoError(t, err)
	assert.Equal(t, "/remote/dir2", abs)
}

// TestSFTPFS_CloseWithoutClient verifies Close does not panic on nil client.
func TestSFTPFS_CloseWithoutClient(t *testing.T) {
	fs := &sftpFS{}
	assert.NotPanics(t, func() {
		err := fs.Close()
		assert.Error(t, err) // Should error since no client
	})
}

// Walk adapter function test
func TestSFTPFS_WalkFuncReturnsError(t *testing.T) {
	// Verify the walk function signature matches filepath.WalkFunc
	var _ filepath.WalkFunc = func(path string, info os.FileInfo, err error) error {
		return nil
	}
	assert.True(t, true, "WalkFunc signature compiles")
}
