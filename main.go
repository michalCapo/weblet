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
	Name      string `json:"name"`
	URL       string `json:"url"`
	PID       int    `json:"pid,omitempty"`
	UseChrome bool   `json:"use_chrome,omitempty"` // Use Chrome for WebRTC-heavy apps
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
		weblet := w // Create a copy to avoid pointer to loop variable
		wm.weblets[w.Name] = &weblet
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
		mode := ""
		if !weblet.UseChrome {
			mode = " [native]"
		}
		fmt.Printf("  %s: %s%s\n", name, weblet.URL, mode)
	}
}

func (wm *WebletManager) Setup() error {
	fmt.Println("=== Weblet Setup ===")
	fmt.Println()

	// Check for window management tools (needed for focusing existing windows)
	fmt.Println("Checking window management tools:")
	wmctrlInstalled := wm.checkTool("wmctrl")
	xdotoolInstalled := wm.checkTool("xdotool")

	if !wmctrlInstalled && !xdotoolInstalled {
		fmt.Println("\n⚠️  Warning: Neither wmctrl nor xdotool found!")
		fmt.Println("   Window focusing feature will not work.")
		fmt.Println("   Install at least one with:")
		fmt.Println("   - sudo apt install wmctrl")
		fmt.Println("   - sudo apt install xdotool")
		fmt.Println()
	} else if !wmctrlInstalled {
		fmt.Println("\n⚠️  Warning: wmctrl not found (xdotool is available)")
		fmt.Println("   Consider installing wmctrl for better compatibility:")
		fmt.Println("   - sudo apt install wmctrl")
		fmt.Println()
	} else if !xdotoolInstalled {
		fmt.Println("\n⚠️  Warning: xdotool not found (wmctrl is available)")
		fmt.Println("   Consider installing xdotool as a fallback option:")
		fmt.Println("   - sudo apt install xdotool")
		fmt.Println()
	} else {
		fmt.Println("\n✓ All window management tools are installed!")
		fmt.Println()
	}

	fmt.Println("✓ Weblet uses native webview for displaying web applications.")
	fmt.Println("  No browser configuration needed.")

	return nil
}

func (wm *WebletManager) checkTool(tool string) bool {
	path, err := exec.LookPath(tool)
	if err != nil {
		fmt.Printf("  ✗ %s: not found\n", tool)
		return false
	}
	fmt.Printf("  ✓ %s: %s\n", tool, path)
	return true
}

func (wm *WebletManager) Run(name string) error {
	weblet, exists := wm.weblets[name]
	if !exists {
		return fmt.Errorf("weblet '%s' not found", name)
	}

	// If weblet uses Chrome, run with Chrome instead of native webview
	if weblet.UseChrome {
		return wm.runWithChrome(weblet)
	}

	// Check if we're already running as a background process
	isBackground := os.Getenv("WEBLET_BACKGROUND") == "1"

	// Check if webview window with this name already exists
	if wm.isWebletWindowOpen(name) {
		// Try to focus the existing window by title
		if isBackground {
			// Background process: just exit silently, window already exists
			return nil
		}
		return wm.focusWindowByTitle(name)
	}

	// Lock file to prevent race conditions
	lockDir := filepath.Join(wm.dataDir, "locks")
	os.MkdirAll(lockDir, 0755)
	lockFile := filepath.Join(lockDir, name+".lock")

	if isBackground {
		// We're the background process - remove lock when done
		defer os.Remove(lockFile)

		// Double-check window doesn't exist (another process might have created it)
		if wm.isWebletWindowOpen(name) {
			return nil
		}

		// Run the webview
		runWebview(weblet.URL, name)
		return nil
	}

	// Parent process: try to acquire lock atomically before spawning
	lock, err := os.OpenFile(lockFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		// Lock exists - another instance is starting, wait for window and focus
		fmt.Printf("Weblet '%s' is starting, waiting for window...\n", name)
		for i := 0; i < 20; i++ {
			time.Sleep(200 * time.Millisecond)
			if wm.isWebletWindowOpen(name) {
				return wm.focusWindowByTitle(name)
			}
		}
		// Timeout - check if lock is stale (older than 10 seconds)
		if info, err := os.Stat(lockFile); err == nil {
			if time.Since(info.ModTime()) > 10*time.Second {
				os.Remove(lockFile) // Stale lock, remove it
				return wm.Run(name) // Retry
			}
		}
		return fmt.Errorf("timeout waiting for weblet '%s' to start", name)
	}
	lock.Close()

	// Fork to background: spawn ourselves with the same arguments
	executable, err := os.Executable()
	if err != nil {
		os.Remove(lockFile)
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	cmd := exec.Command(executable, name)
	cmd.Env = append(os.Environ(), "WEBLET_BACKGROUND=1")

	// Redirect output to /dev/null but keep display access
	devNull, err := os.OpenFile("/dev/null", os.O_WRONLY, 0)
	if err == nil {
		cmd.Stdout = devNull
		cmd.Stderr = devNull
		defer devNull.Close()
	}
	cmd.Stdin = nil

	// Start new process group but don't create new session (keep display)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	if err := cmd.Start(); err != nil {
		os.Remove(lockFile)
		return fmt.Errorf("failed to start background process: %w", err)
	}

	pid := cmd.Process.Pid

	// Detach from the child process so it continues after we exit
	cmd.Process.Release()

	fmt.Printf("Started weblet '%s' in background (PID %d)\n", name, pid)
	return nil
}

// runWithChrome runs the weblet using Chrome/Chromium in app mode
// This is needed for WebRTC-heavy apps like Discord that need full audio device support
func (wm *WebletManager) runWithChrome(weblet *Weblet) error {
	// Check if Chrome window already exists
	if wm.isWebletWindowOpen(weblet.Name) {
		return wm.focusWindowByTitle(weblet.Name)
	}

	// Find Chrome or Chromium
	browsers := []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"}
	var browser string
	for _, b := range browsers {
		if _, err := exec.LookPath(b); err == nil {
			browser = b
			break
		}
	}
	if browser == "" {
		return fmt.Errorf("Chrome or Chromium not found. Install with: sudo apt install google-chrome-stable")
	}

	// Create Chrome user data directory for this weblet
	userDataDir := filepath.Join(wm.dataDir, "chrome-data", weblet.Name)
	os.MkdirAll(userDataDir, 0755)

	// Start Chrome in app mode
	cmd := exec.Command(browser,
		"--app="+weblet.URL,
		"--user-data-dir="+userDataDir,
		"--class=weblet-"+weblet.Name,
	)

	// Redirect output to null
	devNull, _ := os.OpenFile("/dev/null", os.O_WRONLY, 0)
	if devNull != nil {
		cmd.Stdout = devNull
		cmd.Stderr = devNull
		defer devNull.Close()
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start Chrome: %w", err)
	}

	cmd.Process.Release()
	fmt.Printf("Started weblet '%s' with Chrome (WebRTC mode)\n", weblet.Name)
	return nil
}

// SetChromeMode enables or disables Chrome mode for a weblet
func (wm *WebletManager) SetChromeMode(name string, useChrome bool) error {
	weblet, exists := wm.weblets[name]
	if !exists {
		return fmt.Errorf("weblet '%s' not found", name)
	}

	weblet.UseChrome = useChrome
	if err := wm.saveWeblets(); err != nil {
		return err
	}

	if useChrome {
		fmt.Printf("Weblet '%s' will now use Chrome (default, full audio support)\n", name)
	} else {
		fmt.Printf("Weblet '%s' will now use native webview (lighter, no WebRTC audio)\n", name)
	}
	return nil
}

func (wm *WebletManager) Add(name, url string) error {
	if _, exists := wm.weblets[name]; exists {
		return fmt.Errorf("weblet '%s' already exists", name)
	}

	wm.weblets[name] = &Weblet{
		Name:      name,
		URL:       url,
		UseChrome: true, // Chrome is default for full WebRTC/audio support
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

func (wm *WebletManager) isWebletWindowOpen(name string) bool {
	// Check by WM_CLASS first (most reliable - works for both native webview and Chrome)
	// wmctrl -lx output format: WindowID Desktop WM_CLASS Machine WindowTitle...
	cmd := exec.Command("wmctrl", "-lx")
	output, err := cmd.Output()
	if err == nil {
		lines := splitLines(string(output))
		targetClass := strings.ToLower("weblet-" + name)

		for _, line := range lines {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				// WM_CLASS is in format "instance.class" (e.g., "weblet-discord.weblet-discord")
				wmClass := strings.ToLower(parts[2])
				if wmClass == targetClass || strings.HasPrefix(wmClass, targetClass+".") ||
					strings.HasSuffix(wmClass, "."+targetClass) || strings.Contains(wmClass, targetClass) {
					return true
				}
			}
		}
	}

	// Fallback: check by window title
	cmd = exec.Command("wmctrl", "-l")
	output, err = cmd.Output()
	if err != nil {
		return false
	}

	lines := splitLines(string(output))
	nameLower := strings.ToLower(name)

	for _, line := range lines {
		// wmctrl output format: WindowID Desktop Machine WindowTitle...
		parts := strings.Fields(line)
		if len(parts) >= 4 {
			windowTitle := strings.Join(parts[3:], " ")
			windowTitleLower := strings.ToLower(windowTitle)

			// Check if window title matches the weblet name
			if windowTitleLower == nameLower || strings.HasPrefix(windowTitleLower, nameLower+" ") {
				return true
			}
		}
	}

	return false
}

func (wm *WebletManager) focusWindowByTitle(title string) error {
	fmt.Printf("Focusing existing window: %s\n", title)

	// Try to find window by WM_CLASS first (most reliable)
	// wmctrl -lx output format: WindowID Desktop WM_CLASS Machine WindowTitle...
	cmd := exec.Command("wmctrl", "-lx")
	output, err := cmd.Output()
	if err == nil {
		lines := splitLines(string(output))
		targetClass := strings.ToLower("weblet-" + title)

		for _, line := range lines {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				wmClass := strings.ToLower(parts[2])
				if wmClass == targetClass || strings.HasPrefix(wmClass, targetClass+".") ||
					strings.HasSuffix(wmClass, "."+targetClass) || strings.Contains(wmClass, targetClass) {
					windowID := parts[0]
					return wm.focusWindowByID(windowID)
				}
			}
		}
	}

	// Fallback: search by window title
	cmd = exec.Command("wmctrl", "-l")
	output, err = cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to list windows: %w", err)
	}

	lines := splitLines(string(output))
	titleLower := strings.ToLower(title)

	for _, line := range lines {
		// wmctrl output format: WindowID Desktop Machine WindowTitle...
		parts := strings.Fields(line)
		if len(parts) >= 4 {
			windowTitle := strings.Join(parts[3:], " ")
			windowTitleLower := strings.ToLower(windowTitle)

			// Check if window title matches
			if windowTitleLower == titleLower || strings.HasPrefix(windowTitleLower, titleLower+" ") {
				windowID := parts[0]
				return wm.focusWindowByID(windowID)
			}
		}
	}

	return fmt.Errorf("no window found with title: %s", title)
}

func (wm *WebletManager) focusWindowByID(windowID string) error {
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

	// Check if weblet is in PATH, if so use just "weblet" for better portability
	// But only if the PATH version is the same as our current executable
	if pathWeblet, err := exec.LookPath("weblet"); err == nil {
		// Check if the PATH version is the same as our current executable
		if pathWeblet == execPath {
			execPath = "weblet"
		}
		// Otherwise, use the absolute path to ensure we use our version
	}

	// Try to download favicon
	iconPath, err := wm.downloadFavicon(webletURL)
	if err != nil {
		// Use a default icon if favicon download fails
		iconPath = "web-browser"
	}

	// Create desktop file content
	// StartupWMClass must match what we set in view.go (weblet-<name>)
	wmClass := fmt.Sprintf("weblet-%s", name)
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
		wmClass,
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
		fmt.Println("  weblet setup")
		fmt.Println("  weblet list")
		fmt.Println("  weblet <name>           - Run existing weblet")
		fmt.Println("  weblet <name> <url>     - Add and run weblet")
		fmt.Println("  weblet add <name> <url> - Add weblet without running")
		fmt.Println("  weblet remove <name>    - Remove weblet")
		fmt.Println("  weblet native <name>    - Toggle native mode (lighter, no WebRTC)")
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

	case "setup":
		if err := wm.Setup(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

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

	case "native":
		if len(os.Args) != 3 {
			fmt.Println("Usage: weblet native <name>")
			fmt.Println("Toggles native webview mode (lighter weight, but no WebRTC audio)")
			os.Exit(1)
		}
		name := os.Args[2]
		weblet, exists := wm.weblets[name]
		if !exists {
			fmt.Fprintf(os.Stderr, "Error: weblet '%s' not found\n", name)
			os.Exit(1)
		}
		// Toggle native mode (inverse of Chrome mode)
		if err := wm.SetChromeMode(name, !weblet.UseChrome); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	default:
		// Handle: weblet <name> or weblet <name> <url>
		name := command
		var url string

		// Check if URL is provided (add and run immediately)
		if len(os.Args) == 3 {
			url = os.Args[2]

			// Check if weblet already exists
			if existingWeblet, exists := wm.weblets[name]; exists {
				if existingWeblet.URL == url {
					// Same URL - just run it (idempotent behavior)
					fmt.Printf("Weblet '%s' already exists with this URL\n", name)
				} else {
					// Different URL - update it
					existingWeblet.URL = url
					if err := wm.saveWeblets(); err != nil {
						fmt.Fprintf(os.Stderr, "Error saving weblets: %v\n", err)
						os.Exit(1)
					}
					fmt.Printf("Updated weblet '%s' with new URL '%s'\n", name, url)
				}
			} else {
				// Weblet doesn't exist - add it
				if err := wm.Add(name, url); err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					os.Exit(1)
				}
				fmt.Printf("Added weblet '%s' with URL '%s'\n", name, url)
			}
		} else if len(os.Args) > 3 {
			fmt.Println("Usage:")
			fmt.Println("  weblet <name>           - Run existing weblet")
			fmt.Println("  weblet <name> <url>     - Add and run weblet")
			os.Exit(1)
		}

		// Run the weblet
		if err := wm.Run(name); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
}
