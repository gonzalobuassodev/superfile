package backend

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kevinburke/ssh_config"
	"github.com/pelletier/go-toml/v2"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// knownGitHosts are SSH hosts commonly used only for Git operations,
// not for interactive/SFTP access.
var knownGitHosts = map[string]bool{
	"github.com":    true,
	"gitlab.com":    true,
	"bitbucket.org": true,
}

// SSHConnection represents a saved SSH connection configuration.
type SSHConnection struct {
	Name     string `toml:"name"`
	Host     string `toml:"host"`
	Port     int    `toml:"port"`
	User     string `toml:"user"`
	AuthType string `toml:"auth_type"`          // "key" or "password"
	KeyPath  string `toml:"key_path,omitempty"` // path to SSH private key
}

// SSHConfig holds the TOML-serializable structure for the connections file.
type SSHConfig struct {
	Connection []SSHConnection `toml:"connection"`
}

// DefaultKeyPath returns the default SSH key path (~/.ssh/id_ed25519).
func DefaultKeyPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join("~", ".ssh", "id_ed25519")
	}
	return filepath.Join(home, ".ssh", "id_ed25519")
}

// LoadSSHConnections reads SSH connections from a TOML file.
// If the file does not exist, it returns an empty slice with no error.
func LoadSSHConnections(configPath string) ([]SSHConnection, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []SSHConnection{}, nil
		}
		return nil, fmt.Errorf("failed to read SSH config: %w", err)
	}

	var config SSHConfig
	if err := toml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse SSH config: %w", err)
	}

	// Apply defaults
	for i := range config.Connection {
		if config.Connection[i].Port == 0 {
			config.Connection[i].Port = 22
		}
		if config.Connection[i].AuthType == "" {
			config.Connection[i].AuthType = "key"
		}
		if config.Connection[i].KeyPath == "" {
			config.Connection[i].KeyPath = DefaultKeyPath()
		}
	}

	return config.Connection, nil
}

// UserSSHConfigPath returns the path to the user's SSH config file (~/.ssh/config).
var UserSSHConfigPath = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join("~", ".ssh", "config")
	}
	return filepath.Join(home, ".ssh", "config")
}

// isRoutableHost returns true if the SSH host pattern represents a routable host
// (not a wildcard, not a negation, not a known git-only host).
func isRoutableHost(pattern string) bool {
	// Skip wildcard/glob patterns
	if strings.ContainsAny(pattern, "*?") {
		return false
	}
	// Skip negations
	if strings.HasPrefix(pattern, "!") {
		return false
	}
	// Skip known Git-only hosts
	if knownGitHosts[pattern] {
		return false
	}
	return true
}

// LoadSSHConnectionsFromConfigFile parses the user's ~/.ssh/config file and
// returns routable SSH hosts as SSHConnection entries, with values resolved
// through all matching Host blocks (including wildcards for defaults).
func LoadSSHConnectionsFromConfigFile() ([]SSHConnection, error) {
	configPath := UserSSHConfigPath()
	f, err := os.Open(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []SSHConnection{}, nil
		}
		return nil, fmt.Errorf("failed to open SSH config: %w", err)
	}
	defer f.Close()

	cfg, err := ssh_config.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("failed to parse SSH config: %w", err)
	}

	seen := make(map[string]bool)
	var conns []SSHConnection

	for _, host := range cfg.Hosts {
		for _, pattern := range host.Patterns {
			alias := pattern.String()
			if !isRoutableHost(alias) || seen[alias] {
				continue
			}
			seen[alias] = true

			hostName, _ := cfg.Get(alias, "HostName")
			if hostName == "" {
				hostName = alias
			}

			portStr, _ := cfg.Get(alias, "Port")
			port := 22
			if portStr != "" {
				if p, err := strconv.Atoi(portStr); err == nil && p > 0 {
					port = p
				}
			}

			user, _ := cfg.Get(alias, "User")
			identityFile, _ := cfg.Get(alias, "IdentityFile")
			// Expand ~ to home directory in IdentityFile paths
			if strings.HasPrefix(identityFile, "~/") {
				home, homeErr := os.UserHomeDir()
				if homeErr == nil {
					identityFile = filepath.Join(home, identityFile[2:])
				}
			}

			conns = append(conns, SSHConnection{
				Name:     alias,
				Host:     hostName,
				Port:     port,
				User:     user,
				AuthType: "key",
				KeyPath:  identityFile,
			})
		}
	}

	// Apply defaults to any connections missing values
	for i := range conns {
		if conns[i].Port == 0 {
			conns[i].Port = 22
		}
		if conns[i].User == "" {
			conns[i].User = os.Getenv("USER")
		}
		if conns[i].AuthType == "" {
			conns[i].AuthType = "key"
		}
		if conns[i].KeyPath == "" {
			conns[i].KeyPath = DefaultKeyPath()
		}
	}

	return conns, nil
}

// DialWithKey connects to an SSH server using key-based authentication.
func DialWithKey(host string, port int, user string, keyPath string, timeout time.Duration) (*sftp.Client, error) {
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read SSH key %s: %w", keyPath, err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("failed to parse SSH key %s: %w", keyPath, err)
	}

	config := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO: use knownhosts
		Timeout:         timeout,
	}

	return dialSFTP(host, port, config)
}

// DialWithPassword connects to an SSH server using password authentication.
func DialWithPassword(host string, port int, user string, password string, timeout time.Duration) (*sftp.Client, error) {
	config := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.Password(password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO: use knownhosts
		Timeout:         timeout,
	}

	return dialSFTP(host, port, config)
}

// dialSFTP creates an SSH connection and returns an SFTP client.
func dialSFTP(host string, port int, config *ssh.ClientConfig) (*sftp.Client, error) {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	slog.Debug("Dialing SSH", "addr", addr, "user", config.User)

	sshClient, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, fmt.Errorf("SSH dial failed: %w", err)
	}

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		sshClient.Close()
		return nil, fmt.Errorf("SFTP client creation failed: %w", err)
	}

	return sftpClient, nil
}

// HostKeyCallbackWithKnownHosts creates an ssh.HostKeyCallback that validates
// against the known_hosts file at the given path.
func HostKeyCallbackWithKnownHosts(knownHostsPath string) (ssh.HostKeyCallback, error) {
	if knownHostsPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ssh.InsecureIgnoreHostKey(), nil
		}
		knownHostsPath = filepath.Join(home, ".ssh", "known_hosts")
	}

	callback, err := knownhosts.New(knownHostsPath)
	if err != nil {
		// If file doesn't exist yet, use a permissive callback
		if os.IsNotExist(err) {
			return ssh.InsecureIgnoreHostKey(), nil
		}
		return nil, fmt.Errorf("failed to load known_hosts: %w", err)
	}

	return callback, nil
}

// DefaultSSHTimeout is the default timeout for SSH connections.
const DefaultSSHTimeout = 10 * time.Second

// superfileUserSSHConfigPath returns the path to the superfile-specific
// SSH connections TOML file.
func superfileUserSSHConfigPath() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "superfile", "ssh_connections.toml")
	}
	return filepath.Join(configDir, "superfile", "ssh_connections.toml")
}

// SaveSSHConnection saves a single SSH connection to the superfile SSH
// connections file. It appends to existing connections or creates the file if
// it doesn't exist.
func SaveSSHConnection(conn SSHConnection) error {
	configPath := superfileUserSSHConfigPath()

	// Load existing connections
	existing, err := LoadSSHConnections(configPath)
	if err != nil {
		return fmt.Errorf("failed to load existing SSH connections: %w", err)
	}

	// Check for duplicates
	for _, c := range existing {
		if c.Name == conn.Name {
			return fmt.Errorf("connection %q already exists", conn.Name)
		}
	}

	// Append new connection
	existing = append(existing, conn)

	config := SSHConfig{Connection: existing}
	data, err := toml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal SSH config: %w", err)
	}

	// Ensure directory exists
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write SSH config: %w", err)
	}

	return nil
}

// RemoveSSHConnection removes an SSH connection by name from the superfile
// SSH connections TOML file. Returns an error if the connection is not found
// in the file (e.g., it comes from ~/.ssh/config).
func RemoveSSHConnection(name string) error {
	configPath := superfileUserSSHConfigPath()

	existing, err := LoadSSHConnections(configPath)
	if err != nil {
		return fmt.Errorf("failed to load SSH connections: %w", err)
	}

	// Find and remove the connection
	found := false
	var updated []SSHConnection
	for _, c := range existing {
		if c.Name == name {
			found = true
			continue
		}
		updated = append(updated, c)
	}

	if !found {
		return fmt.Errorf("connection %q not found in superfile config", name)
	}

	config := SSHConfig{Connection: updated}
	data, err := toml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal SSH config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write SSH config: %w", err)
	}

	return nil
}

// LoadUserSSHConnections returns SSH connections from both ~/.ssh/config and
// the superfile-specific TOML file, merged by connection name. TOML connections
// take precedence when names collide.
func LoadUserSSHConnections() ([]SSHConnection, error) {
	// Load from ~/.ssh/config
	sshConfigConns, err := LoadSSHConnectionsFromConfigFile()
	if err != nil {
		return nil, fmt.Errorf("failed to load ~/.ssh/config: %w", err)
	}

	// Load from superfile TOML
	tomlConns, err := LoadSSHConnections(superfileUserSSHConfigPath())
	if err != nil {
		return nil, fmt.Errorf("failed to load superfile SSH config: %w", err)
	}

	// Merge: ~/.ssh/config entries come first, then add any TOML-only names
	seen := make(map[string]bool, len(sshConfigConns)+len(tomlConns))
	merged := make([]SSHConnection, 0, len(sshConfigConns)+len(tomlConns))

	for _, c := range sshConfigConns {
		seen[c.Name] = true
		merged = append(merged, c)
	}

	for _, c := range tomlConns {
		if !seen[c.Name] {
			merged = append(merged, c)
		}
	}

	// Apply defaults to any connections that might be missing values
	for i := range merged {
		if merged[i].Port == 0 {
			merged[i].Port = 22
		}
		if merged[i].AuthType == "" {
			merged[i].AuthType = "key"
		}
		if merged[i].KeyPath == "" {
			merged[i].KeyPath = DefaultKeyPath()
		}
	}

	return merged, nil
}
