# Weblet

**A lightweight CLI tool that transforms web applications into desktop apps using Chrome's app mode**

Weblet allows you to quickly convert any website into a desktop application with a simple command-line interface. It manages multiple web apps, tracks running processes, and provides window focusing capabilities for a seamless desktop experience.

## ✨ Key Features

- 🚀 **Quick Setup**: Add web apps with a single command
- 🖥️ **Desktop Integration**: Runs websites as Chrome app windows
- 📋 **Process Management**: Tracks and manages running instances
- 🎯 **Smart Focusing**: Run the same weblet multiple times—it focuses the existing window instead of creating duplicates
- 💾 **Persistent Storage**: Saves configurations in `~/.weblet/weblets.json`
- 🐧 **Linux Optimized**: Built for Linux with window manager integration
- 🖱️ **Desktop Shortcuts**: Automatically creates desktop shortcuts for easy access

## 🎯 Perfect For

- Converting web-based tools (Gmail, GitHub, Slack, Discord, etc.) into desktop apps
- WebRTC-heavy apps (Discord, Zoom, Teams) that need full audio device support
- Creating a unified workspace with multiple web applications
- Developers who prefer CLI-based app management
- Users wanting lightweight alternatives to Electron apps

## Installation

```bash
# Build with version information (recommended)
chmod +x build.sh
./build.sh
mv weblet ~/.local/bin/

# Or build manually with version (format: days_since_2024.HHMM)
DAYS=$(( ( $(date +%s) - $(date -d "2024-01-01" +%s) ) / 86400 ))
go build -ldflags "-X main.version=${DAYS}.$(date +%H%M)" -o weblet
mv weblet ~/.local/bin/

# Or build without version (will show "dev")
go build -o weblet
mv weblet ~/.local/bin/
```

## Usage

### First-time setup
On first run, weblet will automatically detect available browsers and configure the best option. If multiple browsers are found, you'll be prompted to run the setup command:

```bash
weblet setup
```

This will:
1. **Check for window management tools** (`wmctrl` and `xdotool`)
   - These are required for the window focusing feature
   - Warns if missing and provides installation commands
2. **Scan for available browsers** (`google-chrome`, `chromium`, `chromium-browser`) and either:
   - Automatically select the only available browser, or
   - Present an interactive menu to choose your preferred browser

The browser preference is saved in `~/.weblet/weblet.json` and will be used for all future weblet launches.

### Check version
```bash
weblet version
```
Shows the build version in format `days_since_2024.HHMM` (e.g., `639.2212` = 639 days since Jan 1, 2024, built at 22:12). If built without version flags, shows `dev`.

### List all weblets
```bash
weblet list
```

### Run a weblet
```bash
weblet <name>
```
Starts the weblet if it's not running, or focuses the existing window if it's already running.

**Example:**
```bash
weblet discord          # Opens Discord weblet
weblet discord          # Focuses the existing window (no duplicate!)
```

### Add and run a weblet (Quick Start)
```bash
weblet <name> <url>
```
This will add a new weblet and immediately run it. Perfect for quick setup!

**Smart behavior:**
- If the weblet doesn't exist → adds it and runs it
- If the weblet exists with the same URL → just runs it (idempotent)
- If the weblet exists with a different URL → updates the URL and runs it

You can run this command multiple times without errors!

### Add a weblet without running
```bash
weblet add <name> <url>
```
Adds a weblet to your collection without launching it.

### Toggle native mode (experimental)
```bash
weblet native <name>
```
Toggles between Chrome mode (default, full audio support) and native webview mode (lighter weight, experimental).

**Note:** Chrome mode is the default and recommended for most apps, especially WebRTC-heavy ones like Discord. Native mode is lighter but may have compatibility issues with some web apps.

### Remove a weblet
```bash
weblet remove <name>
```

## Examples

```bash
# First-time setup (if multiple browsers detected)
weblet setup

# Quick start: Add and run Gmail immediately
weblet gmail https://mail.google.com

# Run the same command again - no error! (idempotent)
weblet gmail https://mail.google.com
# Output: Weblet 'gmail' already exists with this URL

# Update Gmail URL and run
weblet gmail https://mail.google.com/mail/u/1
# Output: Updated weblet 'gmail' with new URL

# Quick start: Add and run GitHub immediately
weblet github https://github.com

# Or add without running
weblet add slack https://app.slack.com

# List all weblets
weblet list

# Run an existing weblet
weblet slack

# Run again - will focus existing window instead of creating new one
weblet gmail

# Remove GitHub weblet
weblet remove github
```

## Desktop Shortcuts

When you add a weblet, Weblet automatically creates a desktop shortcut that appears in your application launcher (GNOME, KDE, etc.). This allows you to:

- Launch weblets directly from your application menu
- Pin weblets to your dock or taskbar
- Use keyboard shortcuts to launch weblets
- Find weblets in your application search

Desktop shortcuts are created in `~/.local/share/applications/` and include:
- Application name and description
- High-quality website icon (automatically downloaded, prioritizing PNG format)
- Proper categorization in the Network/WebBrowser category
- Startup notification support

### Icon Detection

Weblet automatically fetches the best available icon for each web application by:
1. **HTML Parsing**: Scans the website's HTML for declared icons (`apple-touch-icon`, `favicon`, Open Graph images)
2. **Common Locations**: Tries standard icon paths (favicon-32x32.png, apple-touch-icon.png, etc.)
3. **Format Priority**: Prioritizes PNG files over ICO for better quality
4. **Smart Fallback**: Falls back to standard favicon.ico if no PNG is available

Icons are cached in `~/.weblet/icons/` and reused across launches.

When you remove a weblet, its desktop shortcut is automatically cleaned up.

## Data Storage

Weblets are stored in `~/.weblet/weblets.json`. Browser configuration is saved in `~/.weblet/weblet.json`. The tool automatically creates this directory and files when needed. Favicons are cached in `~/.weblet/icons/` for desktop shortcuts.

## Versioning

Weblet uses a simplified versioning system based on build date/time:

- **Format**: `<days_since_2024>.<HHMM>`
- **Example**: `639.2212` means:
  - `639` = 639 days since January 1, 2024
  - `2212` = Built at 22:12 (10:12 PM)
- **Benefits**: 
  - Short, readable version numbers (7-8 digits)
  - Easy to determine build age and time
  - Unique versions for each build
  - Human-readable format

The version is automatically embedded during build using Go's linker flags. If built without version information, it displays `dev`.

## Requirements

### Core Requirements
- **Google Chrome or Chromium** (automatically detected)
- **Linux** (tested on Ubuntu/Debian with GNOME/KDE)

### Window Management (for focus/reuse feature)
To prevent duplicate windows when running the same weblet multiple times, install at least one of:
- `wmctrl` (recommended): `sudo apt install wmctrl`
- `xdotool` (fallback): `sudo apt install xdotool`

Run `weblet setup` to verify installation.

**Without these tools:** Each `weblet discord` invocation will create a new window instead of focusing the existing one.

### Browser Support
Weblet supports the following browsers (detected automatically):
- `google-chrome` (preferred)
- `chromium`
- `chromium-browser`

On first run, if multiple browsers are detected, you'll be prompted to choose your preferred browser via `weblet setup`.

## 🔧 Troubleshooting

### "Running `weblet discord` creates a new window every time"
**Solution:** Install window management tools to enable the focus feature:
```bash
sudo apt install wmctrl
# or for fallback:
sudo apt install xdotool
```
Then verify installation:
```bash
weblet setup
```

### "Microphone/Camera not working in weblet"
**Solutions:**
1. Ensure you're using **Chrome mode** (default for most weblets):
   ```bash
   weblet native discord  # First time: switches to native (lighter)
   weblet native discord  # Second time: switches back to Chrome
   ```
2. Grant browser permissions when prompted in the weblet window
3. Check your system audio/camera settings

### "Some websites say 'Browser not supported'"
**Solution:** Weblet sets a Chrome user-agent by default. If a site still complains:
1. Try Chrome mode: Switch weblets to Chrome mode if using native
2. File a bug report with the website name

## 📝 Data Storage

- **Weblets config**: `~/.weblet/weblets.json`
- **Chrome data**: `~/.weblet/chrome-data/` (per-weblet isolation)
- **Native webview data**: `~/.weblet/data/`
- **Icons**: `~/.weblet/icons/`
- **Desktop shortcuts**: `~/.local/share/applications/weblet-*.desktop`

