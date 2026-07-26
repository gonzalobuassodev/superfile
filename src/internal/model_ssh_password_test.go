package internal

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"charm.land/bubbles/v2/textinput"
	"github.com/yorukot/superfile/src/internal/common"
)

// --- Task 1.1: passwordModal struct exists on model ---

func TestPasswordModalStruct(t *testing.T) {
	td := t.TempDir()

	t.Run("passwordModal field exists on model", func(t *testing.T) {
		m := defaultTestModel(td)
		// Verify passwordModal is a zero-valued struct with expected fields
		assert.False(t, m.passwordModal.open, "passwordModal should start closed")
		assert.Empty(t, m.passwordModal.connectionName)
		assert.Empty(t, m.passwordModal.host)
		assert.Equal(t, 0, m.passwordModal.port)
		assert.Empty(t, m.passwordModal.user)
		assert.Empty(t, m.passwordModal.errorMesssage)
	})
}

// --- Task 1.2: Messages exist and route correctly ---

func TestSSHPasswordMessages(t *testing.T) {
	t.Run("SSHPasswordRequestMsg fields", func(t *testing.T) {
		msg := SSHPasswordRequestMsg{
			ConnectionName: "test-server",
			Host:           "192.168.1.1",
			Port:           22,
			User:           "admin",
		}
		assert.Equal(t, "test-server", msg.ConnectionName)
		assert.Equal(t, "192.168.1.1", msg.Host)
		assert.Equal(t, 22, msg.Port)
		assert.Equal(t, "admin", msg.User)
	})

	t.Run("SSHPasswordResponseMsg fields", func(t *testing.T) {
		msg := SSHPasswordResponseMsg{
			ConnectionName: "test-server",
			Error:          assert.AnError,
		}
		assert.Equal(t, "test-server", msg.ConnectionName)
		assert.Error(t, msg.Error)
		assert.Nil(t, msg.FS)
	})
}

// --- Task 2.1: GeneratePasswordTextInput ---

func TestGeneratePasswordTextInput(t *testing.T) {
	t.Run("returns focused text input with echo password mode", func(t *testing.T) {
		ti := common.GeneratePasswordTextInput()
		assert.Equal(t, textinput.EchoPassword, ti.EchoMode, "should use EchoPassword mode")
		assert.Equal(t, '*', ti.EchoCharacter, "should use '*' as echo character")
		assert.Equal(t, "Enter SSH password", ti.Placeholder, "should have correct placeholder")
	})
}

// --- Task 3.1: handleConnectToSSH fallback ---

func TestHandleConnectToSSH_FallbackToPasswordModal(t *testing.T) {
	td := t.TempDir()

	t.Run("handleConnectToSSH returns cmd that fails for non-existent connection", func(t *testing.T) {
		m := defaultTestModel(td)
		cmd := m.handleConnectToSSH("non-existent-connection")
		require.NotNil(t, cmd, "should return a tea.Cmd")

		msg := ExecuteTeaCmdWithTimeout(cmd, DefaultTestTimeout)
		require.NotNil(t, msg, "command should produce a message")

		connectedMsg, ok := msg.(SSHConnectedMsg)
		require.True(t, ok, "should produce SSHConnectedMsg")
		assert.Error(t, connectedMsg.Error)
		assert.Contains(t, connectedMsg.Error.Error(), "not found")
	})
}

// --- Task 4.1: passwordModalOpenKey ---

func TestPasswordModalOpenKey(t *testing.T) {
	td := t.TempDir()

	t.Run("password modal closed by default", func(t *testing.T) {
		m := defaultTestModel(td)
		assert.False(t, m.passwordModal.open)
	})

	t.Run("password modal can be opened and closed via escape", func(t *testing.T) {
		m := defaultTestModel(td)
		m.passwordModal.open = true
		m.passwordModal.connectionName = "test"
		m.passwordModal.host = "192.168.1.1"
		m.passwordModal.port = 22
		m.passwordModal.user = "testuser"
		m.passwordModal.textInput = common.GeneratePasswordTextInput()
		assert.True(t, m.passwordModal.open)

		// Send escape via key handler
		_ = TeaUpdate(m, tea.KeyPressMsg{Code: tea.KeyEsc})
		assert.False(t, m.passwordModal.open, "escape should close password modal")
		assert.Empty(t, m.passwordModal.textInput.Value(), "text input should be reset")
	})
}

// --- Task 5.1: passwordModalRender ---

func TestPasswordModalRender(t *testing.T) {
	td := t.TempDir()

	t.Run("render does not panic with password modal open", func(t *testing.T) {
		m := defaultTestModel(td)
		m.passwordModal.open = true
		m.passwordModal.connectionName = "test"
		m.passwordModal.host = "192.168.1.1"
		m.passwordModal.port = 22
		m.passwordModal.user = "testuser"
		m.passwordModal.textInput = common.GeneratePasswordTextInput()

		result := m.passwordModalRender()
		assert.NotEmpty(t, result)
	})

	t.Run("render shows error message when set", func(t *testing.T) {
		m := defaultTestModel(td)
		m.passwordModal.open = true
		m.passwordModal.connectionName = "test"
		m.passwordModal.host = "192.168.1.1"
		m.passwordModal.port = 22
		m.passwordModal.user = "testuser"
		m.passwordModal.textInput = common.GeneratePasswordTextInput()
		m.passwordModal.errorMesssage = "authentication failed"

		result := m.passwordModalRender()
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "authentication failed")
	})

	t.Run("render shows connection info header", func(t *testing.T) {
		m := defaultTestModel(td)
		m.passwordModal.open = true
		m.passwordModal.connectionName = "test-server"
		m.passwordModal.host = "10.0.0.1"
		m.passwordModal.port = 2222
		m.passwordModal.user = "admin"
		m.passwordModal.textInput = common.GeneratePasswordTextInput()

		result := m.passwordModalRender()
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "admin")
		assert.Contains(t, result, "10.0.0.1")
	})
}

// --- Task 3.3: SSHPasswordResponseMsg routing ---

func TestSSHPasswordResponseMsg_Routing(t *testing.T) {
	td := t.TempDir()

	t.Run("error response closes modal and stores error", func(t *testing.T) {
		m := defaultTestModel(td)
		m.passwordModal.open = true
		m.passwordModal.connectionName = "test"
		m.passwordModal.textInput = common.GeneratePasswordTextInput()

		msg := SSHPasswordResponseMsg{
			ConnectionName: "test",
			Error:          assert.AnError,
		}
		_ = TeaUpdate(m, msg)

		// Modal should reopen with error for retry
		assert.True(t, m.passwordModal.open, "password modal should reopen to show error")
		assert.NotEmpty(t, m.passwordModal.errorMesssage, "error message should be stored")
	})
}

// --- Overlay chain ---

func TestPasswordModalOverlayChain(t *testing.T) {
	td := t.TempDir()

	t.Run("password modal renders in overlay", func(t *testing.T) {
		m := defaultTestModel(td)
		m.passwordModal.open = true
		m.passwordModal.connectionName = "test"
		m.passwordModal.host = "host"
		m.passwordModal.port = 22
		m.passwordModal.user = "user"
		m.passwordModal.textInput = common.GeneratePasswordTextInput()

		view := m.viewContent()
		assert.NotEmpty(t, view)
	})

	t.Run("enter key triggers password auth", func(t *testing.T) {
		m := defaultTestModel(td)
		m.passwordModal.open = true
		m.passwordModal.connectionName = "test"
		m.passwordModal.host = "host"
		m.passwordModal.port = 22
		m.passwordModal.user = "user"
		m.passwordModal.textInput = common.GeneratePasswordTextInput()

		m.passwordModal.textInput.SetValue("mypassword")
		require.Equal(t, "mypassword", m.passwordModal.textInput.Value())
		m.passwordModal.textInput.Focus()

		cmd := TeaUpdate(m, tea.KeyPressMsg{Code: tea.KeyEnter})
		assert.False(t, m.passwordModal.open, "enter should close password modal")
		require.NotNil(t, cmd, "should return a command")

		msg := ExecuteTeaCmdWithTimeout(cmd, DefaultTestTimeout)
		require.NotNil(t, msg, "command should produce a message")

		responseMsg, ok := msg.(SSHPasswordResponseMsg)
		require.True(t, ok, "command should produce SSHPasswordResponseMsg")
		assert.Equal(t, "test", responseMsg.ConnectionName)
		assert.Error(t, responseMsg.Error)
	})
}
