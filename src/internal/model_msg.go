package internal

import (
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/yorukot/superfile/src/internal/ui/metadata"
	"github.com/yorukot/superfile/src/internal/ui/notify"
	"github.com/yorukot/superfile/src/internal/ui/processbar"
	"github.com/yorukot/superfile/src/internal/ui/spferror"
	"github.com/yorukot/superfile/src/pkg/backend"
)

// ConnectToSSHMsg requests a connection to an SSH server.
type ConnectToSSHMsg struct {
	ConnectionName string
}

// SSHConnectedMsg is sent after an SSH connection attempt completes.
type SSHConnectedMsg struct {
	ConnectionName string
	FS             backend.FileSystem
	Error          error
}

// SSHDisconnectedMsg is sent when an SSH connection is closed.
type SSHDisconnectedMsg struct {
	ConnectionName string
}

// RemoteDirLoadedMsg is sent when a remote directory listing finishes loading.
type RemoteDirLoadedMsg struct {
	PanelIndex int
	Location   string
	Elements   []os.FileInfo
	Error      error
}

// BaseMessage provides a reqID for operation tracking.
type BaseMessage struct {
	reqID int
}

func (m BaseMessage) GetReqID() int {
	return m.reqID
}

// ProcessBarUpdateMsg wraps a processbar.UpdateMsg to pass through the
// Bubbletea message loop.
type ProcessBarUpdateMsg struct {
	pMsg processbar.UpdateMsg
	BaseMessage
}

// ModelUpdateMessage is an interface for messages that apply directly
// to the model. All types that embed BaseMessage and can be applied
// to the model should implement this interface.
type ModelUpdateMessage interface {
	ApplyToModel(m *model) tea.Cmd
	GetReqID() int
}

// --- ProcessBar Operation Result Messages ---

// DeleteOperationMsg is sent when a delete operation completes.
type DeleteOperationMsg struct {
	State processbar.ProcessState
	BaseMessage
}

func (m DeleteOperationMsg) ApplyToModel(model *model) tea.Cmd {
	// Force the focused panel to refresh its directory listing so deleted
	// files disappear immediately. For remote (SFTP) panels this resets
	// LastLoadedLocation so the async reload in updateModelStateAfterMsg
	// picks it up; for local panels it resets LastTimeGetElement so the
	// next frame re-reads the directory.
	panel := model.getFocusedFilePanel()
	if m.State == processbar.Successful {
		panel.LastLoadedLocation = "" // force re-read on next cycle
		panel.LastTimeGetElement = time.Time{}
	}
	return processCmdToTeaCmd(model.processBarModel.GetListenCmd())
}

// PasteOperationMsg is sent when a paste operation completes.
type PasteOperationMsg struct {
	State processbar.ProcessState
	BaseMessage
}

func (m PasteOperationMsg) ApplyToModel(model *model) tea.Cmd {
	panel := model.getFocusedFilePanel()
	if m.State == processbar.Successful {
		panel.LastLoadedLocation = ""
		panel.LastTimeGetElement = time.Time{}
		if model.clipboard.IsCut() {
			model.clipboard.Reset(false)
		}
	}
	return processCmdToTeaCmd(model.processBarModel.GetListenCmd())
}

// ExtractOperationMsg is sent when an extract operation completes.
type ExtractOperationMsg struct {
	State processbar.ProcessState
	BaseMessage
}

func (m ExtractOperationMsg) ApplyToModel(model *model) tea.Cmd {
	panel := model.getFocusedFilePanel()
	if m.State == processbar.Successful {
		panel.LastLoadedLocation = ""
		panel.LastTimeGetElement = time.Time{}
	}
	return processCmdToTeaCmd(model.processBarModel.GetListenCmd())
}

// CompressOperationMsg is sent when a compress operation completes.
type CompressOperationMsg struct {
	State processbar.ProcessState
	BaseMessage
}

func (m CompressOperationMsg) ApplyToModel(model *model) tea.Cmd {
	panel := model.getFocusedFilePanel()
	if m.State == processbar.Successful {
		panel.LastLoadedLocation = ""
		panel.LastTimeGetElement = time.Time{}
	}
	return processCmdToTeaCmd(model.processBarModel.GetListenCmd())
}

// --- Modal Messages ---

// SpfErrorModalUpdateMsg is sent when an error modal needs to be shown.
type SpfErrorModalUpdateMsg struct {
	ErrorModel spferror.Model
	BaseMessage
}

func (m SpfErrorModalUpdateMsg) ApplyToModel(model *model) tea.Cmd {
	model.spfError = m.ErrorModel
	model.spfError.Open()
	return processCmdToTeaCmd(model.processBarModel.GetListenCmd())
}

// NotifyModalUpdateMsg is sent when a notification should be shown.
type NotifyModalUpdateMsg struct {
	NotifyModel notify.Model
	BaseMessage
}

func (m NotifyModalUpdateMsg) ApplyToModel(model *model) tea.Cmd {
	model.notifyModel = m.NotifyModel
	return processCmdToTeaCmd(model.processBarModel.GetListenCmd())
}

// MetadataUpdateMsg is sent when metadata has been fetched for a file.
type MetadataUpdateMsg struct {
	Meta    metadata.Metadata
	Focused bool
	BaseMessage
}

func (m MetadataUpdateMsg) ApplyToModel(model *model) tea.Cmd {
	model.fileMetaData.SetMetadataCache(m.Meta, m.Focused)
	return processCmdToTeaCmd(model.processBarModel.GetListenCmd())
}

// --- Constructor Functions ---

// NewDeleteOperationMsg creates a DeleteOperationMsg.
func NewDeleteOperationMsg(state processbar.ProcessState, reqID int) tea.Msg {
	return DeleteOperationMsg{
		State:       state,
		BaseMessage: BaseMessage{reqID: reqID},
	}
}

// NewPasteOperationMsg creates a PasteOperationMsg.
func NewPasteOperationMsg(state processbar.ProcessState, reqID int) tea.Msg {
	return PasteOperationMsg{
		State:       state,
		BaseMessage: BaseMessage{reqID: reqID},
	}
}

// NewExtractOperationMsg creates an ExtractOperationMsg.
func NewExtractOperationMsg(state processbar.ProcessState, reqID int) tea.Msg {
	return ExtractOperationMsg{
		State:       state,
		BaseMessage: BaseMessage{reqID: reqID},
	}
}

// NewCompressOperationMsg creates a CompressOperationMsg.
func NewCompressOperationMsg(state processbar.ProcessState, reqID int) tea.Msg {
	return CompressOperationMsg{
		State:       state,
		BaseMessage: BaseMessage{reqID: reqID},
	}
}

// NewSpfErrorModalMsg creates an SpfErrorModalUpdateMsg.
func NewSpfErrorModalMsg(errorModel spferror.Model, reqID int) tea.Msg {
	return SpfErrorModalUpdateMsg{
		ErrorModel:  errorModel,
		BaseMessage: BaseMessage{reqID: reqID},
	}
}

// NewNotifyModalMsg creates a NotifyModalUpdateMsg.
func NewNotifyModalMsg(notifyModel notify.Model, reqID int) tea.Msg {
	return NotifyModalUpdateMsg{
		NotifyModel: notifyModel,
		BaseMessage: BaseMessage{reqID: reqID},
	}
}

// NewMetadataMsg creates a MetadataUpdateMsg.
func NewMetadataMsg(meta metadata.Metadata, focused bool, reqID int) tea.Msg {
	return MetadataUpdateMsg{
		Meta:        meta,
		Focused:     focused,
		BaseMessage: BaseMessage{reqID: reqID},
	}
}
