package filepanel

import (
	"os"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"

	"github.com/yorukot/superfile/src/internal/ui/sortmodel"
	"github.com/yorukot/superfile/src/pkg/backend"
)

// Make sure to use New() to ensure that maps are initialized
// zero value `Model{}`, or direct initialization should be avoided
// or used very carefully if needed
type Model struct {

	// Note: We have tried to minimize direct access to cursor,
	// and read it via GetCursor() at most places, to make it easier
	// to find and harder to cause bugs of invalid value getting set to cursor
	cursor      int
	renderIndex int
	IsFocused   bool
	Location    string
	// FileSystem for this panel. nil means local OS filesystem.
	FS backend.FileSystem
	// RemoteLoading indicates that an async remote directory load is in progress.
	// While true, the panel shows a loading indicator instead of stale content.
	RemoteLoading bool
	// Dimension fields
	width  int // Total width including borders
	height int // Total height including borders

	SortKind     sortmodel.SortKind
	SortReversed bool

	PanelMode PanelMode
	// key is file location, value order of selection
	selected           map[string]int
	selectOrderCounter int
	element            []Element
	DirectoryRecords   map[string]directoryRecord
	// LastLoadedLocation is the location for which element was last loaded.
	// Used by remote panels to detect navigation changes for async loading.
	LastLoadedLocation string
	Rename             textinput.Model
	Renaming           bool
	SearchBar          textinput.Model
	LastTimeGetElement time.Time
	TargetFile         string             // filename to position cursor on after load
	columns            []columnDefinition // columns for rendering
}

// Record for directory navigation
type directoryRecord struct {
	directoryCursor int
	directoryRender int
}

// Element within a file panel
type Element struct {
	Name      string
	Location  string
	Directory bool
	Info      os.FileInfo
}

// Type representing the mode of the panel
type PanelMode uint

// Constants for select mode or browser mode
const (
	SelectMode PanelMode = iota
	BrowserMode
)

type sliceOrderFunc func(i, j int) bool

type columnRenderer func(indexElement int, columnWidth int) string

type columnDefinition struct {
	Name         string
	Size         int
	HeaderAlign  lipgloss.Position
	columnRender columnRenderer
}
