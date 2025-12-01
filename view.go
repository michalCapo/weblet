package main

/*
#cgo linux pkg-config: gtk+-3.0 webkit2gtk-4.1
#include <gtk/gtk.h>
#include <webkit2/webkit2.h>
#include <stdlib.h>

static GtkWidget *main_window = NULL;
static WebKitWebView *main_webview = NULL;
static int app_running = 0;

static void on_destroy(GtkWidget *widget, gpointer data) {
    app_running = 0;
    gtk_main_quit();
}

void weblet_init(const char *title, const char *url, const char *data_dir, int width, int height) {
    gtk_init(NULL, NULL);

    // Create window
    main_window = gtk_window_new(GTK_WINDOW_TOPLEVEL);
    gtk_window_set_title(GTK_WINDOW(main_window), title);
    gtk_window_set_default_size(GTK_WINDOW(main_window), width, height);
    gtk_window_set_position(GTK_WINDOW(main_window), GTK_WIN_POS_CENTER);
    g_signal_connect(main_window, "destroy", G_CALLBACK(on_destroy), NULL);

    // Create WebKitWebsiteDataManager with persistent storage
    WebKitWebsiteDataManager *data_manager = webkit_website_data_manager_new(
        "base-data-directory", data_dir,
        "base-cache-directory", data_dir,
        NULL
    );

    // Create WebKitWebContext with the data manager
    WebKitWebContext *context = webkit_web_context_new_with_website_data_manager(data_manager);

    // Configure cookie manager for persistence
    WebKitCookieManager *cookie_manager = webkit_website_data_manager_get_cookie_manager(data_manager);
    gchar *cookie_file = g_build_filename(data_dir, "cookies.sqlite", NULL);
    webkit_cookie_manager_set_persistent_storage(
        cookie_manager,
        cookie_file,
        WEBKIT_COOKIE_PERSISTENT_STORAGE_SQLITE
    );
    webkit_cookie_manager_set_accept_policy(cookie_manager, WEBKIT_COOKIE_POLICY_ACCEPT_ALWAYS);
    g_free(cookie_file);

    // Create webview with the context
    main_webview = WEBKIT_WEB_VIEW(webkit_web_view_new_with_context(context));

    // Configure settings
    WebKitSettings *settings = webkit_web_view_get_settings(main_webview);
    webkit_settings_set_enable_javascript(settings, TRUE);
    webkit_settings_set_javascript_can_access_clipboard(settings, TRUE);
    webkit_settings_set_enable_media_stream(settings, TRUE);
    webkit_settings_set_enable_mediasource(settings, TRUE);
    webkit_settings_set_enable_webaudio(settings, TRUE);
    webkit_settings_set_enable_webgl(settings, TRUE);
    webkit_settings_set_enable_developer_extras(settings, FALSE);

    // Add webview to window
    gtk_container_add(GTK_CONTAINER(main_window), GTK_WIDGET(main_webview));

    // Load URL
    webkit_web_view_load_uri(main_webview, url);

    // Show all widgets
    gtk_widget_show_all(main_window);

    app_running = 1;
}

void weblet_run() {
    if (app_running) {
        gtk_main();
    }
}

void weblet_quit() {
    if (app_running && main_window != NULL) {
        gtk_widget_destroy(main_window);
    }
}
*/
import "C"

import (
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"unsafe"
)

// runWebview opens a webview window with the given URL and title
// Uses persistent storage for cookies, localStorage, and other web data
// This function blocks until the window is closed
func runWebview(url, title string) {
	// Get data directory for this weblet
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Failed to get home directory: %v", err)
	}

	dataDir := filepath.Join(homeDir, ".weblet", "data", title)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}

	log.Printf("Opened weblet window: %s (%s)", title, url)
	log.Printf("Data directory: %s", dataDir)

	// Convert strings to C strings
	cTitle := C.CString(title)
	cURL := C.CString(url)
	cDataDir := C.CString(dataDir)
	defer C.free(unsafe.Pointer(cTitle))
	defer C.free(unsafe.Pointer(cURL))
	defer C.free(unsafe.Pointer(cDataDir))

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Shutting down weblet...")
		C.weblet_quit()
	}()

	// Initialize and run webview with persistent storage
	C.weblet_init(cTitle, cURL, cDataDir, 1200, 800)
	C.weblet_run()

	log.Println("Weblet window closed")
}
