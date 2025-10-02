package main

import (
	"bufio"
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

type BrowserConfig struct {
	Browser string `json:"browser"`
}

type WebletManager struct {
	weblets       map[string]*Weblet
	dataDir       string
	browserConfig *BrowserConfig
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

	if err := wm.loadBrowserConfig(); err != nil {
		return nil, fmt.Errorf("failed to load browser config: %w", err)
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

func (wm *WebletManager) loadBrowserConfig() error {
	configFile := filepath.Join(wm.dataDir, "weblet.json")
	data, err := os.ReadFile(configFile)
	if err != nil {
		if os.IsNotExist(err) {
			// No config file exists yet, that's okay
			wm.browserConfig = &BrowserConfig{}
			return nil
		}
		return err
	}

	wm.browserConfig = &BrowserConfig{}
	if err := json.Unmarshal(data, wm.browserConfig); err != nil {
		return err
	}

	return nil
}

func (wm *WebletManager) saveBrowserConfig() error {
	configFile := filepath.Join(wm.dataDir, "weblet.json")
	data, err := json.MarshalIndent(wm.browserConfig, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configFile, data, 0644)
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

func (wm *WebletManager) detectAvailableBrowsers() []string {
	var available []string
	browsers := []string{
		"google-chrome",
		"chromium",
		"chromium-browser",
	}

	for _, browser := range browsers {
		if _, err := exec.LookPath(browser); err == nil {
			available = append(available, browser)
		}
	}

	return available
}

func (wm *WebletManager) findBrowser() (string, error) {
	// If browser is already configured, use it
	if wm.browserConfig != nil && wm.browserConfig.Browser != "" {
		if _, err := exec.LookPath(wm.browserConfig.Browser); err == nil {
			return wm.browserConfig.Browser, nil
		}
		// Configured browser not found, fall through to detection
	}

	// Detect available browsers
	available := wm.detectAvailableBrowsers()
	if len(available) == 0 {
		return "", fmt.Errorf("no supported browser found (tried: google-chrome, chromium, chromium-browser)")
	}

	// If only one browser found, use it
	if len(available) == 1 {
		wm.browserConfig.Browser = available[0]
		wm.saveBrowserConfig()
		return available[0], nil
	}

	// Multiple browsers found, need user selection
	return "", fmt.Errorf("multiple browsers found, please run 'weblet setup' to choose")
}

func (wm *WebletManager) Setup() error {
	available := wm.detectAvailableBrowsers()
	if len(available) == 0 {
		return fmt.Errorf("no supported browser found (tried: google-chrome, chromium, chromium-browser)")
	}

	if len(available) == 1 {
		wm.browserConfig.Browser = available[0]
		if err := wm.saveBrowserConfig(); err != nil {
			return fmt.Errorf("failed to save browser config: %w", err)
		}
		fmt.Printf("Automatically selected browser: %s\n", available[0])
		return nil
	}

	// Multiple browsers found, ask user to choose
	fmt.Println("Multiple browsers found. Please choose one:")
	for i, browser := range available {
		fmt.Printf("  %d. %s\n", i+1, browser)
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("Enter your choice (1-", len(available), "): ")
		input, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read input: %w", err)
		}

		input = strings.TrimSpace(input)
		choice := 0
		if _, err := fmt.Sscanf(input, "%d", &choice); err != nil {
			fmt.Println("Invalid input. Please enter a number.")
			continue
		}

		if choice < 1 || choice > len(available) {
			fmt.Printf("Invalid choice. Please enter a number between 1 and %d.\n", len(available))
			continue
		}

		selectedBrowser := available[choice-1]
		wm.browserConfig.Browser = selectedBrowser
		if err := wm.saveBrowserConfig(); err != nil {
			return fmt.Errorf("failed to save browser config: %w", err)
		}

		fmt.Printf("Browser configured: %s\n", selectedBrowser)
		return nil
	}
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

	// Check if Chrome app window exists for this URL (more reliable than PID tracking)
	if wm.isChromeAppWindowOpen(weblet.URL) {
		// Try to find and focus the window by URL
		return wm.focusWindowByURL(weblet.URL)
	}

	// Find available browser
	browser, err := wm.findBrowser()
	if err != nil {
		return err
	}

	// Start new instance
	cmd := exec.Command(browser, "--app="+weblet.URL)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start browser: %w", err)
	}

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

func (wm *WebletManager) isChromeAppWindowOpen(url string) bool {
	// Check if there's a Chrome app window open for this URL
	cmd := exec.Command("wmctrl", "-l")
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	lines := splitLines(string(output))
	domain := extractDomain(url)

	// Create a more flexible matching pattern
	urlPatterns := []string{
		url,
		domain,
		strings.ReplaceAll(domain, ".", " "),   // Replace dots with spaces
		strings.ReplaceAll(domain, "www.", ""), // Remove www prefix
	}

	// Add common site name mappings
	if strings.Contains(domain, "youtube.com") {
		urlPatterns = append(urlPatterns, "YouTube", "youtube")
	}
	if strings.Contains(domain, "gmail.com") {
		urlPatterns = append(urlPatterns, "Gmail", "gmail")
	}
	if strings.Contains(domain, "github.com") {
		urlPatterns = append(urlPatterns, "GitHub", "github")
	}
	if strings.Contains(domain, "teams.microsoft.com") {
		urlPatterns = append(urlPatterns, "Teams", "teams", "Microsoft Teams")
	}

	for _, line := range lines {
		// wmctrl output format: WindowID Desktop PID Machine WindowTitle
		parts := strings.Fields(line)
		if len(parts) >= 4 {
			windowTitle := strings.Join(parts[4:], " ")
			windowTitleLower := strings.ToLower(windowTitle)

			// Check if window title contains any of our patterns
			for _, pattern := range urlPatterns {
				if strings.Contains(windowTitleLower, strings.ToLower(pattern)) {
					return true
				}
			}
		}
	}

	return false
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

func (wm *WebletManager) focusWindowByURL(url string) error {
	fmt.Printf("Focusing existing window for URL %s...\n", url)

	// Try to find the window ID by URL using wmctrl
	cmd := exec.Command("wmctrl", "-l")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to list windows: %w", err)
	}

	lines := splitLines(string(output))
	domain := extractDomain(url)

	// Create a more flexible matching pattern (same as isChromeAppWindowOpen)
	urlPatterns := []string{
		url,
		domain,
		strings.ReplaceAll(domain, ".", " "),   // Replace dots with spaces
		strings.ReplaceAll(domain, "www.", ""), // Remove www prefix
	}

	// Add common site name mappings
	if strings.Contains(domain, "youtube.com") {
		urlPatterns = append(urlPatterns, "YouTube", "youtube")
	}
	if strings.Contains(domain, "gmail.com") {
		urlPatterns = append(urlPatterns, "Gmail", "gmail")
	}
	if strings.Contains(domain, "github.com") {
		urlPatterns = append(urlPatterns, "GitHub", "github")
	}
	if strings.Contains(domain, "teams.microsoft.com") {
		urlPatterns = append(urlPatterns, "Teams", "teams", "Microsoft Teams")
	}

	for _, line := range lines {
		// wmctrl output format: WindowID Desktop PID Machine WindowTitle
		parts := strings.Fields(line)
		if len(parts) >= 4 {
			windowTitle := strings.Join(parts[4:], " ")
			windowTitleLower := strings.ToLower(windowTitle)

			// Check if window title contains any of our patterns
			for _, pattern := range urlPatterns {
				if strings.Contains(windowTitleLower, strings.ToLower(pattern)) {
					windowID := parts[0]
					return wm.focusWindowByID(windowID)
				}
			}
		}
	}

	return fmt.Errorf("no window found for URL %s", url)
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

func extractDomain(url string) string {
	// Simple domain extraction - could be improved with proper URL parsing
	if strings.HasPrefix(url, "https://") {
		url = url[8:]
	} else if strings.HasPrefix(url, "http://") {
		url = url[7:]
	}

	if idx := strings.Index(url, "/"); idx != -1 {
		url = url[:idx]
	}

	return url
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
		fmt.Println("  weblet setup")
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

	default:
		// Run weblet with given name
		name := command

		// Check if browser setup is needed (first run or no browser configured)
		if wm.browserConfig == nil || wm.browserConfig.Browser == "" {
			available := wm.detectAvailableBrowsers()
			if len(available) == 0 {
				fmt.Fprintf(os.Stderr, "Error: no supported browser found (tried: google-chrome, chromium, chromium-browser)\n")
				fmt.Fprintf(os.Stderr, "Please install Google Chrome or Chromium and run 'weblet setup'\n")
				os.Exit(1)
			}

			if len(available) > 1 {
				fmt.Println("Multiple browsers found. Please run 'weblet setup' to choose your preferred browser.")
				os.Exit(1)
			}

			// Auto-configure single browser
			wm.browserConfig.Browser = available[0]
			if err := wm.saveBrowserConfig(); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to save browser config: %v\n", err)
			}
			fmt.Printf("Automatically configured browser: %s\n", available[0])
		}

		if err := wm.Run(name); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
}
