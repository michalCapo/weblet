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

## 🎯 Perfect For

- Converting web-based tools (Gmail, GitHub, Slack, etc.) into desktop apps
- Creating a unified workspace with multiple web applications
- Developers who prefer CLI-based app management
- Users wanting lightweight alternatives to Electron apps

## Installation

```bash
go build -o weblet main.go
mv weblet ~/.local/bin/
```

## Usage

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

## Data Storage

Weblets are stored in `~/.weblet/weblets.json`. The tool automatically creates this directory and file when needed.

## Requirements

- Google Chrome or Chromium browser (Google Chrome is tried first, then Chromium as fallback)
- Linux (tested on Ubuntu/Debian)
- `wmctrl` package for window focusing (optional, install with `sudo apt install wmctrl`)

