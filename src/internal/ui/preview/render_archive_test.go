package preview

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatSize(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{(1 << 30), "1.0 GB"},
	}

	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			assert.Equal(t, tc.expected, formatSize(tc.bytes))
		})
	}
}

func TestIsArchiveFile(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"file.zip", true},
		{"archive.tar", true},
		{"archive.tar.gz", true},
		{"archive.tar.bz2", true},
		{"archive.tar.xz", true},
		{"archive.tar.zst", true},
		{"archive.tgz", true},
		{"file.rar", true},
		{"file.7z", true},
		{"file.jar", true},
		{"file.war", true},
		{"file.cbr", true},
		{"file.cbz", true},
		{"file.txt", false},
		{"file.go", false},
		{"file", false},
		{"", false},
		{"UPPERCASE.ZIP", true},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			assert.Equal(t, tc.expected, isArchiveFile(tc.path))
		})
	}
}

func TestArchiveExt(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"file.zip", ".zip"},
		{"file.tar", ".tar"},
		{"file.tar.gz", ".tar.gz"},
		{"file.tar.bz2", ".tar.bz2"},
		{"file.tar.xz", ".tar.xz"},
		{"file.tar.zst", ".tar.zst"},
		{"file.tgz", ".tgz"},
		{"file.rar", ".rar"},
		{"file.7z", ".7z"},
		{"file.txt", ".txt"},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			assert.Equal(t, tc.expected, archiveExt(tc.path))
		})
	}
}

func TestReadZipArchive(t *testing.T) {
	td := t.TempDir()
	zipPath := filepath.Join(td, "test.zip")

	// Create a test zip
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)

	f1, err := w.Create("hello.txt")
	require.NoError(t, err)
	_, err = f1.Write([]byte("hello world"))
	require.NoError(t, err)

	f2, err := w.Create("subdir/nested.go")
	require.NoError(t, err)
	_, err = f2.Write([]byte("package main"))
	require.NoError(t, err)

	// Create an empty directory entry
	_, err = w.Create("emptydir/")
	require.NoError(t, err)

	err = w.Close()
	require.NoError(t, err)

	err = os.WriteFile(zipPath, buf.Bytes(), 0644)
	require.NoError(t, err)

	entries, err := readZipArchive(zipPath)
	require.NoError(t, err)

	// Should have 3 entries (hello.txt, subdir/nested.go, emptydir)
	assert.Len(t, entries, 3)

	found := make(map[string]bool)
	for _, e := range entries {
		found[e.name] = true
		if e.name == "hello.txt" {
			assert.False(t, e.isDir)
			assert.Equal(t, int64(11), e.size)
		}
		if e.name == "emptydir" {
			assert.True(t, e.isDir)
		}
		if e.name == "subdir/nested.go" {
			assert.False(t, e.isDir)
		}
	}
	assert.True(t, found["hello.txt"])
	assert.True(t, found["subdir/nested.go"])
	assert.True(t, found["emptydir"])
}

func TestReadZipArchive_InvalidFile(t *testing.T) {
	td := t.TempDir()
	badPath := filepath.Join(td, "not-a-zip.zip")
	err := os.WriteFile(badPath, []byte("not a zip file"), 0644)
	require.NoError(t, err)

	_, err = readZipArchive(badPath)
	assert.Error(t, err)
}

func TestReadTarArchive_Gzip(t *testing.T) {
	td := t.TempDir()
	tarPath := filepath.Join(td, "test.tar.gz")

	// Create a gzipped tar
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: "hello.txt",
		Size: 11,
		Mode: 0644,
	}))
	_, err := tw.Write([]byte("hello world"))
	require.NoError(t, err)

	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     "subdir",
		Typeflag: tar.TypeDir,
		Mode:     0755,
	}))
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: "subdir/nested.go",
		Size: 12,
		Mode: 0644,
	}))
	_, err = tw.Write([]byte("package main"))
	require.NoError(t, err)

	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	err = os.WriteFile(tarPath, buf.Bytes(), 0644)
	require.NoError(t, err)

	entries, err := readTarArchive(tarPath, ".tar.gz")
	require.NoError(t, err)

	require.Len(t, entries, 3)

	found := make(map[string]bool)
	for _, e := range entries {
		found[e.name] = true
	}
	assert.True(t, found["hello.txt"])
	assert.True(t, found["subdir"])
	assert.True(t, found["subdir/nested.go"])
}

func TestReadTarArchive_PlainTar(t *testing.T) {
	td := t.TempDir()
	tarPath := filepath.Join(td, "test.tar")

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: "plain.txt",
		Size: 4,
		Mode: 0644,
	}))
	_, err := tw.Write([]byte("data"))
	require.NoError(t, err)
	require.NoError(t, tw.Close())

	err = os.WriteFile(tarPath, buf.Bytes(), 0644)
	require.NoError(t, err)

	entries, err := readTarArchive(tarPath, ".tar")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "plain.txt", entries[0].name)
	assert.Equal(t, int64(4), entries[0].size)
}

func TestReadTarArchive_InvalidFile(t *testing.T) {
	td := t.TempDir()
	badPath := filepath.Join(td, "bad.tar")
	err := os.WriteFile(badPath, []byte("not a tar"), 0644)
	require.NoError(t, err)

	_, err = readTarArchive(badPath, ".tar")
	assert.Error(t, err)
}

func TestReadTarEntries_DotEntry(t *testing.T) {
	// Simulate a tar with "." entry
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: ".",
		Typeflag: tar.TypeDir,
		Mode:  0755,
	}))
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: "real-file.txt",
		Size: 3,
		Mode: 0644,
	}))
	_, err := tw.Write([]byte("abc"))
	require.NoError(t, err)
	require.NoError(t, tw.Close())

	entries, err := readTarEntries(&buf)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "real-file.txt", entries[0].name)
}

func TestReadViaLsar(t *testing.T) {
	td := t.TempDir()
	zipPath := filepath.Join(td, "test-lsar.zip")

	// Create a minimal zip
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)
	f, err := w.Create("lsar-test.txt")
	require.NoError(t, err)
	_, err = f.Write([]byte("test"))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	require.NoError(t, os.WriteFile(zipPath, buf.Bytes(), 0644))

	entries, err := readViaLsar(zipPath)
	if err != nil {
		t.Skip("lsar not installed:", err)
	}
	require.NotEmpty(t, entries)
	assert.Equal(t, "lsar-test.txt", entries[0].name)
}

func TestReadArchiveViaExternal_Fallback(t *testing.T) {
	td := t.TempDir()
	badPath := filepath.Join(td, "nonexistent.xyz")
	err := os.WriteFile(badPath, []byte("data"), 0644)
	require.NoError(t, err)

	_, err = readArchiveViaExternal(badPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no external tool available")
}

func TestArchiveEntrySorting(t *testing.T) {
	// Test the sorting used in renderArchivePreview: dirs first, then alpha
	entries := []archiveEntry{
		{name: "z-file.txt"},
		{name: "a-dir", isDir: true},
		{name: "a-file.txt"},
		{name: "b-dir", isDir: true},
	}

	// Sort using the same less function as renderArchivePreview
	sortEntries(entries)

	assert.True(t, entries[0].isDir, "first should be a-dir")
	assert.True(t, entries[1].isDir, "second should be b-dir")
	assert.Equal(t, "a-dir", entries[0].name)
	assert.Equal(t, "b-dir", entries[1].name)
	assert.Equal(t, "a-file.txt", entries[2].name)
	assert.Equal(t, "z-file.txt", entries[3].name)
}

// sortEntries replicates the sort logic from renderArchivePreview for testing.
func sortEntries(entries []archiveEntry) {
	n := len(entries)
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			a, b := entries[i], entries[j]
			swap := false
			if a.isDir && !b.isDir {
				swap = false
			} else if !a.isDir && b.isDir {
				swap = true
			} else if a.name > b.name {
				swap = true
			}
			if swap {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}
}
