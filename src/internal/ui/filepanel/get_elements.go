package filepanel

import (
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/yorukot/superfile/src/pkg/utils"
)

// TODO : Take common.Config.CaseSensitiveSort as a function parameter
// and also consider testing this caseSensitive with both true and false in
// our unit_test TestReturnDirElement
// getDirectoryElements returns the directory elements for the panel's current location
func (m *Model) getDirectoryElements(displayDotFile bool) []Element {
	if m.FS != nil {
		return m.getRemoteDirectoryElements(displayDotFile)
	}

	dirEntries, err := os.ReadDir(m.Location)
	if err != nil {
		slog.Error("Error while returning folder elements", "error", err)
		return nil
	}

	dirEntries = slices.DeleteFunc(dirEntries, func(e os.DirEntry) bool {
		// Entries not needed to be considered
		_, err := e.Info()
		return err != nil || (strings.HasPrefix(e.Name(), ".") && !displayDotFile)
	})

	// No files/directories to process
	if len(dirEntries) == 0 {
		return nil
	}
	return sortFileElement(m.SortKind, m.SortReversed, dirEntries, m.Location)
}

// getRemoteDirectoryElements reads directory entries from a remote filesystem.
func (m *Model) getRemoteDirectoryElements(displayDotFile bool) []Element {
	infos, err := m.FS.ReadDir(m.Location)
	if err != nil {
		slog.Error("Error while returning remote folder elements", "error", err)
		return nil
	}

	// Filter out dot files if needed
	filtered := make([]os.FileInfo, 0, len(infos))
	for _, info := range infos {
		if strings.HasPrefix(info.Name(), ".") && !displayDotFile {
			continue
		}
		filtered = append(filtered, info)
	}

	if len(filtered) == 0 {
		return nil
	}

	return sortRemoteFileElement(m.SortKind, m.SortReversed, filtered, m.Location, m.FS)
}

// getDirectoryElementsBySearch returns filtered directory elements based on search string
func (m *Model) getDirectoryElementsBySearch(displayDotFile bool) []Element {
	searchString := m.SearchBar.Value()

	var items []os.DirEntry
	var err error

	if m.FS != nil {
		// For remote FS, we read and then wrap as DirEntry for compatibility
		infos, readErr := m.FS.ReadDir(m.Location)
		if readErr != nil {
			slog.Error("Error while return remote folder element function", "error", readErr)
			return nil
		}
		items = make([]os.DirEntry, len(infos))
		for i, info := range infos {
			items[i] = &fileInfoDirEntry{fi: info}
		}
	} else {
		items, err = os.ReadDir(m.Location)
		if err != nil {
			slog.Error("Error while return folder element function", "error", err)
			return nil
		}
	}

	if len(items) == 0 {
		return nil
	}

	folderElementMap := map[string]os.DirEntry{}
	fileAndDirectories := []string{}

	for _, item := range items {
		fileInfo, err := item.Info()
		if err != nil {
			continue
		}
		if !displayDotFile && strings.HasPrefix(fileInfo.Name(), ".") {
			continue
		}

		fileAndDirectories = append(fileAndDirectories, item.Name())
		folderElementMap[item.Name()] = item
	}

	fzfResults := utilsFzfSearch(searchString, fileAndDirectories)
	dirElements := make([]os.DirEntry, 0, len(fzfResults))
	for _, item := range fzfResults {
		resultItem := folderElementMap[item.Key]
		dirElements = append(dirElements, resultItem)
	}

	return sortFileElement(m.SortKind, m.SortReversed, dirElements, m.Location)
}

// fzfResult is a simplified search result item to avoid importing fzf-lib here
// for tests.
type fzfResult struct {
	Key string
}

// utilsFzfSearch is a variable so it can be replaced in tests. In production
// it uses the fzf-lib fuzzy search.
var utilsFzfSearch = func(searchString string, items []string) []fzfResult {
	results := utils.FzfSearch(searchString, items)
	fzfResults := make([]fzfResult, len(results))
	for i, r := range results {
		fzfResults[i] = fzfResult{Key: r.Key}
	}
	return fzfResults
}

// Helper to decide whether to skip updating a panel this tick.
func (m *Model) shouldSkipPanelUpdate(nowTime time.Time) bool {
	// Remote filesystems are slower — use longer intervals to avoid excessive SFTP calls
	if m.FS != nil {
		remoteDelay := 5 * time.Second
		return nowTime.Sub(m.LastTimeGetElement) < remoteDelay
	}

	if !m.IsFocused {
		return nowTime.Sub(m.LastTimeGetElement) < nonFocussedPanelReRenderTime
	}

	reRenderTime := int(float64(m.ElemCount()) / ReRenderChunkDivisor)
	reRenderTime = min(reRenderTime, ReRenderMaxDelay)
	return !m.NeedsReRender() &&
		nowTime.Sub(m.LastTimeGetElement) < time.Duration(reRenderTime)*time.Second
}

func (m *Model) UpdateElementsIfNeeded(force bool, displayDotFile bool) {
	nowTime := time.Now()
	if force || !m.shouldSkipPanelUpdate(nowTime) {
		// Load elements for this panel (with/without search filter)
		m.element = m.getElements(displayDotFile)
		// Update file panel list
		m.LastTimeGetElement = nowTime
		m.LastLoadedLocation = m.Location

		// For hover to file on first time loading
		if m.TargetFile != "" {
			m.applyTargetFileCursor()
		}

		// If cursor becomes invalid due to element update, reset
		if m.ValidateCursorAndRenderIndex() != nil {
			m.scrollToCursor(0)
		}
	}
}

// ClearElements removes all panel elements without triggering a directory read.
// Used for remote panels to clear stale content before async loading.
func (m *Model) ClearElements() {
	m.element = nil
	m.LastTimeGetElement = time.Now()
}

// ApplyRemoteElements sets panel elements from an async remote read result.
// This is called by the model when a RemoteDirLoadedMsg arrives, instead of
// doing a synchronous ReadDir that would block the UI thread.
func (m *Model) ApplyRemoteElements(infos []os.FileInfo, displayDotFile bool) {
	m.RemoteLoading = false
	// Filter out dot files if needed
	filtered := make([]os.FileInfo, 0, len(infos))
	for _, info := range infos {
		if strings.HasPrefix(info.Name(), ".") && !displayDotFile {
			continue
		}
		filtered = append(filtered, info)
	}

	if len(filtered) > 0 {
		m.element = sortRemoteFileElement(m.SortKind, m.SortReversed, filtered, m.Location, m.FS)
	} else {
		m.element = nil
	}

	m.LastTimeGetElement = time.Now()
	m.LastLoadedLocation = m.Location

	// For hover to file on first time loading
	if m.TargetFile != "" {
		m.applyTargetFileCursor()
	}

	// If cursor becomes invalid due to element update, reset
	if m.ValidateCursorAndRenderIndex() != nil {
		m.scrollToCursor(0)
	}
}

// Retrieves elements for a panel based on search bar value and sort options.
func (m *Model) getElements(displayDotFile bool) []Element {
	if m.SearchBar.Value() != "" {
		return m.getDirectoryElementsBySearch(displayDotFile)
	}
	return m.getDirectoryElements(displayDotFile)
}
