package backend

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSSHConnection_Defaults(t *testing.T) {
	conn := SSHConnection{
		Name: "test",
		Host: "example.com",
		User: "admin",
	}
	assert.Equal(t, "test", conn.Name)
	assert.Equal(t, "example.com", conn.Host)
	assert.Equal(t, "admin", conn.User)
}

func TestDefaultKeyPath(t *testing.T) {
	path := DefaultKeyPath()
	assert.Contains(t, path, ".ssh")
	assert.Contains(t, path, "id_ed25519")
	assert.True(t, filepath.IsAbs(path))
}

func TestLoadSSHConnections_FileNotFound(t *testing.T) {
	conns, err := LoadSSHConnections("/tmp/nonexistent_ssh_config_12345.toml")
	require.NoError(t, err)
	assert.Empty(t, conns)
}

func TestLoadSSHConnections_InvalidToml(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "ssh_connections.toml")
	require.NoError(t, os.WriteFile(configPath, []byte("invalid toml [[[\n"), 0644))

	conns, err := LoadSSHConnections(configPath)
	assert.Error(t, err)
	assert.Empty(t, conns)
}

func TestSSHConnection_PortDefault(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "ssh_connections.toml")
	content := []byte(`[[connection]]
name = "test"
host = "192.168.1.1"
user = "admin"
auth_type = "key"
key_path = "/home/user/.ssh/id_rsa"
`)
	require.NoError(t, os.WriteFile(configPath, content, 0644))

	conns, err := LoadSSHConnections(configPath)
	require.NoError(t, err)
	require.Len(t, conns, 1)
	assert.Equal(t, "test", conns[0].Name)
	assert.Equal(t, "192.168.1.1", conns[0].Host)
	assert.Equal(t, 22, conns[0].Port, "default port should be 22")
	assert.Equal(t, "admin", conns[0].User)
}

func TestSSHConnection_ExplicitPort(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "ssh_connections.toml")
	content := []byte(`[[connection]]
name = "prod"
host = "10.0.0.1"
port = 2222
user = "deploy"
auth_type = "password"
key_path = ""
`)
	require.NoError(t, os.WriteFile(configPath, content, 0644))

	conns, err := LoadSSHConnections(configPath)
	require.NoError(t, err)
	require.Len(t, conns, 1)
	assert.Equal(t, 2222, conns[0].Port)
	assert.Equal(t, "password", conns[0].AuthType)
}

func TestSSHConnection_MultipleConnections(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "ssh_connections.toml")
	content := []byte(`[[connection]]
name = "server1"
host = "10.0.0.1"
user = "admin"

[[connection]]
name = "server2"
host = "10.0.0.2"
user = "root"
port = 2222
`)
	require.NoError(t, os.WriteFile(configPath, content, 0644))

	conns, err := LoadSSHConnections(configPath)
	require.NoError(t, err)
	require.Len(t, conns, 2)
	assert.Equal(t, "server1", conns[0].Name)
	assert.Equal(t, "server2", conns[1].Name)
}

func TestDialWithKey_InvalidPath(t *testing.T) {
	client, err := DialWithKey("localhost", 22, "test", "/nonexistent/key", time.Second)
	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "failed to read SSH key")
}

func TestDialWithPassword_InvalidHost(t *testing.T) {
	client, err := DialWithPassword("192.0.2.1", 22, "test", "password", time.Second)
	assert.Error(t, err)
	assert.Nil(t, client)
}

func TestHostKeyCallbackWithKnownHosts_EmptyPath(t *testing.T) {
	callback, err := HostKeyCallbackWithKnownHosts("")
	assert.NoError(t, err)
	assert.NotNil(t, callback)
}

func TestHostKeyCallbackWithKnownHosts_NotFound(t *testing.T) {
	callback, err := HostKeyCallbackWithKnownHosts("/nonexistent/known_hosts_file")
	assert.NoError(t, err)
	assert.NotNil(t, callback)
}

func TestDefaultSSHTimeout(t *testing.T) {
	assert.Equal(t, 10*time.Second, DefaultSSHTimeout)
}

func TestSSHConnection_EmptyKeyPathAppliesDefault(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "ssh_connections.toml")
	content := []byte(`[[connection]]
name = "test"
host = "example.com"
user = "admin"
`)
	require.NoError(t, os.WriteFile(configPath, content, 0644))

	conns, err := LoadSSHConnections(configPath)
	require.NoError(t, err)
	require.Len(t, conns, 1)
	assert.NotEmpty(t, conns[0].KeyPath, "empty key_path should default to DefaultKeyPath")
	assert.Contains(t, conns[0].KeyPath, "id_ed25519")
}

func TestIsRoutableHost(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{"myserver", true},
		{"waugi-mail-vps", true},
		{"contabo", true},
		{"*", false},
		{"*.example.com", false},
		{"web-*", false},
		{"!github.com", false},
		{"github.com", false},
		{"gitlab.com", false},
		{"bitbucket.org", false},
	}
	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			assert.Equal(t, tt.want, isRoutableHost(tt.pattern))
		})
	}
}

func TestUserSSHConfigPath(t *testing.T) {
	path := UserSSHConfigPath()
	assert.Contains(t, path, ".ssh")
	assert.Contains(t, path, "config")
	assert.True(t, filepath.IsAbs(path))
}

func TestLoadSSHConnectionsFromConfigFile_FileNotFound(t *testing.T) {
	// Temporarily override home dir to a non-existent path
	orig := UserSSHConfigPath
	t.Cleanup(func() { UserSSHConfigPath = orig })

	UserSSHConfigPath = func() string {
		return "/tmp/nonexistent_ssh_config_12345"
	}

	conns, err := LoadSSHConnectionsFromConfigFile()
	require.NoError(t, err)
	assert.Empty(t, conns)
}

func TestLoadSSHConnectionsFromConfigFile_ParsesHosts(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "ssh_config")
	content := []byte(`
Host waugi-mail-vps
    HostName lansismail.com.ar
    User gon
    Port 22
    IdentityFile ~/.ssh/waugi-mail-vps

Host contabo
    HostName 213.136.67.130
    User root

Host github.com
    User git

Host *.prod
    User deploy

# Defaults at the end — first match wins in SSH
Host *
    IdentityFile ~/.ssh/id_ed25519
    User defaultuser
`)
	require.NoError(t, os.WriteFile(configPath, content, 0644))

	orig := UserSSHConfigPath
	t.Cleanup(func() { UserSSHConfigPath = orig })
	UserSSHConfigPath = func() string { return configPath }

	conns, err := LoadSSHConnectionsFromConfigFile()
	require.NoError(t, err)

	// Should find: waugi-mail-vps, contabo
	// Should skip: * (wildcard), github.com (git-only), *.prod (wildcard)
	require.Len(t, conns, 2, "expected 2 routable hosts")

	homeDir, _ := os.UserHomeDir()

	waugi := findConnByName(t, conns, "waugi-mail-vps")
	assert.Equal(t, "lansismail.com.ar", waugi.Host)
	assert.Equal(t, 22, waugi.Port)
	assert.Equal(t, "gon", waugi.User)
	assert.Equal(t, filepath.Join(homeDir, ".ssh/waugi-mail-vps"), waugi.KeyPath)

	contabo := findConnByName(t, conns, "contabo")
	assert.Equal(t, "213.136.67.130", contabo.Host)
	assert.Equal(t, 22, contabo.Port, "default port should be 22")
	assert.Equal(t, "root", contabo.User)
	assert.Equal(t, filepath.Join(homeDir, ".ssh/id_ed25519"), contabo.KeyPath, "should inherit from wildcard default and expand ~")
}

func TestLoadSSHConnectionsFromConfigFile_SkipsWildcardPatterns(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "ssh_config_wildcards")
	content := []byte(`
Host *
    IdentityFile ~/.ssh/id_ed25519

Host vpn-*
    HostName vpn.example.com

Host !forbidden
    HostName forbidden.example.com

Host valid-host
    HostName actual.example.com
    User test
`)
	require.NoError(t, os.WriteFile(configPath, content, 0644))

	orig := UserSSHConfigPath
	t.Cleanup(func() { UserSSHConfigPath = orig })
	UserSSHConfigPath = func() string { return configPath }

	conns, err := LoadSSHConnectionsFromConfigFile()
	require.NoError(t, err)
	require.Len(t, conns, 1, "should only find valid-host")
	assert.Equal(t, "valid-host", conns[0].Name)
	assert.Equal(t, "actual.example.com", conns[0].Host)
}

func findConnByName(t *testing.T, conns []SSHConnection, name string) SSHConnection {
	t.Helper()
	for _, c := range conns {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("connection %q not found", name)
	return SSHConnection{}
}
