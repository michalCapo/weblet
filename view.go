package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	webview "github.com/webview/webview_go"
)

// runWebview opens a webview window with the given URL and title
// This function blocks until the window is closed
func runWebview(url, title string) {
	// Create webview (debug mode disabled)
	w := webview.New(false)
	defer w.Destroy()

	w.SetTitle(title)
	w.SetSize(1200, 800, webview.HintNone)
	w.Navigate(url)

	log.Printf("Opened weblet window: %s (%s)", title, url)

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Shutting down weblet...")
		w.Terminate()
	}()

	// Run webview (blocks until window is closed)
	w.Run()

	log.Println("Weblet window closed")
}
