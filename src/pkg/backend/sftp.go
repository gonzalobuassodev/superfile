package backend

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// SudoPasswordProvider is a callback that supplies a sudo password when
// OpenAsRoot detects that the remote sudo requires authentication.
// hostInfo is a human-readable identifier (e.g., "myserver" or "user@host:port").
// Returns the password and true if the user provided one, or ("", false) if cancelled.
type SudoPasswordProvider func(hostInfo string) (password string, ok bool)

// sftpFS implements FileSystem over an SFTP client connection.
type sftpFS struct {
	client               *sftp.Client
	sshClient            *ssh.Client
	name                 string // display name for this connection
	sudoPasswordProvider SudoPasswordProvider
}

// NewSFTPFileSystem creates a new SFTP-backed FileSystem from an existing
// *sftp.Client. The name parameter is a human-readable identifier shown in
// the UI (e.g., the connection name from config).
// NOTE: The returned FileSystem does not carry an SSH client, so OpenAsRoot
// will not be available. Use NewSFTPFileSystemWithSSH when sudo support is needed.
func NewSFTPFileSystem(client *sftp.Client, name string) FileSystem {
	return &sftpFS{
		client: client,
		name:   name,
	}
}

// NewSFTPFileSystemWithSSH creates a new SFTP-backed FileSystem that also
// holds the underlying SSH client, enabling privileged operations such as
// sudo cat via OpenAsRoot.
func NewSFTPFileSystemWithSSH(client *sftp.Client, sshClient *ssh.Client, name string) FileSystem {
	return &sftpFS{
		client:    client,
		sshClient: sshClient,
		name:      name,
	}
}

func (fs *sftpFS) ReadDir(path string) ([]os.FileInfo, error) {
	if fs.client == nil {
		return nil, errors.New("sftpFS: client is nil")
	}
	return fs.client.ReadDir(path)
}

func (fs *sftpFS) Stat(path string) (os.FileInfo, error) {
	if fs.client == nil {
		return nil, errors.New("sftpFS: client is nil")
	}
	return fs.client.Stat(path)
}

func (fs *sftpFS) Lstat(path string) (os.FileInfo, error) {
	if fs.client == nil {
		return nil, errors.New("sftpFS: client is nil")
	}
	return fs.client.Lstat(path)
}

func (fs *sftpFS) Open(path string) (io.ReadCloser, error) {
	if fs.client == nil {
		return nil, errors.New("sftpFS: client is nil")
	}
	return fs.client.Open(path)
}

func (fs *sftpFS) Create(path string) (io.WriteCloser, error) {
	if fs.client == nil {
		return nil, errors.New("sftpFS: client is nil")
	}
	return fs.client.Create(path)
}

func (fs *sftpFS) Remove(path string) error {
	if fs.client == nil {
		return errors.New("sftpFS: client is nil")
	}
	err := fs.client.Remove(path)
	if err != nil && isPermissionDenied(err) {
		slog.Debug("SFTP Remove failed with permission denied, retrying via sudo",
			"path", path)
		return fs.sudoRm(path)
	}
	return err
}

func (fs *sftpFS) RemoveAll(path string) error {
	if fs.client == nil {
		return errors.New("sftpFS: client is nil")
	}
	err := fs.sftpRemoveAll(path)
	if err != nil && isPermissionDenied(err) {
		slog.Debug("SFTP RemoveAll failed with permission denied, retrying via sudo",
			"path", path)
		return fs.sudoRm(path)
	}
	return err
}

// sftpRemoveAll performs the recursive remove via the SFTP protocol only.
// It walks the tree bottom-up so directories are empty when removed.
func (fs *sftpFS) sftpRemoveAll(path string) error {
	var entries []struct {
		path string
		dir  bool
	}
	walker := fs.client.Walk(path)
	for walker.Step() {
		if walker.Err() != nil {
			continue
		}
		entries = append(entries, struct {
			path string
			dir  bool
		}{walker.Path(), walker.Stat().IsDir()})
	}
	// Remove in reverse order (files first, then directories bottom-up)
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].dir {
			if err := fs.client.RemoveDirectory(entries[i].path); err != nil {
				return err
			}
		} else {
			if err := fs.client.Remove(entries[i].path); err != nil {
				return err
			}
		}
	}
	return nil
}

func (fs *sftpFS) Rename(oldPath, newPath string) error {
	if fs.client == nil {
		return errors.New("sftpFS: client is nil")
	}
	return fs.client.Rename(oldPath, newPath)
}

func (fs *sftpFS) MkdirAll(path string, perm os.FileMode) error {
	if fs.client == nil {
		return errors.New("sftpFS: client is nil")
	}
	return fs.client.MkdirAll(path)
}

func (fs *sftpFS) ReadLink(path string) (string, error) {
	if fs.client == nil {
		return "", errors.New("sftpFS: client is nil")
	}
	return fs.client.ReadLink(path)
}

// Walk implements filepath.Walk using the SFTP Walker.
func (fs *sftpFS) Walk(root string, walkFn filepath.WalkFunc) error {
	if fs.client == nil {
		return errors.New("sftpFS: client is nil")
	}
	walker := fs.client.Walk(root)
	for walker.Step() {
		if err := walkFn(walker.Path(), walker.Stat(), walker.Err()); err != nil {
			return err
		}
	}
	return nil
}

// sudoReadCloser reads from an SSH session's stdout and cleans up the
// session on Close. It waits for the remote command to complete on EOF
// so that any non-zero exit is surfaced as an error.
type sudoReadCloser struct {
	session  *ssh.Session
	reader   io.Reader
	closeErr error
	once     sync.Once
}

func (rc *sudoReadCloser) Read(p []byte) (int, error) {
	n, err := rc.reader.Read(p)
	if err == io.EOF {
		rc.closeErr = rc.session.Wait()
		if rc.closeErr != nil {
			return n, fmt.Errorf("sudo cat failed: %w", rc.closeErr)
		}
	}
	return n, err
}

func (rc *sudoReadCloser) Close() error {
	rc.once.Do(func() {
		if rc.closeErr == nil {
			slog.Debug("sudoReadCloser.Close: waiting for session")
			waitDone := make(chan error, 1)
			go func() {
				waitDone <- rc.session.Wait()
			}()
			select {
			case err := <-waitDone:
				rc.closeErr = err
				slog.Debug("sudoReadCloser.Close: session.Wait completed")
			case <-time.After(3 * time.Second):
				slog.Warn("sudoReadCloser.Close: session.Wait timed out, force-closing")
				rc.closeErr = errors.New("sudo cat session timed out")
			}
		}
		slog.Debug("sudoReadCloser.Close: closing session")
		rc.session.Close()
	})
	return rc.closeErr
}

// SetSudoPasswordProvider sets a callback that will be invoked when
// OpenAsRoot detects that the remote sudo requires a password.
func (fs *sftpFS) SetSudoPasswordProvider(provider SudoPasswordProvider) {
	fs.sudoPasswordProvider = provider
}

// sudoCheckNopasswd checks if the remote user has NOPASSWD sudo configured
// by running sudo -n true (non-interactive, no data transferred).
// Returns true if sudo is usable without a password.
func sudoCheckNopasswd(sess *ssh.Session) bool {
	err := sess.Run("sudo -n true 2>/dev/null")
	return err == nil
}

// OpenAsRoot opens the file at path for reading using sudo cat over SSH.
// This is used when a standard sftp.Open fails with permission denied.
// It requires the sftpFS to have been created with NewSFTPFileSystemWithSSH.
//
// It first checks with sudo -n true (lightweight, no file data) whether
// NOPASSWD is available. If yes, uses streaming sudo cat. If not, calls the
// SudoPasswordProvider and retries with sudo -S (streaming, no memory buffer).
func (fs *sftpFS) OpenAsRoot(path string) (io.ReadCloser, error) {
	if fs.sshClient == nil {
		return nil, errors.New("sftpFS: no SSH client available for sudo")
	}

	// Lightweight check: sudo -n true — no file data, fast round trip.
	checkSess, err := fs.sshClient.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create SSH session: %w", err)
	}
	nopasswd := sudoCheckNopasswd(checkSess)
	checkSess.Close()

	if nopasswd {
		return fs.sudoCat(path)
	}

	// Sudo requires a password — ask via the provider.
	if fs.sudoPasswordProvider == nil {
		return nil, errors.New("sudo password required but no password provider " +
			"configured; set a SudoPasswordProvider on the SFTP filesystem, " +
			"or configure NOPASSWD sudo for the remote user")
	}

	password, ok := fs.sudoPasswordProvider(fs.name)
	if !ok {
		return nil, errors.New("sudo password prompt cancelled by user")
	}

	return fs.sudoCatWithPassword(path, password)
}

// sudoCat runs sudo cat <path> with streaming (no password required).
// Always uses streaming so large files are not buffered in memory.
func (fs *sftpFS) sudoCat(path string) (io.ReadCloser, error) {
	sess, err := fs.sshClient.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create SSH session for sudo cat: %w", err)
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		sess.Close()
		return nil, fmt.Errorf("failed to get stdout pipe: %w", err)
	}
	if err := sess.Start("sudo cat " + sshQuote(path)); err != nil {
		sess.Close()
		return nil, fmt.Errorf("failed to exec sudo cat: %w", err)
	}
	return &sudoReadCloser{session: sess, reader: stdout}, nil
}

// sudoCatWithPassword runs sudo -S cat <path>, sending the password to stdin.
// Returns a streaming ReadCloser so large files are not buffered in memory.
func (fs *sftpFS) sudoCatWithPassword(path, password string) (io.ReadCloser, error) {
	sess, err := fs.sshClient.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create SSH session for password sudo: %w", err)
	}

	stdin, err := sess.StdinPipe()
	if err != nil {
		sess.Close()
		return nil, fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	stdout, err := sess.StdoutPipe()
	if err != nil {
		sess.Close()
		return nil, fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	if err := sess.Start("sudo -S cat " + sshQuote(path)); err != nil {
		sess.Close()
		return nil, fmt.Errorf("failed to exec sudo -S cat: %w", err)
	}

	// Send the password followed by newline to sudo's stdin prompt
	if _, err := fmt.Fprintf(stdin, "%s\n", password); err != nil {
		sess.Close()
		return nil, fmt.Errorf("failed to send sudo password: %w", err)
	}
	stdin.Close()

	return &sudoReadCloser{session: sess, reader: stdout}, nil
}

// sudoRm attempts to remove a file or directory tree using sudo over SSH.
// It first checks for NOPASSWD sudo, and if not available, calls the
// SudoPasswordProvider to prompt the user for credentials.
func (fs *sftpFS) sudoRm(path string) error {
	if fs.sshClient == nil {
		return errors.New("sftpFS: no SSH client available for sudo rm")
	}

	// Lightweight check: sudo -n true — no file data, fast round trip.
	checkSess, err := fs.sshClient.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create SSH session: %w", err)
	}
	nopasswd := sudoCheckNopasswd(checkSess)
	checkSess.Close()

	if nopasswd {
		return fs.sudoRmExec(path)
	}

	// Sudo requires a password — ask via the provider.
	if fs.sudoPasswordProvider == nil {
		return errors.New("sudo password required but no password provider " +
			"configured; set a SudoPasswordProvider on the SFTP filesystem, " +
			"or configure NOPASSWD sudo for the remote user")
	}

	password, ok := fs.sudoPasswordProvider(fs.name)
	if !ok {
		return errors.New("sudo password prompt cancelled by user")
	}

	return fs.sudoRmWithPassword(path, password)
}

// sudoRmExec runs sudo rm -rf over SSH (no password required).
func (fs *sftpFS) sudoRmExec(path string) error {
	sess, err := fs.sshClient.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create SSH session: %w", err)
	}
	defer sess.Close()

	var stderrBuf strings.Builder
	sess.Stderr = &stderrBuf

	if err := sess.Run("sudo rm -rf " + sshQuote(path)); err != nil {
		return fmt.Errorf("sudo rm failed: %w\n%s", err, stderrBuf.String())
	}
	return nil
}

// sudoRmWithPassword runs sudo -S rm -rf over SSH, sending the password
// to sudo's stdin prompt.
func (fs *sftpFS) sudoRmWithPassword(path, password string) error {
	sess, err := fs.sshClient.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create SSH session: %w", err)
	}
	defer sess.Close()

	stdin, err := sess.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	var stderrBuf strings.Builder
	sess.Stderr = &stderrBuf

	if err := sess.Start("sudo -S rm -rf " + sshQuote(path)); err != nil {
		stdin.Close()
		return fmt.Errorf("failed to exec sudo -S rm: %w", err)
	}

	// Send the password followed by newline to sudo's stdin prompt
	if _, err := fmt.Fprintf(stdin, "%s\n", password); err != nil {
		stdin.Close()
		return fmt.Errorf("failed to send sudo password: %w", err)
	}
	stdin.Close()

	if err := sess.Wait(); err != nil {
		return fmt.Errorf("sudo rm failed: %w\n%s", err, stderrBuf.String())
	}
	return nil
}

func (fs *sftpFS) Close() error {
	if fs.client == nil {
		return errors.New("sftpFS: client is nil")
	}
	return fs.client.Close()
}

func (fs *sftpFS) Join(elem ...string) string {
	return path.Join(elem...)
}

func (fs *sftpFS) Dir(p string) string {
	return path.Dir(p)
}

func (fs *sftpFS) Base(p string) string {
	return path.Base(p)
}

func (fs *sftpFS) Abs(p string) (string, error) {
	absPath := path.Clean(p)
	if path.IsAbs(absPath) {
		return absPath, nil
	}
	// For remote FS, treat relative paths as rooted at "/"
	return path.Clean("/" + p), nil
}

func (fs *sftpFS) Name() string {
	return fs.name
}

func (fs *sftpFS) IsLocal() bool {
	return false
}
