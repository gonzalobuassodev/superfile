package backend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
)

// TransferProgress carries the current state of an in-progress file transfer.
type TransferProgress struct {
	FileName    string
	BytesCopied int64
	TotalBytes  int64
}

// ProgressCallback is called periodically during file transfers.
// It is safe to call from multiple goroutines; implementations should not block.
type ProgressCallback func(TransferProgress)

// defaultProgressInterval is the minimum time between progress callbacks
// to avoid flooding the channel while still showing smooth byte-level updates.
const (
	defaultProgressInterval = 100 * time.Millisecond
	defaultStallTimeout     = 15 * time.Second
)

// PermissionDeniedWithSudoError is returned when a remote file cannot be opened
// due to permission denied, and the filesystem supports retrying with sudo.
// The caller can use RootFS to retry the read via OpenAsRoot.
type PermissionDeniedWithSudoError struct {
	Path      string
	Wrapped   error
	RootFS    RootFileSystem
	LocalDest string
	Info      os.FileInfo
}

func (e *PermissionDeniedWithSudoError) Error() string {
	return fmt.Sprintf("permission denied: %s", e.Path)
}

func (e *PermissionDeniedWithSudoError) Unwrap() error {
	return e.Wrapped
}

// isPermissionDenied returns true if the error indicates a permission denied
// condition, either from the OS, SFTP protocol, or the error message.
func isPermissionDenied(err error) bool {
	if err == nil {
		return false
	}
	if os.IsPermission(err) {
		return true
	}
	// Check for SFTP status code 3 (SSH_FX_PERMISSION_DENIED)
	var se *sftp.StatusError
	if errors.As(err, &se) && se.Code == 3 {
		return true
	}
	// Fallback: check error string
	return strings.Contains(strings.ToLower(err.Error()), "permission denied")
}

// FormatTransferSize converts a byte count to a human-readable string (B/KB/MB/GB).
func FormatTransferSize(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1fGB", float64(bytes)/float64(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(bytes)/float64(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.0fKB", float64(bytes)/float64(1<<10))
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}

// ProgressReader wraps an io.Reader and reports progress via a callback.
// Reports at most once per minInterval so transfers feel smooth without flooding.
// When stallCh is non-nil, it sends a signal on every read to reset a stall timer.
// Thread-safe via internal mutex.
type ProgressReader struct {
	mu         sync.Mutex
	reader     io.Reader
	total      int64
	callback   ProgressCallback
	fileName   string
	minInterval time.Duration
	stallCh    chan<- struct{}

	// mutable state protected by mu
	copied     int64
	lastReport time.Time
	firedDone  bool
}

func (pr *ProgressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)

	pr.mu.Lock()
	pr.copied += int64(n)
	copied := pr.copied

	shouldReport := !pr.firedDone && pr.callback != nil && pr.total > 0
	if shouldReport {
		justFinished := copied >= pr.total
		if justFinished {
			pr.firedDone = true
		}
		if justFinished || time.Since(pr.lastReport) >= pr.minInterval {
			pr.lastReport = time.Now()
		} else {
			shouldReport = false
		}
	}
	pr.mu.Unlock()

	if n > 0 && pr.stallCh != nil {
		select {
		case pr.stallCh <- struct{}{}:
		default:
		}
	}

	if shouldReport {
		pr.callback(TransferProgress{
			FileName:    pr.fileName,
			BytesCopied: copied,
			TotalBytes:  pr.total,
		})
	}
	return n, err
}

// Download copies a file or directory from a remote filesystem to a local
// destination path. It uses the sourceFS to read and the local FS for writing.
func Download(sourceFS FileSystem, remotePath, localDest string) error {
	return DownloadWithProgress(sourceFS, remotePath, localDest, nil)
}

// DownloadWithProgress is like Download but reports progress via callback.
func DownloadWithProgress(sourceFS FileSystem, remotePath, localDest string, progress ProgressCallback) error {
	info, err := sourceFS.Stat(remotePath)
	if err != nil {
		return fmt.Errorf("failed to stat remote path %s: %w", remotePath, err)
	}

	if info.IsDir() {
		slog.Debug("DownloadWithProgress: is directory, calling downloadDir",
			"path", remotePath)
		err = downloadDir(sourceFS, remotePath, localDest, info, progress)
		slog.Debug("DownloadWithProgress: downloadDir returned",
			"path", remotePath, "error", err)
		return err
	}
	slog.Debug("DownloadWithProgress: is file, calling downloadFile",
		"path", remotePath)
	err = downloadFile(sourceFS, remotePath, localDest, info, progress)
	slog.Debug("DownloadWithProgress: downloadFile returned",
		"path", remotePath, "error", err)
	return err
}

func downloadFile(sourceFS FileSystem, remotePath, localDest string, info os.FileInfo, progress ProgressCallback) error {
	remoteFile, err := sourceFS.Open(remotePath)
	if err != nil {
		if isPermissionDenied(err) {
			if rootFS, ok := sourceFS.(RootFileSystem); ok {
				slog.Debug("Permission denied, retrying with sudo", "path", remotePath)
				rootReader, rootErr := rootFS.OpenAsRoot(remotePath)
				if rootErr != nil {
					slog.Error("Sudo retry also failed", "path", remotePath, "error", rootErr)
					return &PermissionDeniedWithSudoError{
						Path:      remotePath,
						Wrapped:   errors.Join(err, rootErr),
						RootFS:    rootFS,
						LocalDest: localDest,
						Info:      info,
					}
				}
				// Sudo retry succeeded — treat rootReader as the remote file
				remoteFile = rootReader
				// Remote file cleanup is handled via defer below; rootReader
				// wraps the SSH session, so remoteFile.Close() cleans it up.
			}
		}
		if remoteFile == nil {
			return fmt.Errorf("failed to open remote file %s: %w", remotePath, err)
		}
	}
	defer func() {
		done := make(chan struct{})
		go func() {
			remoteFile.Close()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			slog.Warn("downloadFile: remoteFile.Close timed out",
				"path", remotePath)
		}
	}()

	localFile, err := os.OpenFile(localDest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return fmt.Errorf("failed to create local file %s: %w", localDest, err)
	}
	defer localFile.Close()

	progressCh := make(chan struct{}, 64)

	var reader io.Reader = remoteFile
	if progress != nil && info.Size() > 0 {
		reader = &ProgressReader{
			reader:      remoteFile,
			total:       info.Size(),
			callback:    progress,
			fileName:    filepath.Base(remotePath),
			minInterval: defaultProgressInterval,
			stallCh:     progressCh,
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(localFile, reader)
		errCh <- copyErr
	}()

	go func() {
		stallTimer := time.NewTimer(defaultStallTimeout)
		defer stallTimer.Stop()
		for {
			select {
			case <-progressCh:
				if !stallTimer.Stop() {
					<-stallTimer.C
				}
				stallTimer.Reset(defaultStallTimeout)
		case <-stallTimer.C:
			slog.Warn("downloadFile stall timer fired — no progress for",
				"timeout", defaultStallTimeout, "path", remotePath)
			cancel()
			// Close the remote file to unblock the stuck Read in io.Copy
			remoteFile.Close()
			return
		case <-ctx.Done():
			return
		}
	}
}()

select {
case err := <-errCh:
	if err != nil {
		return fmt.Errorf("failed to copy remote file %s to %s: %w", remotePath, localDest, err)
	}
	cancel()
	slog.Debug("downloadFile completed successfully",
		"path", remotePath, "size", info.Size())
	return nil
case <-ctx.Done():
	errMsg := fmt.Errorf("download %s stalled: no progress for %v", filepath.Base(remotePath), defaultStallTimeout)
	slog.Warn("downloadFile timed out", "path", remotePath, "error", errMsg)
	return errMsg
}
}

func downloadDir(sourceFS FileSystem, remotePath, localDest string, info os.FileInfo, progress ProgressCallback) error {
	if err := os.MkdirAll(localDest, info.Mode()); err != nil {
		return fmt.Errorf("failed to create local directory %s: %w", localDest, err)
	}

	entries, err := sourceFS.ReadDir(remotePath)
	if err != nil {
		return fmt.Errorf("failed to read remote directory %s: %w", remotePath, err)
	}
	slog.Debug("downloadDir: ReadDir returned",
		"path", remotePath, "count", len(entries))

	for i, entry := range entries {
		srcPath := sourceFS.Join(remotePath, entry.Name())
		dstPath := filepath.Join(localDest, entry.Name())
		slog.Debug("downloadDir: processing entry",
			"i", i, "name", entry.Name(),
			"isDir", entry.IsDir(), "path", remotePath)

		if entry.IsDir() {
			entryInfo, err := sourceFS.Stat(srcPath)
			if err != nil {
				return fmt.Errorf("failed to stat remote path %s: %w", srcPath, err)
			}
			slog.Debug("downloadDir: recursing into subdirectory",
				"name", entry.Name(), "srcPath", srcPath)
			if err := downloadDir(sourceFS, srcPath, dstPath, entryInfo, progress); err != nil {
				return err
			}
			slog.Debug("downloadDir: subdirectory done",
				"name", entry.Name(), "srcPath", srcPath)
		} else {
			slog.Debug("downloadDir: calling downloadFile",
				"name", entry.Name(), "srcPath", srcPath)
			if err := downloadFile(sourceFS, srcPath, dstPath, entry, progress); err != nil {
				return err
			}
			slog.Debug("downloadDir: downloadFile done",
				"name", entry.Name(), "srcPath", srcPath)
		}
	}
	slog.Debug("downloadDir: all entries processed, returning nil",
		"path", remotePath)
	return nil
}

// Upload copies a file or directory from local filesystem to a remote
// destination path. It uses the local FS for reading and the targetFS for writing.
func Upload(targetFS FileSystem, localPath, remoteDest string) error {
	return UploadWithProgress(targetFS, localPath, remoteDest, nil)
}

// UploadWithProgress is like Upload but reports progress via callback.
func UploadWithProgress(targetFS FileSystem, localPath, remoteDest string, progress ProgressCallback) error {
	info, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("failed to stat local path %s: %w", localPath, err)
	}

	if info.IsDir() {
		return uploadDir(targetFS, localPath, remoteDest, info, progress)
	}
	return uploadFile(targetFS, localPath, remoteDest, info, progress)
}

func uploadFile(targetFS FileSystem, localPath, remoteDest string, info os.FileInfo, progress ProgressCallback) error {
	localFile, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("failed to open local file %s: %w", localPath, err)
	}
	defer localFile.Close()

	remoteFile, err := targetFS.Create(remoteDest)
	if err != nil {
		return fmt.Errorf("failed to create remote file %s: %w", remoteDest, err)
	}
	defer remoteFile.Close()

	// progressCh receives a signal every time data is transferred
	progressCh := make(chan struct{}, 64)

	var reader io.Reader = localFile
	if progress != nil && info.Size() > 0 {
		reader = &ProgressReader{
			reader:      localFile,
			total:       info.Size(),
			callback:    progress,
			fileName:    filepath.Base(localPath),
			minInterval: defaultProgressInterval,
			stallCh:     progressCh,
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(remoteFile, reader)
		errCh <- copyErr
	}()

	// Stall monitor: if no progressCh signal for defaultStallTimeout, cancel
	// progressCh is buffered so the io.Copy goroutine never blocks
	go func() {
		stallTimer := time.NewTimer(defaultStallTimeout)
		defer stallTimer.Stop()
		for {
			select {
			case <-progressCh:
				if !stallTimer.Stop() {
					<-stallTimer.C
				}
				stallTimer.Reset(defaultStallTimeout)
		case <-stallTimer.C:
			slog.Warn("uploadFile stall timer fired — no progress for",
				"timeout", defaultStallTimeout, "path", localPath)
			cancel()
			remoteFile.Close()
			return
		case <-ctx.Done():
			return
		}
	}
}()

select {
case err := <-errCh:
	if err != nil {
		return fmt.Errorf("failed to upload %s to %s: %w", localPath, remoteDest, err)
	}
	cancel() // signal stall monitor to stop
	slog.Debug("uploadFile completed successfully",
		"path", localPath, "size", info.Size())
	return nil
case <-ctx.Done():
	errMsg := fmt.Errorf("upload %s stalled: no progress for %v", filepath.Base(localPath), defaultStallTimeout)
	slog.Warn("uploadFile timed out", "path", localPath, "error", errMsg)
	return errMsg
}
}

func uploadDir(targetFS FileSystem, localPath, remoteDest string, info os.FileInfo, progress ProgressCallback) error {
	if err := targetFS.MkdirAll(remoteDest, info.Mode()); err != nil {
		return fmt.Errorf("failed to create remote directory %s: %w", remoteDest, err)
	}

	entries, err := os.ReadDir(localPath)
	if err != nil {
		return fmt.Errorf("failed to read local directory %s: %w", localPath, err)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(localPath, entry.Name())
		dstPath := targetFS.Join(remoteDest, entry.Name())

		entryInfo, err := entry.Info()
		if err != nil {
			return fmt.Errorf("failed to stat local entry %s: %w", srcPath, err)
		}

		if entryInfo.IsDir() {
			if err := uploadDir(targetFS, srcPath, dstPath, entryInfo, progress); err != nil {
				return err
			}
		} else {
			if err := uploadFile(targetFS, srcPath, dstPath, entryInfo, progress); err != nil {
				return err
			}
		}
	}
	return nil
}

// RemoteCopy copies files or directories from one remote filesystem to another.
// When sourceFS == targetFS, it uses Rename for cut operations within the same FS.
func RemoteCopy(sourceFS, targetFS FileSystem, srcPath, dstPath string) error {
	return RemoteCopyWithProgress(sourceFS, targetFS, srcPath, dstPath, nil)
}

// RemoteCopyWithProgress is like RemoteCopy but reports progress via callback.
func RemoteCopyWithProgress(sourceFS, targetFS FileSystem, srcPath, dstPath string, progress ProgressCallback) error {
	info, err := sourceFS.Stat(srcPath)
	if err != nil {
		return fmt.Errorf("failed to stat source path %s: %w", srcPath, err)
	}

	if info.IsDir() {
		return remoteCopyDir(sourceFS, targetFS, srcPath, dstPath, info, progress)
	}
	return remoteCopyFile(sourceFS, targetFS, srcPath, dstPath, info, progress)
}

func remoteCopyFile(sourceFS, targetFS FileSystem, srcPath, dstPath string, info os.FileInfo, progress ProgressCallback) error {
	srcFile, err := sourceFS.Open(srcPath)
	if err != nil {
		if isPermissionDenied(err) {
			if rootFS, ok := sourceFS.(RootFileSystem); ok {
				return &PermissionDeniedWithSudoError{
					Path:      srcPath,
					Wrapped:   err,
					RootFS:    rootFS,
					LocalDest: dstPath,
					Info:      info,
				}
			}
		}
		return fmt.Errorf("failed to open remote source file %s: %w", srcPath, err)
	}
	defer srcFile.Close()

	dstFile, err := targetFS.Create(dstPath)
	if err != nil {
		return fmt.Errorf("failed to create remote destination file %s: %w", dstPath, err)
	}
	defer dstFile.Close()

	progressCh := make(chan struct{}, 64)

	var reader io.Reader = srcFile
	if progress != nil && info.Size() > 0 {
		reader = &ProgressReader{
			reader:      srcFile,
			total:       info.Size(),
			callback:    progress,
			fileName:    filepath.Base(srcPath),
			minInterval: defaultProgressInterval,
			stallCh:     progressCh,
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(dstFile, reader)
		errCh <- copyErr
	}()

	go func() {
		stallTimer := time.NewTimer(defaultStallTimeout)
		defer stallTimer.Stop()
		for {
			select {
			case <-progressCh:
				if !stallTimer.Stop() {
					<-stallTimer.C
				}
				stallTimer.Reset(defaultStallTimeout)
		case <-stallTimer.C:
			slog.Warn("remoteCopyFile stall timer fired — no progress for",
				"timeout", defaultStallTimeout, "path", srcPath)
			cancel()
			// Close the destination file to unblock the stuck Write in io.Copy
			dstFile.Close()
			return
		case <-ctx.Done():
			return
		}
	}
}()

select {
case err := <-errCh:
	if err != nil {
		return fmt.Errorf("failed to copy remote file %s to %s: %w", srcPath, dstPath, err)
	}
	cancel()
	slog.Debug("remoteCopyFile completed successfully",
		"path", srcPath, "size", info.Size())
	return nil
case <-ctx.Done():
	errMsg := fmt.Errorf("remote copy %s stalled: no progress for %v", filepath.Base(srcPath), defaultStallTimeout)
	slog.Warn("remoteCopyFile timed out", "path", srcPath, "error", errMsg)
	return errMsg
}
}

// DeleteRemoteWithProgress removes files/directories from a remote FS,
// reporting per-file progress via the callback.
// For a single file it removes it directly; for a directory it walks the
// tree, removes each file individually with progress, then cleans up the
// now-empty directory tree with a single RemoveAll.
func DeleteRemoteWithProgress(fs FileSystem, rootPath string, progress ProgressCallback) error {
	info, err := fs.Stat(rootPath)
	if err != nil {
		return fmt.Errorf("failed to stat remote path %s: %w", rootPath, err)
	}

	// Single file — remove directly
	if !info.IsDir() {
		if err := fs.Remove(rootPath); err != nil {
			return fmt.Errorf("failed to delete remote file %s: %w", rootPath, err)
		}
		if progress != nil {
			progress(TransferProgress{
				FileName:    filepath.Base(rootPath),
				BytesCopied: 1,
				TotalBytes:  1,
			})
		}
		return nil
	}

	// Directory — single walk: collect file paths and count in one pass.
	// Then delete each file from the collected slice (no second walk).
	var filePaths []string
	err = fs.Walk(rootPath, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !info.IsDir() {
			filePaths = append(filePaths, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to walk remote path for counting: %w", err)
	}

	// Delete each file with progress (flat iteration, not a second walk)
	for i, path := range filePaths {
		if err := fs.Remove(path); err != nil {
			return fmt.Errorf("failed to delete remote file %s: %w", path, err)
		}
		if progress != nil {
			progress(TransferProgress{
				FileName:    filepath.Base(path),
				BytesCopied: int64(i + 1),
				TotalBytes:  int64(len(filePaths)),
			})
		}
	}

	// Remove the now-empty directory tree
	if err := fs.RemoveAll(rootPath); err != nil {
		return fmt.Errorf("failed to remove empty remote directory %s: %w", rootPath, err)
	}
	return nil
}

func remoteCopyDir(sourceFS, targetFS FileSystem, srcPath, dstPath string, info os.FileInfo, progress ProgressCallback) error {
	if err := targetFS.MkdirAll(dstPath, info.Mode()); err != nil {
		return fmt.Errorf("failed to create remote directory %s: %w", dstPath, err)
	}

	entries, err := sourceFS.ReadDir(srcPath)
	if err != nil {
		return fmt.Errorf("failed to read remote source directory %s: %w", srcPath, err)
	}

	for _, entry := range entries {
		srcEntry := sourceFS.Join(srcPath, entry.Name())
		dstEntry := targetFS.Join(dstPath, entry.Name())

		if entry.IsDir() {
			entryInfo, err := sourceFS.Stat(srcEntry)
			if err != nil {
				return fmt.Errorf("failed to stat remote entry %s: %w", srcEntry, err)
			}
			if err := remoteCopyDir(sourceFS, targetFS, srcEntry, dstEntry, entryInfo, progress); err != nil {
				return err
			}
		} else {
			if err := remoteCopyFile(sourceFS, targetFS, srcEntry, dstEntry, entry, progress); err != nil {
				return err
			}
		}
	}
	return nil
}
