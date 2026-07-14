package filepreview

import (
	"log/slog"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// osClipboard implements clipboard.OSClipboard using platform-specific
// OS clipboard tools (osascript on macOS, xclip/wl-clipboard on Linux).
type osClipboard struct{}

// NewOSClipboard creates a new OS clipboard helper.
// On unsupported platforms (e.g. Windows) or when no clipboard tool is
// found on Linux, all operations silently return empty/nil.
func NewOSClipboard() *osClipboard {
	return &osClipboard{}
}

// WriteFileURIs writes file:// URIs for the given paths to the OS clipboard.
// On macOS it uses osascript to set NSPasteboard content.
// On Linux it probes $PATH for xclip, then wl-copy.
// Silently skips when no clipboard tool is available.
func (c *osClipboard) WriteFileURIs(paths []string) error {
	if len(paths) == 0 {
		return nil
	}

	switch runtime.GOOS {
	case "darwin":
		return c.writeMacOS(paths)
	case "linux":
		return c.writeLinux(paths)
	default:
		slog.Debug("OS clipboard not supported on this platform",
			"GOOS", runtime.GOOS)
		return nil
	}
}

// ReadFileURIs reads file:// URIs from the OS clipboard and returns
// the parsed absolute file paths. On macOS it uses osascript to read
// NSPasteboard. On Linux it probes $PATH for xclip, then wl-paste.
// Returns empty slice when no clipboard tool is available or no file URIs found.
func (c *osClipboard) ReadFileURIs() ([]string, error) {
	switch runtime.GOOS {
	case "darwin":
		return c.readMacOS()
	case "linux":
		return c.readLinux()
	default:
		slog.Debug("OS clipboard not supported on this platform",
			"GOOS", runtime.GOOS)
		return nil, nil
	}
}

// ---------- macOS (osascript) ----------

func (c *osClipboard) writeMacOS(paths []string) error {
	// Build AppleScript that sets NSPasteboard with file:// URLs + plain text paths
	var fileURLs, plainPaths []string
	for _, p := range paths {
		absPath, err := filepath.Abs(p)
		if err != nil {
			absPath = p
		}
		fileURLs = append(fileURLs, "file://"+absPath)
		plainPaths = append(plainPaths, absPath)
	}

	// Use set the clipboard to a list of file URLs via AppleScript
	script := buildMacOSWriteScript(fileURLs, plainPaths)
	cmd := exec.Command("osascript", "-e", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error("Failed to write to macOS clipboard via osascript",
			"error", err, "output", string(output))
		return nil // Graceful degradation — don't fail the operation
	}
	return nil
}

func buildMacOSWriteScript(fileURLs, plainPaths []string) string {
	var sb strings.Builder
	sb.WriteString("use framework \"AppKit\"\n")
	sb.WriteString("use scripting additions\n")
	sb.WriteString("\n")

	sb.WriteString("set pb to current application's NSPasteboard's generalPasteboard()\n")
	sb.WriteString("pb's clearContents()\n")
	sb.WriteString("\n")

	// Write file URLs via NSURL objects (adds public.file-url UTI)
	sb.WriteString("set urlList to current application's NSMutableArray's alloc()'s init()\n")
	for _, p := range plainPaths {
		escaped := strings.ReplaceAll(p, "\"", "\\\"")
		sb.WriteString("(urlList's addObject:(current application's NSURL's fileURLWithPath:\"")
		sb.WriteString(escaped)
		sb.WriteString("\"))\n")
	}
	sb.WriteString("pb's writeObjects:urlList\n")
	sb.WriteString("\n")

	// Also write plain text paths (comma+newline separated) for broader compatibility
	sb.WriteString("set textItems to \"")
	for i, p := range plainPaths {
		if i > 0 {
			sb.WriteString("\\n")
		}
		sb.WriteString(strings.ReplaceAll(p, "\"", "\\\""))
	}
	sb.WriteString("\"\n")
	sb.WriteString("pb's setString:textItems forType:(current application's NSPasteboardTypeString)\n")

	return sb.String()
}

func (c *osClipboard) readMacOS() ([]string, error) {
	// Use a Swift helper to read file URLs from NSPasteboard.
	// AppleScript's AppKit bridge doesn't support NSPasteboardItem's
	// stringForType:, so we use swift directly.
	return c.readMacOSSwift()
}

func (c *osClipboard) readMacOSSwift() ([]string, error) {
	script := `import AppKit
let pb = NSPasteboard.general
var paths: [String] = []
if let items = pb.pasteboardItems {
	for item in items {
		if let urlStr = item.string(forType: .fileURL),
		   let url = URL(string: urlStr) {
			paths.append(url.path)
		}
	}
}
if paths.isEmpty,
   let plist = pb.propertyList(forType: NSPasteboard.PasteboardType("NSFilenamesPboardType")) as? [String] {
	paths = plist
}
print(paths.joined(separator: "\n"))
`
	cmd := exec.Command("swift", "-")
	cmd.Stdin = strings.NewReader(script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.Debug("Failed to read macOS clipboard via swift",
			"error", err, "output", string(output))
		return nil, nil
	}
	result := strings.TrimSpace(string(output))
	if result == "" {
		return nil, nil
	}
	// Output is raw paths (resolved from file URLs or NSFilenamesPboardType)
	return parseLinesAsPaths(result)
}

func (c *osClipboard) readMacOSTextFallback() ([]string, error) {
	script := `set theText to (the clipboard as text)`
	cmd := exec.Command("osascript", "-e", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.Debug("Failed to read macOS clipboard as text", "error", err)
		return nil, nil
	}

	text := strings.TrimSpace(string(output))
	if text == "" {
		return nil, nil
	}

	// Treat newline-separated text as file paths
	lines := strings.Split(text, "\n")
	paths := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			paths = append(paths, line)
		}
	}
	return paths, nil
}

// ---------- Linux (xclip / wl-clipboard) ----------

func (c *osClipboard) writeLinux(paths []string) error {
	var sb strings.Builder
	for i, p := range paths {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("file://")
		absPath, err := filepath.Abs(p)
		if err != nil {
			sb.WriteString(p)
		} else {
			sb.WriteString(absPath)
		}
	}
	data := sb.String()

	if hasTool("xclip") {
		cmd := exec.Command("xclip", "-selection", "clipboard", "-t", "text/uri-list")
		cmd.Stdin = strings.NewReader(data)
		if err := cmd.Run(); err != nil {
			slog.Debug("xclip write failed, trying wl-copy", "error", err)
		} else {
			return nil
		}
	}

	if hasTool("wl-copy") {
		cmd := exec.Command("wl-copy", "-t", "text/uri-list")
		cmd.Stdin = strings.NewReader(data)
		if err := cmd.Run(); err != nil {
			slog.Debug("wl-copy write failed", "error", err)
			return nil
		}
		return nil
	}

	slog.Debug("No clipboard tool found on Linux (probed xclip, wl-copy)")
	return nil
}

func (c *osClipboard) readLinux() ([]string, error) {
	if hasTool("xclip") {
		cmd := exec.Command("xclip", "-selection", "clipboard", "-o", "-t", "text/uri-list")
		output, err := cmd.CombinedOutput()
		if err == nil {
			text := strings.TrimSpace(string(output))
			if text != "" {
				return parseFileURLs(text)
			}
		}
		slog.Debug("xclip read failed or empty, trying fallback", "error", err)
		// Fallback: read as plain text
		cmd = exec.Command("xclip", "-selection", "clipboard", "-o")
		output, err = cmd.CombinedOutput()
		if err == nil {
			text := strings.TrimSpace(string(output))
			if text != "" {
				return parseLinesAsPaths(text)
			}
		}
	}

	if hasTool("wl-paste") {
		cmd := exec.Command("wl-paste", "-t", "text/uri-list")
		output, err := cmd.CombinedOutput()
		if err == nil {
			text := strings.TrimSpace(string(output))
			if text != "" {
				return parseFileURLs(text)
			}
		}
		// Fallback: read as plain text
		cmd = exec.Command("wl-paste")
		output, err = cmd.CombinedOutput()
		if err == nil {
			text := strings.TrimSpace(string(output))
			if text != "" {
				return parseLinesAsPaths(text)
			}
		}
	}

	slog.Debug("No clipboard tool found on Linux (probed xclip, wl-paste)")
	return nil, nil
}

// ---------- Helpers ----------

func hasTool(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// parseFileURLs parses file:// URI formatted text, returning absolute paths.
// Supports both single and multi-line URI lists.
func parseFileURLs(text string) ([]string, error) {
	lines := strings.Split(text, "\n")
	paths := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Strip file:// prefix
		path := strings.TrimPrefix(line, "file://")
		// Strip trailing whitespace or carriage returns
		path = strings.TrimSpace(path)
		if path != "" {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

// parseLinesAsPaths treats each line as a raw file path.
func parseLinesAsPaths(text string) ([]string, error) {
	lines := strings.Split(text, "\n")
	paths := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			paths = append(paths, line)
		}
	}
	return paths, nil
}
