# Weblet

**A lightweight CLI tool that transforms web applications into desktop apps using Chrome's app mode**

Weblet allows you to quickly convert any website into a desktop application with a simple command-line interface. It manages multiple web apps, tracks running processes, and provides window focusing capabilities for a seamless desktop experience.

## ✨ Key Features

- 🚀 **Quick Setup**: Add web apps with a single command
- 🖥️ **Desktop Integration**: Runs websites as Chrome app windows
- 📋 **Process Management**: Tracks and manages running instances
- 🎯 **Smart Focusing**: Automatically focuses existing windows instead of creating duplicates
- 💾 **Persistent Storage**: Saves configurations in `~/.weblet/weblets.json`
- 🐧 **Linux Optimized**: Built for Linux with window manager integration
- 🖱️ **Desktop Shortcuts**: Automatically creates desktop shortcuts for easy access

## 🎯 Perfect For

- Converting web-based tools (Gmail, GitHub, Slack, etc.) into desktop apps
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

This will scan for available browsers (`google-chrome`, `chromium`, `chromium-browser`) and either:
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
This will start the weblet as a Chrome app if it's not running, or focus on it if it's already running.

### Add a new weblet
```bash
weblet add <name> <url>
```

### Remove a weblet
```bash
weblet remove <name>
```

## Examples

```bash
# First-time setup (if multiple browsers detected)
weblet setup

# Add a weblet for Gmail
weblet add gmail https://mail.google.com

# Add a weblet for GitHub
weblet add github https://github.com

# List all weblets
weblet list

# Run Gmail
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

- Google Chrome or Chromium browser (automatically detected and configured on first run)
- Linux (tested on Ubuntu/Debian)
- `wmctrl` package for window focusing (optional, install with `sudo apt install wmctrl`)

### Browser Support
Weblet supports the following browsers (detected automatically):
- `google-chrome` (preferred)
- `chromium`
- `chromium-browser`

On first run, if multiple browsers are detected, you'll be prompted to choose your preferred browser via `weblet setup`.

