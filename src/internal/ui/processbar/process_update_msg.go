package processbar

type Cmd func() UpdateMsg

type UpdateMsg interface {
	Apply(m *Model) (Cmd, error)
	GetReqID() int
}

// TODO: Can we remove this duplication with model_msg ?
type BaseMsg struct {
	reqID int
}

func (msg BaseMsg) GetReqID() int {
	return msg.reqID
}

type newProcessMsg struct {
	BaseMsg

	NewProcess Process
}

func (msg newProcessMsg) Apply(m *Model) (Cmd, error) {
	return m.GetListenCmd(), m.AddProcess(msg.NewProcess)
}

type updateProcessMsg struct {
	BaseMsg

	NewProcess Process
}

func (msg updateProcessMsg) Apply(m *Model) (Cmd, error) {
	return m.GetListenCmd(), m.UpdateExistingProcess(msg.NewProcess)
}

// NewUpdateProcessMsg creates an exported UpdateMsg from a Process, so that
// external code can construct an updateProcessMsg and send it directly to the
// Bubbletea event loop via program.Send() (bypassing the channel).
func NewUpdateProcessMsg(p Process) UpdateMsg {
	return updateProcessMsg{NewProcess: p, BaseMsg: BaseMsg{reqID: -1}}
}

// Construction will be options UpdateName(), UpdateDone(), etc..

type stopListeningMsg struct {
	BaseMsg
}

func (msg stopListeningMsg) Apply(_ *Model) (Cmd, error) {
	//nolint:nilnil // This is a no-op apply.
	return nil, nil
}
