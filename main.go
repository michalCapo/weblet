package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// version is set at build time using ldflags
var version = "dev"

type Weblet struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	PID  int    `json:"pid,omitempty"`
}

type WebletManager struct {
	weblets map[string]*Weblet
	dataDir string
}

func NewWebletManager() (*WebletManager, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	dataDir := filepath.Join(homeDir, ".weblet")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	wm := &WebletManager{
		weblets: make(map[string]*Weblet),
		dataDir: dataDir,
	}

	if err := wm.loadWeblets(); err != nil {
		return nil, fmt.Errorf("failed to load weblets: %w", err)
	}

	return wm, nil
}

func (wm *WebletManager) loadWeblets() error {
	dataFile := filepath.Join(wm.dataDir, "weblets.json")
	data, err := os.ReadFile(dataFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // File doesn't exist yet, that's okay
		}
		return err
	}

	var weblets []Weblet
	if err := json.Unmarshal(data, &weblets); err != nil {
		return err
	}

	for _, w := range weblets {
		wm.weblets[w.Name] = &w
	}

	return nil
}

func (wm *WebletManager) saveWeblets() error {
	dataFile := filepath.Join(wm.dataDir, "weblets.json")
	var weblets []Weblet
	for _, w := range wm.weblets {
		weblets = append(weblets, *w)
	}

	data, err := json.MarshalIndent(weblets, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(dataFile, data, 0644)
}

func (wm *WebletManager) List() {
	if len(wm.weblets) == 0 {
		fmt.Println("No weblets available.")
		return
	}

	fmt.Println("Available weblets:")
	for name, weblet := range wm.weblets {
		status := "stopped"
		if weblet.PID > 0 {
			// Check if process is still running
			if wm.isProcessRunning(weblet.PID) {
				status = "running"
			} else {
				// Clean up stale PID
				weblet.PID = 0
			}
		}
		fmt.Printf("  %s: %s (%s)\n", name, weblet.URL, status)
	}
}

func (wm *WebletManager) findBrowser() (string, error) {
	// Try browsers in order of preference
	browsers := []string{
		"google-chrome",
		"chromium",
		"chromium-browser",
	}

	for _, browser := range browsers {
		if _, err := exec.LookPath(browser); err == nil {
			return browser, nil
		}
	}

	return "", fmt.Errorf("no supported browser found (tried: google-chrome, chromium, chromium-browser)")
}

func (wm *WebletManager) Run(name string) error {
	weblet, exists := wm.weblets[name]
	if !exists {
		return fmt.Errorf("weblet '%s' not found", name)
	}

	// Check if already running
	if weblet.PID > 0 && wm.isProcessRunning(weblet.PID) {
		// Focus on the existing window
		return wm.focusWindow(weblet.PID)
	}

	// Find available browser
	browser, err := wm.findBrowser()
	if err != nil {
		return err
	}

	// Start new instance
	cmd := exec.Command(browser, "--app="+weblet.URL)
	cmd.Start()

	weblet.PID = cmd.Process.Pid
	wm.saveWeblets()

	fmt.Printf("Started weblet '%s' with PID %d using %s\n", name, weblet.PID, browser)
	return nil
}

func (wm *WebletManager) Add(name, url string) error {
	if _, exists := wm.weblets[name]; exists {
		return fmt.Errorf("weblet '%s' already exists", name)
	}

	wm.weblets[name] = &Weblet{
		Name: name,
		URL:  url,
	}

	if err := wm.saveWeblets(); err != nil {
		return err
	}

	// Create desktop file for GNOME
	if err := wm.createDesktopFile(name, url); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to create desktop file: %v\n", err)
	}

	return nil
}

func (wm *WebletManager) Remove(name string) error {
	weblet, exists := wm.weblets[name]
	if !exists {
		return fmt.Errorf("weblet '%s' not found", name)
	}

	// Stop if running
	if weblet.PID > 0 && wm.isProcessRunning(weblet.PID) {
		wm.stopProcess(weblet.PID)
	}

	delete(wm.weblets, name)

	if err := wm.saveWeblets(); err != nil {
		return err
	}

	// Remove desktop file for GNOME
	if err := wm.removeDesktopFile(name); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to remove desktop file: %v\n", err)
	}

	return nil
}

func (wm *WebletManager) isProcessRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	err = process.Signal(syscall.Signal(0))
	return err == nil
}

func (wm *WebletManager) focusWindow(pid int) error {
	fmt.Printf("Focusing existing window for PID %d...\n", pid)

	// Try to find the window ID by PID
	windowID, err := wm.findWindowByPID(pid)
	if err != nil {
		return fmt.Errorf("failed to find window for PID %d: %w", pid, err)
	}

	// Try multiple methods to focus the window
	methods := []struct {
		name string
		cmd  *exec.Cmd
	}{
		{
			name: "wmctrl -i -a",
			cmd:  exec.Command("wmctrl", "-i", "-a", windowID),
		},
		{
			name: "xdotool windowactivate",
			cmd:  exec.Command("xdotool", "windowactivate", windowID),
		},
	}

	var lastErr error
	for _, method := range methods {
		if err := method.cmd.Run(); err == nil {
			fmt.Printf("Successfully focused window using %s\n", method.name)
			return nil
		} else {
			lastErr = err
		}
	}

	return fmt.Errorf("failed to focus window: %w", lastErr)
}

func (wm *WebletManager) findWindowByPID(pid int) (string, error) {
	// Try wmctrl first
	cmd := exec.Command("wmctrl", "-lp")
	output, err := cmd.Output()
	if err == nil {
		// Parse wmctrl output: WindowID Desktop PID Machine WindowTitle
		lines := string(output)
		for _, line := range splitLines(lines) {
			var windowID string
			var desktop int
			var windowPID int
			_, err := fmt.Sscanf(line, "%s %d %d", &windowID, &desktop, &windowPID)
			if err == nil && windowPID == pid {
				return windowID, nil
			}
		}
	}

	// Fallback to xdotool
	cmd = exec.Command("xdotool", "search", "--pid", fmt.Sprintf("%d", pid))
	output, err = cmd.Output()
	if err == nil {
		lines := splitLines(string(output))
		if len(lines) > 0 && lines[0] != "" {
			// Return the first window ID found
			return lines[0], nil
		}
	}

	// Last resort: try xprop with all windows
	cmd = exec.Command("bash", "-c", fmt.Sprintf("xdotool search --all --name '' | while read wid; do xprop -id $wid _NET_WM_PID | grep -q '%d$' && echo $wid && break; done", pid))
	output, err = cmd.Output()
	if err == nil {
		windowID := string(output)
		if windowID != "" {
			return windowID, nil
		}
	}

	return "", fmt.Errorf("no window found for PID %d", pid)
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if line != "" {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	if start < len(s) {
		line := s[start:]
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func (wm *WebletManager) stopProcess(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}

func (wm *WebletManager) getDesktopFilePath(name string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	desktopDir := filepath.Join(homeDir, ".local", "share", "applications")
	if err := os.MkdirAll(desktopDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create applications directory: %w", err)
	}

	return filepath.Join(desktopDir, fmt.Sprintf("weblet-%s.desktop", name)), nil
}

func (wm *WebletManager) downloadFavicon(webletURL string) (string, error) {
	parsedURL, err := url.Parse(webletURL)
	if err != nil {
		return "", err
	}

	iconDir := filepath.Join(wm.dataDir, "icons")
	if err := os.MkdirAll(iconDir, 0755); err != nil {
		return "", err
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	// First, try to parse HTML to find icon links
	iconURLs := wm.findIconsFromHTML(webletURL, client)

	// Add common favicon locations as fallback
	baseURL := fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Host)
	iconURLs = append(iconURLs,
		baseURL+"/apple-touch-icon.png",
		baseURL+"/apple-touch-icon-precomposed.png",
		baseURL+"/favicon-32x32.png",
		baseURL+"/favicon-16x16.png",
		baseURL+"/favicon-96x96.png",
		baseURL+"/favicon-128x128.png",
		baseURL+"/favicon.png",
		baseURL+"/icon.png",
		baseURL+"/favicon.ico",
	)

	// Try each icon URL, prioritizing PNG files
	for _, iconURL := range iconURLs {
		// Skip non-PNG files unless it's the last resort
		if !strings.HasSuffix(strings.ToLower(iconURL), ".png") &&
			!strings.HasSuffix(strings.ToLower(iconURL), ".ico") {
			continue
		}

		iconPath, err := wm.downloadIconFile(iconURL, parsedURL.Host, client, iconDir)
		if err == nil && iconPath != "" {
			// Prefer PNG over ICO
			if strings.HasSuffix(strings.ToLower(iconPath), ".png") {
				return iconPath, nil
			}
			// Store ICO as fallback
			if strings.HasSuffix(strings.ToLower(iconPath), ".ico") {
				// Try to find a PNG still, but keep this as backup
				for _, pngURL := range iconURLs {
					if strings.HasSuffix(strings.ToLower(pngURL), ".png") {
						pngPath, pngErr := wm.downloadIconFile(pngURL, parsedURL.Host, client, iconDir)
						if pngErr == nil && pngPath != "" {
							return pngPath, nil
						}
					}
				}
				// No PNG found, use ICO
				return iconPath, nil
			}
		}
	}

	return "", fmt.Errorf("failed to download any icon")
}

func (wm *WebletManager) findIconsFromHTML(webletURL string, client *http.Client) []string {
	var iconURLs []string

	resp, err := client.Get(webletURL)
	if err != nil {
		return iconURLs
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return iconURLs
	}

	// Read HTML body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return iconURLs
	}

	html := string(body)

	// Parse base URL for relative paths
	parsedURL, _ := url.Parse(webletURL)
	baseURL := fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Host)

	// Find all icon-related link tags
	patterns := []string{
		`<link[^>]*rel=["'](?:apple-touch-icon|icon|shortcut icon)["'][^>]*href=["']([^"']+)["'][^>]*>`,
		`<link[^>]*href=["']([^"']+)["'][^>]*rel=["'](?:apple-touch-icon|icon|shortcut icon)["'][^>]*>`,
		`<meta[^>]*property=["']og:image["'][^>]*content=["']([^"']+)["'][^>]*>`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindAllStringSubmatch(html, -1)
		for _, match := range matches {
			if len(match) > 1 {
				iconURL := match[1]
				// Convert relative URLs to absolute
				if strings.HasPrefix(iconURL, "//") {
					iconURL = parsedURL.Scheme + ":" + iconURL
				} else if strings.HasPrefix(iconURL, "/") {
					iconURL = baseURL + iconURL
				} else if !strings.HasPrefix(iconURL, "http") {
					iconURL = baseURL + "/" + iconURL
				}
				iconURLs = append(iconURLs, iconURL)
			}
		}
	}

	return iconURLs
}

func (wm *WebletManager) downloadIconFile(iconURL, host string, client *http.Client, iconDir string) (string, error) {
	resp, err := client.Get(iconURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to fetch: status %d", resp.StatusCode)
	}

	// Determine file extension from URL or content type
	ext := ".ico"
	if strings.Contains(strings.ToLower(iconURL), ".png") {
		ext = ".png"
	} else if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "png") {
		ext = ".png"
	}

	iconPath := filepath.Join(iconDir, host+ext)
	out, err := os.Create(iconPath)
	if err != nil {
		return "", err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		os.Remove(iconPath)
		return "", err
	}

	return iconPath, nil
}

func (wm *WebletManager) createDesktopFile(name, webletURL string) error {
	desktopFilePath, err := wm.getDesktopFilePath(name)
	if err != nil {
		return err
	}

	// Get the path to the weblet executable
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Try to download favicon
	iconPath, err := wm.downloadFavicon(webletURL)
	if err != nil {
		// Use a default icon if favicon download fails
		iconPath = "web-browser"
	}

	// Create desktop file content
	desktopContent := fmt.Sprintf(`[Desktop Entry]
Version=1.0
Type=Application
Name=%s
Comment=Weblet for %s
Exec=%s %s
Icon=%s
Terminal=false
Categories=Network;WebBrowser;
StartupNotify=true
StartupWMClass=%s
`,
		name,
		webletURL,
		execPath,
		name,
		iconPath,
		name,
	)

	// Write the desktop file
	if err := os.WriteFile(desktopFilePath, []byte(desktopContent), 0644); err != nil {
		return fmt.Errorf("failed to write desktop file: %w", err)
	}

	// Make the desktop file executable
	if err := os.Chmod(desktopFilePath, 0755); err != nil {
		return fmt.Errorf("failed to make desktop file executable: %w", err)
	}

	fmt.Printf("Created desktop file: %s\n", desktopFilePath)

	// Update desktop database to make GNOME pick up the new application
	exec.Command("update-desktop-database", filepath.Dir(desktopFilePath)).Run()

	return nil
}

func (wm *WebletManager) removeDesktopFile(name string) error {
	desktopFilePath, err := wm.getDesktopFilePath(name)
	if err != nil {
		return err
	}

	// Remove the desktop file if it exists
	if _, err := os.Stat(desktopFilePath); err == nil {
		if err := os.Remove(desktopFilePath); err != nil {
			return fmt.Errorf("failed to remove desktop file: %w", err)
		}
		fmt.Printf("Removed desktop file: %s\n", desktopFilePath)

		// Update desktop database
		exec.Command("update-desktop-database", filepath.Dir(desktopFilePath)).Run()
	}

	return nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage:")
		fmt.Println("  weblet version")
		fmt.Println("  weblet list")
		fmt.Println("  weblet <name>")
		fmt.Println("  weblet add <name> <url>")
		fmt.Println("  weblet remove <name>")
		os.Exit(1)
	}

	wm, err := NewWebletManager()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "version":
		fmt.Printf("weblet version %s\n", version)
		return

	case "list":
		wm.List()

	case "add":
		if len(os.Args) != 4 {
			fmt.Println("Usage: weblet add <name> <url>")
			os.Exit(1)
		}
		name := os.Args[2]
		url := os.Args[3]
		if err := wm.Add(name, url); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Added weblet '%s' with URL '%s'\n", name, url)

	case "remove":
		if len(os.Args) != 3 {
			fmt.Println("Usage: weblet remove <name>")
			os.Exit(1)
		}
		name := os.Args[2]
		if err := wm.Remove(name); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Removed weblet '%s'\n", name)

	default:
		// Run weblet with given name
		name := command
		if err := wm.Run(name); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
}
