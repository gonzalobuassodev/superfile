package backend

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Download copies a file or directory from a remote filesystem to a local
// destination path. It uses the sourceFS to read and the local FS for writing.
func Download(sourceFS FileSystem, remotePath, localDest string) error {
	info, err := sourceFS.Stat(remotePath)
	if err != nil {
		return fmt.Errorf("failed to stat remote path %s: %w", remotePath, err)
	}

	if info.IsDir() {
		return downloadDir(sourceFS, remotePath, localDest, info)
	}
	return downloadFile(sourceFS, remotePath, localDest, info)
}

func downloadFile(sourceFS FileSystem, remotePath, localDest string, info os.FileInfo) error {
	remoteFile, err := sourceFS.Open(remotePath)
	if err != nil {
		return fmt.Errorf("failed to open remote file %s: %w", remotePath, err)
	}
	defer remoteFile.Close()

	localFile, err := os.OpenFile(localDest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return fmt.Errorf("failed to create local file %s: %w", localDest, err)
	}
	defer localFile.Close()

	if _, err := io.Copy(localFile, remoteFile); err != nil {
		return fmt.Errorf("failed to copy remote file %s to %s: %w", remotePath, localDest, err)
	}
	return nil
}

func downloadDir(sourceFS FileSystem, remotePath, localDest string, info os.FileInfo) error {
	if err := os.MkdirAll(localDest, info.Mode()); err != nil {
		return fmt.Errorf("failed to create local directory %s: %w", localDest, err)
	}

	entries, err := sourceFS.ReadDir(remotePath)
	if err != nil {
		return fmt.Errorf("failed to read remote directory %s: %w", remotePath, err)
	}

	for _, entry := range entries {
		srcPath := sourceFS.Join(remotePath, entry.Name())
		dstPath := filepath.Join(localDest, entry.Name())

		if entry.IsDir() {
			entryInfo, err := sourceFS.Stat(srcPath)
			if err != nil {
				return fmt.Errorf("failed to stat remote path %s: %w", srcPath, err)
			}
			if err := downloadDir(sourceFS, srcPath, dstPath, entryInfo); err != nil {
				return err
			}
		} else {
			if err := downloadFile(sourceFS, srcPath, dstPath, entry); err != nil {
				return err
			}
		}
	}
	return nil
}

// Upload copies a file or directory from local filesystem to a remote
// destination path. It uses the local FS for reading and the targetFS for writing.
func Upload(targetFS FileSystem, localPath, remoteDest string) error {
	info, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("failed to stat local path %s: %w", localPath, err)
	}

	if info.IsDir() {
		return uploadDir(targetFS, localPath, remoteDest, info)
	}
	return uploadFile(targetFS, localPath, remoteDest, info)
}

func uploadFile(targetFS FileSystem, localPath, remoteDest string, info os.FileInfo) error {
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

	if _, err := io.Copy(remoteFile, localFile); err != nil {
		return fmt.Errorf("failed to upload %s to %s: %w", localPath, remoteDest, err)
	}
	return nil
}

func uploadDir(targetFS FileSystem, localPath, remoteDest string, info os.FileInfo) error {
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
			if err := uploadDir(targetFS, srcPath, dstPath, entryInfo); err != nil {
				return err
			}
		} else {
			if err := uploadFile(targetFS, srcPath, dstPath, entryInfo); err != nil {
				return err
			}
		}
	}
	return nil
}

// RemoteCopy copies files or directories from one remote filesystem to another.
// When sourceFS == targetFS, it uses Rename for cut operations within the same FS.
func RemoteCopy(sourceFS, targetFS FileSystem, srcPath, dstPath string) error {
	info, err := sourceFS.Stat(srcPath)
	if err != nil {
		return fmt.Errorf("failed to stat source path %s: %w", srcPath, err)
	}

	if info.IsDir() {
		return remoteCopyDir(sourceFS, targetFS, srcPath, dstPath, info)
	}
	return remoteCopyFile(sourceFS, targetFS, srcPath, dstPath, info)
}

func remoteCopyFile(sourceFS, targetFS FileSystem, srcPath, dstPath string, info os.FileInfo) error {
	srcFile, err := sourceFS.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open remote source file %s: %w", srcPath, err)
	}
	defer srcFile.Close()

	dstFile, err := targetFS.Create(dstPath)
	if err != nil {
		return fmt.Errorf("failed to create remote destination file %s: %w", dstPath, err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy remote file %s to %s: %w", srcPath, dstPath, err)
	}
	return nil
}

func remoteCopyDir(sourceFS, targetFS FileSystem, srcPath, dstPath string, info os.FileInfo) error {
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
			if err := remoteCopyDir(sourceFS, targetFS, srcEntry, dstEntry, entryInfo); err != nil {
				return err
			}
		} else {
			if err := remoteCopyFile(sourceFS, targetFS, srcEntry, dstEntry, entry); err != nil {
				return err
			}
		}
	}
	return nil
}
