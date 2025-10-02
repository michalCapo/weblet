package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

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

	return wm.saveWeblets()
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
	return wm.saveWeblets()
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

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage:")
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
