//go:build no_native

package view

import (
	"log"
)

// RunWebview is a stub that informs the user that native mode is not available
func RunWebview(webletURL, title string) {
	log.Fatalf("Error: Native webview mode is not available in this build. Please use Chrome mode (default) or rebuild with WebKit support.")
}
