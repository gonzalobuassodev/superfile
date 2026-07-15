package internal

import (
	"errors"
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/yorukot/superfile/src/internal/common"
	"github.com/yorukot/superfile/src/internal/ui/filemodel"
	"github.com/yorukot/superfile/src/internal/ui/filepanel"
	"github.com/yorukot/superfile/src/internal/ui/notify"
	"github.com/yorukot/superfile/src/internal/ui/spferror"
	"github.com/yorukot/superfile/src/pkg/backend"
	"github.com/yorukot/superfile/src/pkg/utils"

	tea "charm.land/bubbletea/v2"

	variable "github.com/yorukot/superfile/src/config"
)

// mainKey handles most of key commands in the regular state of the application. For
// keys that performs actions in multiple panels, like going up or down,
// check the state of model m and handle properly.
// TODO: This function has grown too big. It needs to be fixed, via major
// updates and fixes in key handling code
func (m *model) mainKey(msg string) tea.Cmd { //nolint: gocyclo,cyclop,funlen // See above
	switch {
	// If move up Key is pressed, check the current state and executes
	case slices.Contains(common.Hotkeys.ListUp, msg):
		switch m.focusPanel {
		case sidebarFocus:
			m.sidebarModel.ListUp()
		case processBarFocus:
			m.processBarModel.ListUp()
		case metadataFocus:
			m.fileMetaData.ListUp()
		case nonePanelFocus:
			m.getFocusedFilePanel().ListUp()
		}

		// If move down Key is pressed, check the current state and executes
	case slices.Contains(common.Hotkeys.ListDown, msg):
		switch m.focusPanel {
		case sidebarFocus:
			m.sidebarModel.ListDown()
		case processBarFocus:
			m.processBarModel.ListDown()
		case metadataFocus:
			m.fileMetaData.ListDown()
		case nonePanelFocus:
			m.getFocusedFilePanel().ListDown()
		}

	case slices.Contains(common.Hotkeys.PageUp, msg):
		m.getFocusedFilePanel().PgUp()

	case slices.Contains(common.Hotkeys.PageDown, msg):
		m.getFocusedFilePanel().PgDown()

	case slices.Contains(common.Hotkeys.ChangePanelMode, msg):
		m.getFocusedFilePanel().ChangeFilePanelMode()

	case slices.Contains(common.Hotkeys.NextFilePanel, msg):
		if m.focusPanel == nonePanelFocus {
			m.fileModel.NextFilePanel()
		}

	case slices.Contains(common.Hotkeys.PreviousFilePanel, msg):
		if m.focusPanel == nonePanelFocus {
			m.fileModel.PreviousFilePanel()
		}

	case slices.Contains(common.Hotkeys.CloseFilePanel, msg):
		// Close remote filesystem connection if panel has one
		focusedPanel := m.getFocusedFilePanel()
		m.closeRemotePanel(focusedPanel)

		cmd, err := m.fileModel.CloseFilePanel()
		if err != nil && !errors.Is(err, filemodel.ErrMinimumPanelCount) {
			slog.Error("unexpected error while closing new panel", "error", err)
		}
		return cmd
	case slices.Contains(common.Hotkeys.CreateNewFilePanel, msg):
		cmd, err := m.fileModel.CreateNewFilePanel(variable.HomeDir)
		if err != nil && !errors.Is(err, filemodel.ErrMaximumPanelCount) {
			slog.Error("unexpected error while creating new panel", "error", err)
		}
		return cmd
	case slices.Contains(common.Hotkeys.SplitFilePanel, msg):
		cmd, err := m.splitPanel()
		if err != nil && !errors.Is(err, filemodel.ErrMaximumPanelCount) {
			slog.Error("unexpected error while splitting panel", "error", err)
		}
		return cmd
	case slices.Contains(common.Hotkeys.ToggleFilePreviewPanel, msg):
		return m.fileModel.ToggleFilePreviewPanel()

	case slices.Contains(common.Hotkeys.FocusOnSidebar, msg):
		m.focusOnSideBar()

	case slices.Contains(common.Hotkeys.FocusOnProcessBar, msg):
		m.focusOnProcessBar()

	case slices.Contains(common.Hotkeys.FocusOnMetaData, msg):
		m.focusOnMetadata()

	case slices.Contains(common.Hotkeys.PasteItems, msg):
		return m.getPasteItemCmd()

	case slices.Contains(common.Hotkeys.FilePanelItemCreate, msg):
		m.panelCreateNewFile()
	case slices.Contains(common.Hotkeys.PinnedDirectory, msg):
		m.pinnedDirectory()

	case slices.Contains(common.Hotkeys.ToggleDotFile, msg):
		m.toggleDotFileController()

	case slices.Contains(common.Hotkeys.ToggleFooter, msg):
		return m.toggleFooterController()

	case slices.Contains(common.Hotkeys.ExtractFile, msg):
		return m.getExtractFileCmd()

	case slices.Contains(common.Hotkeys.CompressFile, msg):
		return m.getCompressSelectedFilesCmd()

	case slices.Contains(common.Hotkeys.OpenCommandLine, msg):
		m.promptModal.Open(true)
	case slices.Contains(common.Hotkeys.OpenSPFPrompt, msg):
		m.promptModal.Open(false)
	case slices.Contains(common.Hotkeys.OpenZoxide, msg):
		return m.zoxideModal.Open()

	case slices.Contains(common.Hotkeys.OpenHelpMenu, msg):
		m.helpMenu.Open()

	case slices.Contains(common.Hotkeys.OpenSortOptionsMenu, msg):
		m.sortModal.Open(m.getFocusedFilePanel().SortKind)

	case slices.Contains(common.Hotkeys.ToggleReverseSort, msg):
		m.getFocusedFilePanel().ToggleReverseSort()

	case slices.Contains(common.Hotkeys.OpenFileWithEditor, msg):
		return m.openFileWithEditor()

	case slices.Contains(common.Hotkeys.OpenCurrentDirectoryWithEditor, msg):
		return m.openDirectoryWithEditor()

	default:
		return m.normalAndBrowserModeKey(msg)
	}

	return nil
}

func (m *model) normalAndBrowserModeKey(msg string) tea.Cmd {
	// if not focus on the filepanel return
	if !m.getFocusedFilePanel().IsFocused {
		return m.unfocusedFilePanelKey(msg)
	}
	// Check if in the select mode and focusOn filepanel
	if m.getFocusedFilePanel().PanelMode == filepanel.SelectMode {
		return m.filePanelSelectModeKey(msg)
	}

	return m.filePanelNormalModeKey(msg)
}

func (m *model) unfocusedFilePanelKey(msg string) tea.Cmd {
	if m.focusPanel != sidebarFocus {
		return nil
	}

	if slices.Contains(common.Hotkeys.Confirm, msg) {
		return m.sidebarSelectDirectory()
	}
	if slices.Contains(common.Hotkeys.FilePanelItemRename, msg) {
		m.sidebarModel.PinnedItemRename()
	}
	if slices.Contains(common.Hotkeys.SearchBar, msg) {
		m.sidebarSearchBarFocus()
	}
	if slices.Contains(common.Hotkeys.SidebarAddSSH, msg) {
		section := m.sidebarModel.GetCurrentDirectorySection()
		if section == utils.SidebarSectionSSH {
			m.sidebarModel.StartAddSSH()
		}
	}
	if slices.Contains(common.Hotkeys.DeleteItems, msg) {
		section := m.sidebarModel.GetCurrentDirectorySection()
		if section == utils.SidebarSectionSSH {
			name := m.sidebarModel.GetCurrentDirectoryName()
			if name != "" {
				m.pendingSSHDeleteName = name
				m.notifyModel = notify.New(true,
					"Delete SSH Connection",
					"Delete connection \""+name+"\"?",
					notify.DeleteSSHConnectionAction)
			}
		}
	}
	return nil
}

func (m *model) filePanelSelectModeKey(msg string) tea.Cmd {
	panel := m.getFocusedFilePanel()

	switch {
	case slices.Contains(common.Hotkeys.Confirm, msg):
		panel.SingleItemSelect()
	case slices.Contains(common.Hotkeys.FilePanelSelectModeItemsSelectUp, msg):
		panel.ItemSelectUp()
	case slices.Contains(common.Hotkeys.FilePanelSelectModeItemsSelectDown, msg):
		panel.ItemSelectDown()
	case slices.Contains(common.Hotkeys.DeleteItems, msg):
		return m.getDeleteTriggerCmd(false)
	case slices.Contains(common.Hotkeys.PermanentlyDeleteItems, msg):
		return m.getDeleteTriggerCmd(true)
	case slices.Contains(common.Hotkeys.CopyItems, msg):
		cmd := m.copyMultipleItem(false)
		panel.ChangeFilePanelMode()
		return cmd
	case slices.Contains(common.Hotkeys.CutItems, msg):
		cmd := m.copyMultipleItem(true)
		panel.ChangeFilePanelMode()
		return cmd
	case slices.Contains(common.Hotkeys.PasteItems, msg):
		cmd := m.getPasteItemCmd()
		panel.ChangeFilePanelMode()
		return cmd
	case slices.Contains(common.Hotkeys.CopyPath, msg):
		m.copyPath()
	case slices.Contains(common.Hotkeys.FilePanelSelectAllItem, msg):
		panel.SelectAllItem()
	}
	return nil
}

func (m *model) filePanelNormalModeKey(msg string) tea.Cmd {
	switch {
	case slices.Contains(common.Hotkeys.Confirm, msg):
		m.enterPanel()
	case slices.Contains(common.Hotkeys.ParentDirectory, msg):
		m.parentDirectory()
	case slices.Contains(common.Hotkeys.DeleteItems, msg):
		return m.getDeleteTriggerCmd(false)
	case slices.Contains(common.Hotkeys.PermanentlyDeleteItems, msg):
		return m.getDeleteTriggerCmd(true)
	case slices.Contains(common.Hotkeys.CopyItems, msg):
		return m.copySingleItem(false)
	case slices.Contains(common.Hotkeys.CutItems, msg):
		return m.copySingleItem(true)
	case slices.Contains(common.Hotkeys.FilePanelItemRename, msg):
		m.panelItemRename()
	case slices.Contains(common.Hotkeys.SearchBar, msg):
		m.searchBarFocus()
	case slices.Contains(common.Hotkeys.CopyPath, msg):
		m.copyPath()
	case slices.Contains(common.Hotkeys.CopyPWD, msg):
		m.copyPWD()
	}
	return nil
}

// Check the hotkey to cancel operation or create file
func (m *model) typingModalOpenKey(msg string) {
	switch {
	case slices.Contains(common.Hotkeys.CancelTyping, msg):
		m.typingModal.errorMesssage = ""
		m.cancelTypingModal()
	case slices.Contains(common.Hotkeys.ConfirmTyping, msg):
		m.createItem()
	}
}

func (m *model) notifyModelOpenKey(msg string) tea.Cmd {
	isCancel := slices.Contains(common.Hotkeys.CancelTyping, msg) || slices.Contains(common.Hotkeys.Quit, msg)
	isConfirm := slices.Contains(common.Hotkeys.ConfirmTyping, msg)

	if !isCancel && !isConfirm {
		slog.Warn("Invalid keypress in notifyModel", "msg", msg)
		return nil
	}
	m.notifyModel.Close()
	action := m.notifyModel.GetConfirmAction()
	if isCancel {
		return m.handleNotifyModelCancel(action)
	}
	return m.handleNotifyModelConfirm(action)
}

func (m *model) handleNotifyModelCancel(action notify.ConfirmActionType) tea.Cmd {
	switch action {
	case notify.RenameAction:
		m.cancelRename()
	case notify.QuitAction:
		m.modelQuitState = notQuitting
	case notify.DeleteAction, notify.NoAction, notify.PermanentDeleteAction:
		// Do nothing
	case notify.DeleteSSHConnectionAction:
		m.pendingSSHDeleteName = ""
	default:
		slog.Error("Unknown type of action", "action", action)
	}
	return nil
}

func (m *model) handleNotifyModelConfirm(action notify.ConfirmActionType) tea.Cmd {
	switch action {
	case notify.DeleteAction:
		cmd := m.getDeleteCmd(false)
		if panel := m.getFocusedFilePanel(); panel.PanelMode == filepanel.SelectMode {
			panel.ChangeFilePanelMode()
		}
		return cmd
	case notify.PermanentDeleteAction:
		cmd := m.getDeleteCmd(true)
		if panel := m.getFocusedFilePanel(); panel.PanelMode == filepanel.SelectMode {
			panel.ChangeFilePanelMode()
		}
		return cmd
	case notify.RenameAction:
		m.confirmRename()
	case notify.QuitAction:
		m.modelQuitState = quitConfirmationReceived
	case notify.NoAction:
		// Ignore
	case notify.DeleteSSHConnectionAction:
		name := m.pendingSSHDeleteName
		m.pendingSSHDeleteName = ""
		if name != "" {
			if err := backend.RemoveSSHConnection(name); err != nil {
				slog.Debug("Cannot delete SSH connection", "name", name, "error", err)
			}
		}
	default:
		slog.Error("Unknown type of action", "action", action)
	}
	return nil
}

func (m *model) spfErrorModelOpenKey(msg string) tea.Cmd {
	isAbort := slices.Contains(spferror.KeyAbort(), msg)
	isSkip := slices.Contains(spferror.KeySkip(), msg)

	if !isAbort && !isSkip {
		slog.Warn("Invalid keypress in spfErrorModel", "msg", msg)
		return nil
	}
	defer func() {
		slog.Debug("Unlock mutex for modal error window")
		m.mutexErrorModal.Unlock()
	}()
	state := m.spfError.Close()
	if state == nil {
		return nil
	}
	reqID := m.nextIoReqCnt()
	if isSkip {
		return func() tea.Msg { return state.Skip(m.runFileProcessor, reqID) }
	}
	return func() tea.Msg { return state.Abort(m.runFileProcessor, reqID) }
}

// Handles key inputs inside sort options menu
func (m *model) sortOptionsKey(msg string) {
	switch {
	case slices.Contains(common.Hotkeys.OpenSortOptionsMenu, msg):
		m.sortModal.Close()
	case slices.Contains(common.Hotkeys.Quit, msg):
		m.sortModal.Close()
	case slices.Contains(common.Hotkeys.Confirm, msg):
		m.confirmSortOptions()
	case slices.Contains(common.Hotkeys.ListUp, msg):
		m.sortModal.ListUp()
	case slices.Contains(common.Hotkeys.ListDown, msg):
		m.sortModal.ListDown()
	}
}

func (m *model) renamingKey(msg string) tea.Cmd {
	switch {
	case slices.Contains(common.Hotkeys.CancelTyping, msg):
		m.cancelRename()
	case slices.Contains(common.Hotkeys.ConfirmTyping, msg):
		if m.IsRenamingConflicting() {
			return m.warnModalForRenaming()
		}
		m.confirmRename()
	}

	return nil
}

func (m *model) sidebarRenamingKey(msg string) {
	switch {
	case slices.Contains(common.Hotkeys.CancelTyping, msg):
		m.sidebarModel.CancelSidebarRename()
	case slices.Contains(common.Hotkeys.ConfirmTyping, msg):
		m.sidebarModel.ConfirmSidebarRename()
	}
}

func (m *model) sidebarAddSSHKey(msg string) {
	switch {
	case slices.Contains(common.Hotkeys.CancelTyping, msg):
		m.sidebarModel.CancelAddSSH()
	case slices.Contains(common.Hotkeys.ConfirmTyping, msg):
		value := m.sidebarModel.ConfirmAddSSH()
		if value != "" {
			m.saveSSHConnection(value)
		}
	}
}

// saveSSHConnection parses the user input and saves an SSH connection to the
// superfile-specific TOML config file.
func (m *model) saveSSHConnection(input string) {
	conn := parseSSHInput(input)
	if err := backend.SaveSSHConnection(conn); err != nil {
		slog.Error("Error saving SSH connection", "error", err)
		return
	}
	slog.Debug("SSH connection saved", "name", conn.Name)
}

// parseSSHInput parses a user-provided SSH host string and returns an SSHConnection.
// It supports "user@host" and plain "host" formats.
func parseSSHInput(input string) backend.SSHConnection {
	conn := backend.SSHConnection{
		Port:     22,
		AuthType: "key",
		KeyPath:  backend.DefaultKeyPath(),
	}

	user, host, ok := strings.Cut(input, "@")
	if ok && host != "" {
		conn.User = user
		conn.Host = host
		conn.Name = input
	} else {
		conn.Host = input
		conn.Name = input
		conn.User = os.Getenv("USER")
	}
	return conn
}

// Check the key input and cancel or confirms the search
func (m *model) focusOnSearchbarKey(msg string) {
	switch {
	case slices.Contains(common.Hotkeys.CancelTyping, msg):
		m.cancelSearch()
	case slices.Contains(common.Hotkeys.ConfirmTyping, msg):
		m.confirmSearch()
	}
}
