package filepreview

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseFileURLs(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "single file URL",
			input:    "file:///home/user/doc.txt",
			expected: []string{"/home/user/doc.txt"},
		},
		{
			name:     "multiple file URLs",
			input:    "file:///home/user/doc.txt\nfile:///home/user/photo.jpg",
			expected: []string{"/home/user/doc.txt", "/home/user/photo.jpg"},
		},
		{
			name:     "URLs with comment lines",
			input:    "# comment\nfile:///home/user/doc.txt\n",
			expected: []string{"/home/user/doc.txt"},
		},
		{
			name:     "empty input",
			input:    "",
			expected: nil,
		},
		{
			name:     "whitespace only",
			input:    "  \n  \n",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseFileURLs(tt.input)
			assert.NoError(t, err)
			if tt.expected == nil {
				assert.Empty(t, result)
			} else {
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestParseLinesAsPaths(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "single path",
			input:    "/home/user/doc.txt",
			expected: []string{"/home/user/doc.txt"},
		},
		{
			name:     "multiple paths newline separated",
			input:    "/home/user/doc.txt\n/home/user/photo.jpg",
			expected: []string{"/home/user/doc.txt", "/home/user/photo.jpg"},
		},
		{
			name:     "empty input",
			input:    "",
			expected: nil,
		},
		{
			name:     "trailing newline",
			input:    "/home/user/doc.txt\n",
			expected: []string{"/home/user/doc.txt"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseLinesAsPaths(tt.input)
			assert.NoError(t, err)
			if tt.expected == nil {
				assert.Empty(t, result)
			} else {
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestNewOSClipboard(t *testing.T) {
	c := NewOSClipboard()
	assert.NotNil(t, c)
}

func TestHasTool(t *testing.T) {
	// On any OS, "go" should be in PATH (the test runs via `go test`)
	assert.True(t, hasTool("go"))
	assert.False(t, hasTool("this-tool-definitely-does-not-exist-12345"))
}

func TestWriteEmptyPaths(t *testing.T) {
	c := NewOSClipboard()
	err := c.WriteFileURIs(nil)
	assert.NoError(t, err)

	err = c.WriteFileURIs([]string{})
	assert.NoError(t, err)
}

func TestReadWriteNilOnUnsupportedPlatform(t *testing.T) {
	// This test verifies that the implementation handles platforms gracefully.
	// On macOS/Linux these actually try to call osascript/xclip.
	// On Windows/others they return nil gracefully.
	if runtime.GOOS == "windows" {
		c := NewOSClipboard()
		err := c.WriteFileURIs([]string{"/tmp/test.txt"})
		assert.NoError(t, err)

		items, err := c.ReadFileURIs()
		assert.NoError(t, err)
		assert.Empty(t, items)
	}
}
