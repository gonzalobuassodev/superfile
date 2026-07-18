package preview

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/yorukot/superfile/src/internal/common"
	"github.com/yorukot/superfile/src/internal/ui/rendering"
)

// archiveEntry represents a single entry inside an archive
type archiveEntry struct {
	name    string
	size    int64
	isDir   bool
	mode    os.FileMode
}

// isArchiveFile checks if a file path has an archive extension
func isArchiveFile(path string) bool {
	lower := strings.ToLower(path)
	ext := filepath.Ext(lower)

	if common.ArchiveExtensions[ext] {
		return true
	}

	for _, ce := range common.CompoundArchiveExtensions {
		if strings.HasSuffix(lower, ce) {
			return true
		}
	}

	return false
}

// archiveExt returns the canonical archive format key for a path.
// For compound extensions like .tar.gz it returns the full suffix.
func archiveExt(path string) string {
	lower := strings.ToLower(path)

	// Check compound extensions first (longer match)
	for _, ce := range common.CompoundArchiveExtensions {
		if strings.HasSuffix(lower, ce) {
			return ce
		}
	}

	return filepath.Ext(lower)
}

// renderArchivePreview lists the contents of an archive file in the preview panel.
// It supports zip, tar, tar.gz/tgz, tar.bz2, jar, war, cbz natively (Go stdlib),
// and rar/7z via external tools (lsar, unrar, 7z).
func renderArchivePreview(r *rendering.Renderer, itemPath string, previewHeight int) string {
	ext := archiveExt(itemPath)

	slog.Debug("Rendering archive preview", "path", itemPath, "ext", ext)

	var entries []archiveEntry

	switch {
	case ext == ".zip", ext == ".jar", ext == ".war", ext == ".cbz":
		var err error
		entries, err = readZipArchive(itemPath)
		if err != nil {
			slog.Error("Error reading zip archive", "error", err)
			r.AddLines(common.FilePreviewArchiveReadErrorText)
			return r.Render()
		}
	case ext == ".tar", ext == ".tgz", ext == ".tar.gz", ext == ".tar.bz2", ext == ".tar.xz", ext == ".tar.zst":
		var err error
		entries, err = readTarArchive(itemPath, ext)
		if err != nil {
			slog.Error("Error reading tar archive", "error", err)
			r.AddLines(common.FilePreviewArchiveReadErrorText)
			return r.Render()
		}
	case ext == ".rar", ext == ".cbr", ext == ".7z":
		var err error
		entries, err = readArchiveViaExternal(itemPath)
		if err != nil {
			slog.Error("Error reading archive via external tool", "error", err)
			r.AddLines(common.FilePreviewArchiveReadErrorText)
			return r.Render()
		}
	default:
		r.AddLines(common.FilePreviewArchiveReadErrorText)
		return r.Render()
	}

	if len(entries) == 0 {
		r.AddLines(common.FilePreviewEmptyText)
		return r.Render()
	}

	// Sort: directories first, then alphabetically
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].isDir && !entries[j].isDir {
			return true
		}
		if !entries[i].isDir && entries[j].isDir {
			return false
		}
		return entries[i].name < entries[j].name
	})

	// Render entries up to previewHeight
	for i := 0; i < previewHeight && i < len(entries); i++ {
		entry := entries[i]
		style := common.GetElementIcon(entry.name, entry.isDir, false, common.Config.Nerdfont)
		icon := style.Icon + " "
		iconColor := style.Color
		if !common.Config.Nerdfont {
			icon = ""
			iconColor = common.Theme.FilePanelFG
		}

		line := lipgloss.NewStyle().Foreground(lipgloss.Color(iconColor)).
			Background(common.FilePanelBGColor).Render(icon)

		if entry.isDir {
			line += common.FilePanelStyle.Render(entry.name + "/")
		} else {
			line += common.FilePanelStyle.Render(fmt.Sprintf("%-*s", previewHeight*2, entry.name))
		}

		if !entry.isDir && entry.size > 0 {
			line += common.FilePanelStyle.Render("  " + formatSize(entry.size))
		}

		r.AddLines(line)
	}

	return r.Render()
}

// readZipArchive reads the contents of a zip file
func readZipArchive(path string) ([]archiveEntry, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("opening zip: %w", err)
	}
	defer reader.Close()

	entries := make([]archiveEntry, 0, len(reader.File))
	for _, f := range reader.File {
		name := filepath.Clean(f.Name)
		if name == "." {
			continue
		}
		entries = append(entries, archiveEntry{
			name:  name,
			size:  int64(f.UncompressedSize64),
			isDir: f.FileInfo().IsDir(),
			mode:  f.Mode(),
		})
	}
	return entries, nil
}

// readTarArchive reads the contents of a tar file, optionally decompressing
// gzip, bzip2, xz, or zstd based on the extension.
func readTarArchive(path string, ext string) ([]archiveEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening tar: %w", err)
	}
	defer f.Close()

	var reader io.Reader = f

	switch ext {
	case ".tgz", ".tar.gz":
		gr, err := gzip.NewReader(f)
		if err != nil {
			return nil, fmt.Errorf("decompressing gzip: %w", err)
		}
		defer gr.Close()
		reader = gr
	case ".tar.bz2":
		reader = bzip2.NewReader(f)
	case ".tar.xz":
		// xz requires external tool or CGO; try xz command
		return readTarViaExternalDecompressor(path, "xz", "-dc")
	case ".tar.zst":
		// zstd similarly requires external tool
		return readTarViaExternalDecompressor(path, "zstd", "-dc")
	}

	return readTarEntries(reader)
}

// readTarViaExternalDecompressor decompresses through an external command
// and reads the tar stream from its output.
func readTarViaExternalDecompressor(path string, cmdName string, args ...string) ([]archiveEntry, error) {
	cmd := exec.Command(cmdName, append(args, path)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("creating pipe for %s: %w", cmdName, err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %s: %w", cmdName, err)
	}
	defer cmd.Wait() //nolint:errcheck

	return readTarEntries(stdout)
}

// readTarEntries reads tar entries from a reader
func readTarEntries(reader io.Reader) ([]archiveEntry, error) {
	tr := tar.NewReader(reader)
	var entries []archiveEntry

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading tar entry: %w", err)
		}

		name := filepath.Clean(header.Name)
		if name == "." {
			continue
		}

		entries = append(entries, archiveEntry{
			name:  name,
			size:  header.Size,
			isDir: header.Typeflag == tar.TypeDir,
			mode:  os.FileMode(header.Mode),
		})
	}

	return entries, nil
}

// readArchiveViaExternal tries external tools to list archive contents
// for formats without native Go support (rar, 7z).
func readArchiveViaExternal(path string) ([]archiveEntry, error) {
	// Try lsar first (part of The Unarchiver, common on macOS)
	if entries, err := readViaLsar(path); err == nil {
		return entries, nil
	}

	// Try unrar for rar files
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".rar" || ext == ".cbr" {
		if entries, err := readViaUnrar(path); err == nil {
			return entries, nil
		}
	}

	// Try 7z for 7z/rar
	if entries, err := readVia7z(path); err == nil {
		return entries, nil
	}

	return nil, fmt.Errorf("no external tool available to list archive: %s", path)
}

// readViaLsar uses the `lsar` command to list archive contents
func readViaLsar(path string) ([]archiveEntry, error) {
	cmd := exec.Command("lsar", "-j", path)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("lsar failed: %w", err)
	}

	// lsar -j outputs JSON; we parse it simply by looking for filename fields
	// Skip JSON parsing complexity — parse lines that look like path entries
	lines := strings.Split(string(output), "\n")
	var entries []archiveEntry

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "{") || strings.HasPrefix(line, "}") ||
			strings.HasPrefix(line, "[") || strings.HasPrefix(line, "]") ||
			strings.Contains(line, "\"pathname\"") || strings.Contains(line, "\"container\"") {
			continue
		}
		// Simple heuristic: lines with quoted strings that look like paths
		if strings.HasPrefix(line, "\"") && strings.HasSuffix(line, "\",") {
			name := strings.TrimSuffix(strings.TrimPrefix(line, "\""), "\",")
			name = filepath.Clean(name)
			if name == "." {
				continue
			}
			entries = append(entries, archiveEntry{
				name:  name,
				isDir: strings.HasSuffix(name, "/"),
			})
		}
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("lsar returned no parseable entries")
	}

	return entries, nil
}

// readViaUnrar uses the `unrar` command to list rar contents
func readViaUnrar(path string) ([]archiveEntry, error) {
	cmd := exec.Command("unrar", "lt", path)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("unrar failed: %w", err)
	}

	// unrar lt outputs lines like:
	//   filename.ext           size  date
	var entries []archiveEntry
	lines := strings.Split(string(output), "\n")
	inFileList := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "-----------") {
			inFileList = !inFileList
			continue
		}
		if !inFileList || line == "" {
			continue
		}

		// Skip attribute/summary lines
		if strings.HasPrefix(line, "Attributes") || strings.HasPrefix(line, "---") {
			continue
		}

		// Try to parse: name might have size trailing
		// unrar lt format varies; just take what looks like a path
		name := strings.TrimSpace(line)
		if name == "" || strings.Contains(name, "Pathname") || strings.Contains(name, "Total") {
			continue
		}

		entries = append(entries, archiveEntry{
			name: name,
		})
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("unrar returned no entries")
	}

	return entries, nil
}

// readVia7z uses the `7z` command to list archive contents
func readVia7z(path string) ([]archiveEntry, error) {
	cmd := exec.Command("7z", "l", "-ba", path)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("7z failed: %w", err)
	}

	// 7z l -ba outputs lines like:
	//   2024-01-01 12:00:00 ....A 12345 100 file.txt
	//   date       time   attr  size  comp  name
	var entries []archiveEntry
	lines := strings.Split(string(output), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// 7z format: date time attr size compressed name
		// We just take whatever is after the 5th field
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}

		// Check attr for 'D' = directory
		attr := fields[2]
		isDir := strings.Contains(attr, "D")

		// Name is everything after the 5th field
		name := strings.Join(fields[5:], " ")
		name = filepath.Clean(name)
		if name == "." {
			continue
		}

		entries = append(entries, archiveEntry{
			name:  name,
			isDir: isDir,
		})
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("7z returned no entries")
	}

	return entries, nil
}

// formatSize formats a byte size into a human-readable string
func formatSize(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(bytes)/(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(bytes)/(1<<10))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
