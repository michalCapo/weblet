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

	"github.com/michalCapo/weblet/view"
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
		view.RunWebview(weblet.URL, name)
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
	// Create Chrome user data directory for this weblet
	userDataDir := filepath.Join(wm.dataDir, "chrome-data", weblet.Name)
	os.MkdirAll(userDataDir, 0755)

	// Most reliable check: look for Chrome process with this weblet's user-data-dir
	// This works on both X11 and Wayland
	if wm.isChromeProcessRunning(userDataDir) {
		fmt.Printf("Weblet '%s' is already running, focusing window...\n", weblet.Name)
		// Try to focus the window using available methods
		if err := wm.focusChromeWindowAnyMethod(weblet.Name, weblet.URL); err != nil {
			// If focusing fails (e.g., on Wayland without proper tools), inform user
			fmt.Printf("Note: Could not focus window automatically (%v). Please switch to it manually.\n", err)
		}
		return nil
	}

	// Fallback: Check if Chrome window exists by WM_CLASS or window title (X11 only)
	if wm.isWebletWindowOpen(weblet.Name) {
		return wm.focusWindowByTitle(weblet.Name)
	}

	// Additional check: look for Chrome windows with the weblet's URL in the title
	// Chrome app windows typically show the page title
	if wm.isChromeWebletWindowOpen(weblet.Name, weblet.URL) {
		return wm.focusChromeWindow(weblet.Name, weblet.URL)
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

	// Start Chrome in app mode
	// Force X11 mode via XWayland so wmctrl can focus the window on Wayland
	cmd := exec.Command(browser,
		"--app="+weblet.URL,
		"--user-data-dir="+userDataDir,
		"--class=weblet-"+weblet.Name,
		"--ozone-platform=x11",
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

// Refresh re-downloads the icon and updates the desktop file for a weblet
func (wm *WebletManager) Refresh(name string) error {
	weblet, exists := wm.weblets[name]
	if !exists {
		return fmt.Errorf("weblet '%s' not found", name)
	}

	// Remove old icon files for this weblet
	iconDir := filepath.Join(wm.dataDir, "icons")
	extensions := []string{".png", ".ico", ".svg", ".jpg"}
	for _, ext := range extensions {
		iconPath := filepath.Join(iconDir, name+ext)
		os.Remove(iconPath) // Ignore errors, file might not exist
	}

	// Re-create the desktop file (which will re-download the icon)
	if err := wm.createDesktopFile(name, weblet.URL); err != nil {
		return fmt.Errorf("failed to refresh weblet: %w", err)
	}

	fmt.Printf("Refreshed weblet '%s'\n", name)
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

// isChromeWebletWindowOpen checks if a Chrome app window for this weblet is open
// Chrome app mode windows may not use the WM_CLASS we set, so we also check by window title
func (wm *WebletManager) isChromeWebletWindowOpen(name, webletURL string) bool {
	cmd := exec.Command("wmctrl", "-l")
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	lines := splitLines(string(output))
	nameLower := strings.ToLower(name)

	// Known mappings of weblet names to possible window titles
	// e.g., "discord" weblet might have a window titled "Discord"
	possibleTitles := []string{nameLower}

	// Extract domain from URL for additional matching
	if parsed, err := url.Parse(webletURL); err == nil {
		host := strings.TrimPrefix(parsed.Host, "www.")
		// For app.discord.com -> "discord"
		parts := strings.Split(host, ".")
		if len(parts) >= 2 {
			possibleTitles = append(possibleTitles, strings.ToLower(parts[len(parts)-2]))
		}
	}

	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) >= 4 {
			windowTitle := strings.Join(parts[3:], " ")
			windowTitleLower := strings.ToLower(windowTitle)

			for _, title := range possibleTitles {
				// Check various patterns Chrome might use
				if strings.Contains(windowTitleLower, title) {
					return true
				}
			}
		}
	}

	return false
}

// focusChromeWindow finds and focuses a Chrome app window for the weblet
func (wm *WebletManager) focusChromeWindow(name, webletURL string) error {
	fmt.Printf("Focusing existing Chrome window: %s\n", name)

	cmd := exec.Command("wmctrl", "-l")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to list windows: %w", err)
	}

	lines := splitLines(string(output))
	nameLower := strings.ToLower(name)

	// Known mappings of weblet names to possible window titles
	possibleTitles := []string{nameLower}

	// Extract domain from URL for additional matching
	if parsed, err := url.Parse(webletURL); err == nil {
		host := strings.TrimPrefix(parsed.Host, "www.")
		parts := strings.Split(host, ".")
		if len(parts) >= 2 {
			possibleTitles = append(possibleTitles, strings.ToLower(parts[len(parts)-2]))
		}
	}

	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) >= 4 {
			windowTitle := strings.Join(parts[3:], " ")
			windowTitleLower := strings.ToLower(windowTitle)

			for _, title := range possibleTitles {
				if strings.Contains(windowTitleLower, title) {
					windowID := parts[0]
					return wm.focusWindowByID(windowID)
				}
			}
		}
	}

	return fmt.Errorf("no Chrome window found for: %s", name)
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

// isChromeProcessRunning checks if a Chrome process is running with the given user-data-dir
// This works on both X11 and Wayland by checking /proc
func (wm *WebletManager) isChromeProcessRunning(userDataDir string) bool {
	// Read all process directories in /proc
	procDir, err := os.Open("/proc")
	if err != nil {
		return false
	}
	defer procDir.Close()

	entries, err := procDir.Readdirnames(-1)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		// Check if entry is a PID (all digits)
		isPid := true
		for _, c := range entry {
			if c < '0' || c > '9' {
				isPid = false
				break
			}
		}
		if !isPid {
			continue
		}

		// Read the cmdline for this process
		cmdlinePath := filepath.Join("/proc", entry, "cmdline")
		cmdline, err := os.ReadFile(cmdlinePath)
		if err != nil {
			continue
		}

		// cmdline is null-separated, check if it contains our user-data-dir
		cmdlineStr := string(cmdline)
		if strings.Contains(cmdlineStr, userDataDir) {
			// Also verify it's a Chrome/Chromium process
			if strings.Contains(cmdlineStr, "chrome") || strings.Contains(cmdlineStr, "chromium") {
				return true
			}
		}
	}

	return false
}

// focusChromeWindowAnyMethod tries multiple methods to focus a Chrome weblet window
// This handles both X11 and Wayland environments
func (wm *WebletManager) focusChromeWindowAnyMethod(name, webletURL string) error {
	// First try the standard wmctrl/xdotool methods (works on X11)
	if err := wm.focusChromeWindow(name, webletURL); err == nil {
		return nil
	}

	// Try using gdbus to activate the window via GNOME Shell (works on Wayland with GNOME)
	// Find windows matching our criteria
	nameLower := strings.ToLower(name)
	possibleTitles := []string{nameLower}

	// Extract domain from URL for additional matching
	if parsed, err := url.Parse(webletURL); err == nil {
		host := strings.TrimPrefix(parsed.Host, "www.")
		parts := strings.Split(host, ".")
		if len(parts) >= 2 {
			possibleTitles = append(possibleTitles, strings.ToLower(parts[len(parts)-2]))
		}
	}

	// Try using gdbus to call GNOME Shell's window activation
	// This uses the org.gnome.Shell.Extensions.Windows interface if available
	gdbusCmd := exec.Command("gdbus", "call", "--session",
		"--dest", "org.gnome.Shell",
		"--object-path", "/org/gnome/Shell",
		"--method", "org.gnome.Shell.Eval",
		fmt.Sprintf(`
			const start = Date.now();
			const targets = %q.split(',');
			let found = false;
			global.get_window_actors().forEach(actor => {
				const win = actor.get_meta_window();
				const title = (win.get_title() || '').toLowerCase();
				for (const target of targets) {
					if (title.includes(target.trim())) {
						win.activate(start);
						found = true;
						return;
					}
				}
			});
			found;
		`, strings.Join(possibleTitles, ",")))

	if output, err := gdbusCmd.Output(); err == nil {
		// gdbus returns something like "(true, 'true')" or "(true, 'false')"
		// The first bool is success of eval, the second (in quotes) is our result
		outputStr := string(output)
		if strings.Contains(outputStr, "'true'") {
			fmt.Printf("Successfully focused window using GNOME Shell\n")
			return nil
		}
	}

	return fmt.Errorf("could not focus window using any available method")
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

func (wm *WebletManager) downloadFavicon(webletURL, webletName string) (string, error) {
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
		baseURL+"/favicon-192x192.png",
		baseURL+"/favicon-256x256.png",
		baseURL+"/favicon-32x32.png",
		baseURL+"/favicon-16x16.png",
		baseURL+"/favicon-96x96.png",
		baseURL+"/favicon-128x128.png",
		baseURL+"/favicon.png",
		baseURL+"/icon.png",
		baseURL+"/favicon.ico",
	)

	// Add icon services as reliable fallbacks (provide proper app icons)
	domain := parsedURL.Host
	// Strip www. prefix for cleaner domain matching
	cleanDomain := strings.TrimPrefix(domain, "www.")

	iconURLs = append(iconURLs,
		// icon.horse - provides high quality favicons
		fmt.Sprintf("https://icon.horse/icon/%s", cleanDomain),
		// Google's favicon service
		fmt.Sprintf("https://www.google.com/s2/favicons?domain=%s&sz=128", cleanDomain),
		fmt.Sprintf("https://www.google.com/s2/favicons?domain=%s&sz=64", cleanDomain),
		// DuckDuckGo's icon service
		fmt.Sprintf("https://icons.duckduckgo.com/ip3/%s.ico", cleanDomain),
	)

	var icoFallback string

	// Try each icon URL, prioritizing PNG files
	for _, iconURL := range iconURLs {
		iconPath, err := wm.downloadIconFile(iconURL, webletName, client, iconDir)
		if err == nil && iconPath != "" {
			// Prefer PNG over ICO
			if strings.HasSuffix(strings.ToLower(iconPath), ".png") {
				return iconPath, nil
			}
			// Store ICO as fallback
			if strings.HasSuffix(strings.ToLower(iconPath), ".ico") && icoFallback == "" {
				icoFallback = iconPath
			}
		}
	}

	// Use ICO fallback if we have one
	if icoFallback != "" {
		return icoFallback, nil
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

	// Find all icon-related link tags (prioritize larger icons)
	// Note: We do NOT include og:image as those are social media preview images, not app icons
	patterns := []string{
		// Web app manifest first (contains high-res icons designed for apps)
		`<link[^>]*rel=["']manifest["'][^>]*href=["']([^"']+)["'][^>]*>`,
		`<link[^>]*href=["']([^"']+)["'][^>]*rel=["']manifest["'][^>]*>`,
		// Apple touch icons (usually 180x180 or larger, designed for app icons)
		`<link[^>]*rel=["']apple-touch-icon(?:-precomposed)?["'][^>]*href=["']([^"']+)["'][^>]*>`,
		`<link[^>]*href=["']([^"']+)["'][^>]*rel=["']apple-touch-icon(?:-precomposed)?["'][^>]*>`,
		// Standard icons with sizes attribute (prefer larger)
		`<link[^>]*rel=["']icon["'][^>]*sizes=["'](?:192x192|256x256|512x512|384x384|128x128|96x96)["'][^>]*href=["']([^"']+)["'][^>]*>`,
		`<link[^>]*href=["']([^"']+)["'][^>]*rel=["']icon["'][^>]*sizes=["'](?:192x192|256x256|512x512|384x384|128x128|96x96)["'][^>]*>`,
		// Standard icons (any size)
		`<link[^>]*rel=["'](?:icon|shortcut icon)["'][^>]*href=["']([^"']+)["'][^>]*>`,
		`<link[^>]*href=["']([^"']+)["'][^>]*rel=["'](?:icon|shortcut icon)["'][^>]*>`,
	}

	var manifestURL string
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindAllStringSubmatch(html, -1)
		for _, match := range matches {
			if len(match) > 1 {
				foundURL := match[1]
				// Convert relative URLs to absolute
				if strings.HasPrefix(foundURL, "//") {
					foundURL = parsedURL.Scheme + ":" + foundURL
				} else if strings.HasPrefix(foundURL, "/") {
					foundURL = baseURL + foundURL
				} else if !strings.HasPrefix(foundURL, "http") {
					foundURL = baseURL + "/" + foundURL
				}

				// Check if this is a manifest file
				if strings.Contains(pattern, "manifest") {
					if manifestURL == "" {
						manifestURL = foundURL
					}
				} else {
					iconURLs = append(iconURLs, foundURL)
				}
			}
		}
	}

	// Parse manifest file for high-res icons
	if manifestURL != "" {
		manifestIcons := wm.findIconsFromManifest(manifestURL, client)
		// Prepend manifest icons (they're usually higher quality)
		iconURLs = append(manifestIcons, iconURLs...)
	}

	return iconURLs
}

// findIconsFromManifest parses a web app manifest and extracts icon URLs
func (wm *WebletManager) findIconsFromManifest(manifestURL string, client *http.Client) []string {
	var iconURLs []string

	resp, err := client.Get(manifestURL)
	if err != nil {
		return iconURLs
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return iconURLs
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return iconURLs
	}

	// Parse manifest JSON
	var manifest struct {
		Icons []struct {
			Src   string `json:"src"`
			Sizes string `json:"sizes"`
			Type  string `json:"type"`
		} `json:"icons"`
	}

	if err := json.Unmarshal(body, &manifest); err != nil {
		return iconURLs
	}

	// Parse base URL for relative paths
	parsedURL, _ := url.Parse(manifestURL)
	baseURL := fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Host)

	// Sort icons by size (prefer larger), and prefer PNG
	type iconInfo struct {
		url  string
		size int
	}
	var icons []iconInfo

	for _, icon := range manifest.Icons {
		iconURL := icon.Src
		// Convert relative URLs to absolute
		if strings.HasPrefix(iconURL, "//") {
			iconURL = parsedURL.Scheme + ":" + iconURL
		} else if strings.HasPrefix(iconURL, "/") {
			iconURL = baseURL + iconURL
		} else if !strings.HasPrefix(iconURL, "http") {
			// Handle relative path from manifest location
			manifestDir := filepath.Dir(parsedURL.Path)
			iconURL = baseURL + filepath.Join(manifestDir, iconURL)
		}

		// Parse size (e.g., "192x192" -> 192)
		size := 0
		if icon.Sizes != "" {
			parts := strings.Split(icon.Sizes, "x")
			if len(parts) > 0 {
				fmt.Sscanf(parts[0], "%d", &size)
			}
		}

		icons = append(icons, iconInfo{url: iconURL, size: size})
	}

	// Sort by size descending (larger first)
	for i := 0; i < len(icons)-1; i++ {
		for j := i + 1; j < len(icons); j++ {
			if icons[j].size > icons[i].size {
				icons[i], icons[j] = icons[j], icons[i]
			}
		}
	}

	for _, icon := range icons {
		iconURLs = append(iconURLs, icon.url)
	}

	return iconURLs
}

func (wm *WebletManager) downloadIconFile(iconURL, webletName string, client *http.Client, iconDir string) (string, error) {
	resp, err := client.Get(iconURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to fetch: status %d", resp.StatusCode)
	}

	// Read the response body
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Validate minimum size (icons should be at least a few bytes)
	if len(data) < 100 {
		return "", fmt.Errorf("icon too small: %d bytes", len(data))
	}

	// Determine file extension from content type or URL
	ext := ".ico"
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "png") || strings.Contains(strings.ToLower(iconURL), ".png") {
		ext = ".png"
	} else if strings.Contains(contentType, "svg") {
		ext = ".svg"
	} else if strings.Contains(contentType, "jpeg") || strings.Contains(contentType, "jpg") {
		ext = ".jpg"
	}

	// For PNG images, validate dimensions to ensure it's a proper icon (roughly square)
	// This helps avoid grabbing social media preview images which are rectangular
	if ext == ".png" {
		if !wm.isValidIconDimensions(data) {
			return "", fmt.Errorf("image is not a valid icon (not square)")
		}
	}

	// Use weblet name for the icon file (ensures unique icon per weblet)
	iconPath := filepath.Join(iconDir, webletName+ext)
	out, err := os.Create(iconPath)
	if err != nil {
		return "", err
	}
	defer out.Close()

	_, err = out.Write(data)
	if err != nil {
		os.Remove(iconPath)
		return "", err
	}

	return iconPath, nil
}

// isValidIconDimensions checks if PNG data represents a roughly square icon
// Returns true for square or near-square images (aspect ratio between 0.8 and 1.25)
func (wm *WebletManager) isValidIconDimensions(data []byte) bool {
	// PNG header: 8 bytes signature, then IHDR chunk
	// IHDR chunk: 4 bytes length, 4 bytes type ("IHDR"), 4 bytes width, 4 bytes height
	if len(data) < 24 {
		return false
	}

	// Check PNG signature
	pngSig := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	for i := 0; i < 8; i++ {
		if data[i] != pngSig[i] {
			return true // Not a PNG, skip dimension check
		}
	}

	// Check for IHDR chunk type at offset 12-15
	if data[12] != 'I' || data[13] != 'H' || data[14] != 'D' || data[15] != 'R' {
		return true // Invalid PNG structure, skip check
	}

	// Read width (big-endian) at offset 16-19
	width := uint32(data[16])<<24 | uint32(data[17])<<16 | uint32(data[18])<<8 | uint32(data[19])
	// Read height (big-endian) at offset 20-23
	height := uint32(data[20])<<24 | uint32(data[21])<<16 | uint32(data[22])<<8 | uint32(data[23])

	if width == 0 || height == 0 {
		return false
	}

	// Calculate aspect ratio
	var ratio float64
	if width > height {
		ratio = float64(width) / float64(height)
	} else {
		ratio = float64(height) / float64(width)
	}

	// Accept roughly square icons (aspect ratio up to 1.25)
	// This allows for some padding but rejects 1200x630 social images (ratio ~1.9)
	return ratio <= 1.25
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
	iconPath, err := wm.downloadFavicon(webletURL, name)
	if err != nil {
		fmt.Printf("Warning: Could not download icon: %v\n", err)
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
		fmt.Println("  weblet refresh <name>   - Refresh icon and desktop file")
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

	case "refresh":
		if len(os.Args) != 3 {
			fmt.Println("Usage: weblet refresh <name>")
			fmt.Println("Re-downloads the icon and updates the desktop file")
			os.Exit(1)
		}
		name := os.Args[2]
		if err := wm.Refresh(name); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

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
