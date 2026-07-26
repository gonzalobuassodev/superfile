package internal

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/yorukot/superfile/src/internal/ui/processbar"
	"github.com/yorukot/superfile/src/pkg/backend"
	"github.com/yorukot/superfile/src/pkg/utils"
)

// isSamePartition checks if two paths are on the same filesystem partition
func isSamePartition(path1, path2 string) (bool, error) {
	// Get the absolute path to handle relative paths
	absPath1, err := filepath.Abs(path1)
	if err != nil {
		return false, fmt.Errorf("failed to get absolute path of the first path: %w", err)
	}

	absPath2, err := filepath.Abs(path2)
	if err != nil {
		return false, fmt.Errorf("failed to get absolute path of the second path: %w", err)
	}

	if runtime.GOOS == utils.OsWindows {
		// On Windows, we can check if both paths are on the same drive (same letter)
		drive1 := getDriveLetter(absPath1)
		drive2 := getDriveLetter(absPath2)
		return drive1 == drive2, nil
	}

	// For Unix-like systems, we use the same path to check the root partition
	return filepath.VolumeName(absPath1) == filepath.VolumeName(absPath2), nil
}

// getDriveLetter extracts the drive letter from a Windows path
func getDriveLetter(path string) string {
	// Windows paths are usually like "C:\path\to\file"
	// So we need to extract the drive letter (e.g., "C")
	return strings.ToUpper(string(path[0]))
}

// moveElement moves a file or directory efficiently
func moveElement(src, dst string) error {
	// Check if source and destination are on the same partition
	sameDev, err := isSamePartition(src, dst)
	if err != nil {
		return fmt.Errorf("failed to check partitions: %w", err)
	}

	// If on the same partition, attempt to rename (which will use the same inode)
	if sameDev {
		if err = os.Rename(src, dst); err == nil {
			return nil
		}
		// If rename fails, fall back to copy+delete
	}

	// If on different partitions or rename failed, fall back to copy+delete
	err = copyElement(src, dst)
	if err != nil {
		return fmt.Errorf("failed to copy: %w", err)
	}

	err = os.RemoveAll(src)
	if err != nil {
		return fmt.Errorf("failed to remove source after copy: %w", err)
	}

	return nil
}

// copyElement handles copying of both files and directories
func copyElement(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("failed to stat source: %w", err)
	}

	if srcInfo.IsDir() {
		return copyDir(src, dst, srcInfo)
	}
	return copyFile(src, dst, srcInfo)
}

// copyDir recursively copies a directory
func copyDir(src, dst string, srcInfo os.FileInfo) error {
	err := os.MkdirAll(dst, srcInfo.Mode())
	if err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("failed to read source directory: %w", err)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		entryInfo, err := entry.Info()
		if err != nil {
			return fmt.Errorf("failed to get entry info: %w", err)
		}

		if entryInfo.IsDir() {
			err = copyDir(srcPath, dstPath, entryInfo)
		} else {
			err = copyFile(srcPath, dstPath, entryInfo)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// copyFile copies a single file
func copyFile(src, dst string, srcInfo os.FileInfo) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy file contents: %w", err)
	}
	return nil
}

// pasteDir handles directory copying with progress tracking
func pasteDir(src, dst string, p *processbar.Process, cut bool, processBarModel *processbar.Model) error {
	dst, err := renameIfDuplicate(dst)
	if err != nil {
		return err
	}

	// Check if we can do a fast move within the same partition
	sameDev, err := isSamePartition(src, dst)
	if err == nil && sameDev && cut {
		// For cut operations on same partition, try fast rename first
		err = os.Rename(src, dst)
		if err == nil {
			return nil
		}
		// If rename fails, fall back to manual copy
	}

	err = filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		newPath := filepath.Join(dst, relPath)
		return actualPasteOperation(info, path, newPath, cut, sameDev, p, processBarModel)
	})

	if err != nil {
		return err
	}

	// If this was a cut operation and we had to do a manual copy, remove the source
	if cut && !sameDev {
		err = os.RemoveAll(src)
		if err != nil {
			return fmt.Errorf("failed to remove source after move: %w", err)
		}
	}

	return nil
}

func actualPasteOperation(info os.FileInfo, path string, newPath string, cut bool, sameDev bool,
	p *processbar.Process, processBarModel *processbar.Model) error {
	var err error
	if info.IsDir() {
		// TODO - this is likely not needed because we did
		// dst, err := renameIfDuplicate(dst) above
		newPath, err = renameIfDuplicate(newPath)
		if err != nil {
			return err
		}
		err = os.MkdirAll(newPath, info.Mode())
		return err
	}

	// File
	p.CurrentFile = filepath.Base(path)
	if cut && sameDev {
		err = os.Rename(path, newPath)
	} else {
		err = copyFile(path, newPath, info)
	}

	if err != nil {
		p.State = processbar.Failed
		pSendErr := processBarModel.SendUpdateProcessMsg(*p, true)
		if pSendErr != nil {
			slog.Error("Error sending process update", "error", pSendErr)
		}
		return err
	}

	p.Done++
	processBarModel.TrySendingUpdateProcessMsg(*p)
	return nil
}

// remoteMoveElement moves (renames) a file or directory within a remote FS.
func remoteMoveElement(fs backend.FileSystem, src, dst string) error {
	return fs.Rename(src, dst)
}

// remotePasteDir handles pasting to/from remote filesystems.
// When sourceFS == nil, it's an upload (local→remote).
// When targetFS == nil, it's a download (remote→local).
// When both are non-nil, it's a remote→remote copy.
func remotePasteDir(sourceFS, targetFS backend.FileSystem, src, dst string,
	p *processbar.Process, cut bool, processBarModel *processbar.Model) error {
	dst = renameIfDuplicateLocal(dst)
	slog.Debug("remotePasteDir: start",
		"src", src, "dst", dst, "cut", cut,
		"sourceFS", sourceFS != nil, "targetFS", targetFS != nil)

	// Build progress callback that updates the process bar.
	// Fires every ~100ms per file so the user sees byte/MB progress
	// tick up continuously. Done++ fires exactly once per completed file
	// (the ProgressReader deduplicates the final callback).
	progressCb := func(tp backend.TransferProgress) {
		if tp.TotalBytes > 0 {
			copied := backend.FormatTransferSize(tp.BytesCopied)
			total := backend.FormatTransferSize(tp.TotalBytes)
			p.CurrentFile = fmt.Sprintf("%s (%s/%s)", tp.FileName, copied, total)
			if tp.BytesCopied >= tp.TotalBytes {
				p.Done++
			}
		} else {
			// Empty file — mark it done immediately
			p.CurrentFile = tp.FileName
			p.Done++
		}
		processBarModel.TrySendingUpdateProcessMsg(*p)
	}

	if sourceFS == nil && targetFS != nil {
		// Local → Remote (upload)
		if err := backend.UploadWithProgress(targetFS, src, dst, progressCb); err != nil {
			slog.Debug("remotePasteDir: upload failed", "src", src, "error", err)
			return err
		}
		if cut {
			slog.Debug("remotePasteDir: cut=true, removing local source after upload",
				"src", src)
			if err := os.RemoveAll(src); err != nil {
				slog.Debug("remotePasteDir: remove after upload failed", "src", src, "error", err)
				return fmt.Errorf("failed to remove source after upload: %w", err)
			}
			slog.Debug("remotePasteDir: local source removed after upload", "src", src)
		}
		slog.Debug("remotePasteDir: upload done, returning nil", "src", src)
		return nil
	}

	if sourceFS != nil && targetFS == nil {
		// Remote → Local (download)
		slog.Debug("remotePasteDir: starting DownloadWithProgress",
			"src", src, "dst", dst, "cut", cut)
		if err := backend.DownloadWithProgress(sourceFS, src, dst, progressCb); err != nil {
			slog.Debug("remotePasteDir: DownloadWithProgress failed",
				"src", src, "error", err)
			return err
		}
		slog.Debug("remotePasteDir: DownloadWithProgress returned successfully",
			"src", src, "dst", dst, "cut", cut)
		if cut {
			slog.Debug("remotePasteDir: cut=true, removing remote source after download",
				"src", src)
			if err := sourceFS.RemoveAll(src); err != nil {
				slog.Debug("remotePasteDir: remove after download failed",
					"src", src, "error", err)
				return fmt.Errorf("failed to remove remote source after download: %w", err)
			}
			slog.Debug("remotePasteDir: remote source removed after download", "src", src)
		}
		slog.Debug("remotePasteDir: download done, returning nil",
			"src", src, "cut", cut)
		return nil
	}

	// Remote → Remote
	if cut && sameRemoteFS(sourceFS, targetFS) {
		slog.Debug("remotePasteDir: same-FS rename", "src", src, "dst", dst)
		if err := targetFS.Rename(src, dst); err != nil {
			return err
		}
		slog.Debug("remotePasteDir: rename done, returning nil", "src", src)
		return nil
	}
	slog.Debug("remotePasteDir: starting RemoteCopyWithProgress",
		"src", src, "dst", dst, "cut", cut)
	if err := backend.RemoteCopyWithProgress(sourceFS, targetFS, src, dst, progressCb); err != nil {
		slog.Debug("remotePasteDir: RemoteCopyWithProgress failed", "src", src, "error", err)
		return err
	}
	slog.Debug("remotePasteDir: RemoteCopyWithProgress returned successfully",
		"src", src, "dst", dst, "cut", cut)
	if cut {
		slog.Debug("remotePasteDir: cut=true, removing remote source after copy",
			"src", src)
		if err := sourceFS.RemoveAll(src); err != nil {
			slog.Debug("remotePasteDir: remove after copy failed", "src", src, "error", err)
			return fmt.Errorf("failed to remove remote source after copy: %w", err)
		}
		slog.Debug("remotePasteDir: remote source removed after copy", "src", src)
	}
	slog.Debug("remotePasteDir: copy done, returning nil", "src", src)
	return nil
}

// renameIfDuplicateLocal checks for duplicate names on the local filesystem
// and appends a suffix if needed. For remote destinations, use the FS's own
// duplicate check.
func renameIfDuplicateLocal(dst string) string {
	dst, _ = renameIfDuplicate(dst)
	return dst
}

// sameRemoteFS returns true if both FS pointers refer to the same instance.
func sameRemoteFS(a, b backend.FileSystem) bool {
	if a == nil || b == nil {
		return false
	}
	return a == b
}

// isAncestor checks if dst is the same as src or a subdirectory of src.
// It handles symlinks by resolving them and applies case-insensitive comparison on Windows.
func isAncestor(src, dst string) bool {
	// Resolve symlinks for both paths
	srcResolved, err := filepath.EvalSymlinks(src)
	if err != nil {
		// If we can't resolve symlinks, fall back to original path
		srcResolved = src
	}

	dstResolved, err := filepath.EvalSymlinks(dst)
	if err != nil {
		// If we can't resolve symlinks, fall back to original path
		dstResolved = dst
	}

	// Get absolute paths. Abs() also Cleans paths to normalize separators and resolve . and ..
	srcAbs, err := filepath.Abs(srcResolved)
	if err != nil {
		return false
	}

	dstAbs, err := filepath.Abs(dstResolved)
	if err != nil {
		return false
	}

	// On Windows, perform case-insensitive comparison
	if runtime.GOOS == "windows" {
		srcAbs = strings.ToLower(srcAbs)
		dstAbs = strings.ToLower(dstAbs)
	}

	// Check if dst is the same as src
	if srcAbs == dstAbs {
		return true
	}

	// Check if dst is a subdirectory of src
	// Use filepath.Rel to check the relationship
	rel, err := filepath.Rel(srcAbs, dstAbs)
	if err != nil {
		return false
	}

	// If rel is "." or doesn't start with "..", then dst is inside src
	return rel == "." || !strings.HasPrefix(rel, "..")
}
