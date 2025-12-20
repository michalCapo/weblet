//go:build !no_native

package view

/*
#cgo linux pkg-config: gtk+-3.0 webkit2gtk-4.1 gdk-3.0 gdk-x11-3.0 x11
#include <gtk/gtk.h>
#include <gdk/gdk.h>
#include <gdk/gdkx.h>
#include <webkit2/webkit2.h>
#include <stdlib.h>
#include <string.h>

static GtkWidget *main_window = NULL;
static WebKitWebView *main_webview = NULL;
static int app_running = 0;

static void on_destroy(GtkWidget *widget, gpointer data) {
    app_running = 0;
    gtk_main_quit();
}

// Set WM_CLASS after window is realized
static void on_realize(GtkWidget *widget, gpointer data) {
    const char *wm_class = (const char *)data;
    GdkWindow *gdk_window = gtk_widget_get_window(widget);
    if (gdk_window != NULL && GDK_IS_X11_WINDOW(gdk_window)) {
        gdk_x11_window_set_utf8_property(gdk_window, "_GTK_APPLICATION_ID", wm_class);
        // Set WM_CLASS using Xlib
        Display *display = GDK_DISPLAY_XDISPLAY(gdk_display_get_default());
        Window xwindow = GDK_WINDOW_XID(gdk_window);
        XClassHint *class_hint = XAllocClassHint();
        if (class_hint) {
            class_hint->res_name = (char *)wm_class;
            class_hint->res_class = (char *)wm_class;
            XSetClassHint(display, xwindow, class_hint);
            XFree(class_hint);
        }
    }
}

// Handle permission requests (microphone, camera, notifications, etc.)
static gboolean on_permission_request(WebKitWebView *web_view,
                                       WebKitPermissionRequest *request,
                                       gpointer user_data) {
    // Auto-grant media (microphone/camera) permissions
    if (WEBKIT_IS_USER_MEDIA_PERMISSION_REQUEST(request)) {
        g_print("Granting microphone/camera permission\n");
        webkit_permission_request_allow(request);
        return TRUE;
    }

    // Auto-grant notification permissions
    if (WEBKIT_IS_NOTIFICATION_PERMISSION_REQUEST(request)) {
        g_print("Granting notification permission\n");
        webkit_permission_request_allow(request);
        return TRUE;
    }

    // Auto-grant geolocation permissions
    if (WEBKIT_IS_GEOLOCATION_PERMISSION_REQUEST(request)) {
        g_print("Granting geolocation permission\n");
        webkit_permission_request_allow(request);
        return TRUE;
    }

    // Auto-grant device info permissions (enumerate devices)
    if (WEBKIT_IS_DEVICE_INFO_PERMISSION_REQUEST(request)) {
        g_print("Granting device info permission\n");
        webkit_permission_request_allow(request);
        return TRUE;
    }

    // For other permissions, allow by default
    webkit_permission_request_allow(request);
    return TRUE;
}

void weblet_init(const char *title, const char *url, const char *data_dir, const char *icon_path, const char *wm_class, int width, int height) {
    // Set application name for GNOME
    g_set_prgname(wm_class);
    g_set_application_name(title);

    gtk_init(NULL, NULL);

    // Create window
    main_window = gtk_window_new(GTK_WINDOW_TOPLEVEL);
    gtk_window_set_title(GTK_WINDOW(main_window), title);
    gtk_window_set_default_size(GTK_WINDOW(main_window), width, height);
    gtk_window_set_position(GTK_WINDOW(main_window), GTK_WIN_POS_CENTER);

    // Set window role (helps with window matching)
    gtk_window_set_role(GTK_WINDOW(main_window), wm_class);

    g_signal_connect(main_window, "destroy", G_CALLBACK(on_destroy), NULL);

    // Connect realize signal to set WM_CLASS after window is mapped
    char *wm_class_copy = strdup(wm_class);
    g_signal_connect(main_window, "realize", G_CALLBACK(on_realize), wm_class_copy);

    // Set window icon if provided
    if (icon_path != NULL && icon_path[0] != '\0') {
        GError *error = NULL;
        GdkPixbuf *icon = gdk_pixbuf_new_from_file(icon_path, &error);
        if (icon != NULL) {
            gtk_window_set_icon(GTK_WINDOW(main_window), icon);
            g_object_unref(icon);
        } else if (error != NULL) {
            g_error_free(error);
        }
    }

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

    // Configure settings for full web app support
    WebKitSettings *settings = webkit_web_view_get_settings(main_webview);

    // Set Chrome user-agent to avoid "Unsupported Browser" on Discord, Teams, etc.
    webkit_settings_set_user_agent(settings,
        "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36");

    webkit_settings_set_enable_javascript(settings, TRUE);
    webkit_settings_set_javascript_can_access_clipboard(settings, TRUE);

    // Audio/Video support
    webkit_settings_set_enable_media_stream(settings, TRUE);        // Microphone/Camera
    webkit_settings_set_enable_mediasource(settings, TRUE);         // MSE for video playback
    webkit_settings_set_enable_webaudio(settings, TRUE);            // Web Audio API
    webkit_settings_set_enable_media(settings, TRUE);               // HTML5 media elements
    webkit_settings_set_media_playback_requires_user_gesture(settings, FALSE);  // Allow autoplay
    webkit_settings_set_enable_encrypted_media(settings, TRUE);     // DRM/encrypted media

    // Hardware acceleration for better media performance
    webkit_settings_set_hardware_acceleration_policy(settings, WEBKIT_HARDWARE_ACCELERATION_POLICY_ALWAYS);

    // Other features
    webkit_settings_set_enable_webgl(settings, TRUE);
    webkit_settings_set_enable_developer_extras(settings, FALSE);

    // Connect permission request handler for microphone/camera/notifications
    g_signal_connect(main_webview, "permission-request", G_CALLBACK(on_permission_request), NULL);

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
        // Add timer to check for focus requests from IPC (every 100ms)
        g_timeout_add(100, on_focus_check, NULL);
        gtk_main();
    }
}

void weblet_quit() {
    if (app_running && main_window != NULL) {
        gtk_widget_destroy(main_window);
    }
}

void weblet_focus() {
    if (app_running && main_window != NULL) {
        gtk_window_present(GTK_WINDOW(main_window));
    }
}

// Process pending GTK events from non-main thread safely
static int focus_requested = 0;

gboolean on_focus_check(gpointer data) {
    if (focus_requested) {
        focus_requested = 0;
        weblet_focus();
    }
    return TRUE; // Keep timer running
}

void weblet_request_focus() {
    focus_requested = 1;
}
*/
import "C"

import (
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"unsafe"
)

// tryFocusExistingWindow attempts to connect to an existing weblet instance
// Returns true if focus request was sent successfully, false if no instance exists
func tryFocusExistingWindow(socketPath string) bool {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return false
	}
	defer conn.Close()

	// Send focus command
	conn.Write([]byte("focus"))
	return true
}

// startFocusListener starts a Unix socket listener for focus requests
func startFocusListener(socketPath string) (net.Listener, error) {
	// Remove stale socket if exists
	os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return // Listener closed
			}

			buf := make([]byte, 16)
			n, _ := conn.Read(buf)
			if n > 0 && string(buf[:n]) == "focus" {
				log.Println("Received focus request from another instance")
				C.weblet_request_focus()
			}
			conn.Close()
		}
	}()

	return listener, nil
}

// runWebview opens a webview window with the given URL and title
// Uses persistent storage for cookies, localStorage, and other web data
// This function blocks until the window is closed
func RunWebview(webletURL, title string) {
	// Get data directory for this weblet
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Failed to get home directory: %v", err)
	}

	dataDir := filepath.Join(homeDir, ".weblet", "data", title)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}

	// Socket path for single-instance communication
	sockDir := filepath.Join(homeDir, ".weblet", "sockets")
	os.MkdirAll(sockDir, 0755)
	socketPath := filepath.Join(sockDir, title+".sock")

	// Try to focus existing instance first
	if tryFocusExistingWindow(socketPath) {
		log.Printf("Focused existing weblet window: %s", title)
		return
	}

	// Find icon for this weblet
	iconPath := findWebletIcon(homeDir, webletURL)

	// WM_CLASS should match StartupWMClass in .desktop file
	// Format: weblet-<name> to match weblet-<name>.desktop
	wmClass := fmt.Sprintf("weblet-%s", title)

	log.Printf("Opened weblet window: %s (%s)", title, webletURL)
	log.Printf("Data directory: %s", dataDir)

	// Start socket listener for focus requests
	listener, err := startFocusListener(socketPath)
	if err != nil {
		log.Printf("Warning: Failed to start focus listener: %v", err)
	} else {
		defer func() {
			listener.Close()
			os.Remove(socketPath)
		}()
	}

	// Convert strings to C strings
	cTitle := C.CString(title)
	cURL := C.CString(webletURL)
	cDataDir := C.CString(dataDir)
	cIconPath := C.CString(iconPath)
	cWMClass := C.CString(wmClass)
	defer C.free(unsafe.Pointer(cTitle))
	defer C.free(unsafe.Pointer(cURL))
	defer C.free(unsafe.Pointer(cDataDir))
	defer C.free(unsafe.Pointer(cIconPath))
	defer C.free(unsafe.Pointer(cWMClass))

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Shutting down weblet...")
		C.weblet_quit()
	}()

	// Initialize and run webview with persistent storage
	C.weblet_init(cTitle, cURL, cDataDir, cIconPath, cWMClass, 1200, 800)
	C.weblet_run()

	log.Println("Weblet window closed")
}

// findWebletIcon looks for an icon file for the given URL
func findWebletIcon(homeDir, webletURL string) string {
	iconDir := filepath.Join(homeDir, ".weblet", "icons")

	// Parse the URL to get the host
	parsedURL, err := url.Parse(webletURL)
	if err != nil {
		return ""
	}

	host := parsedURL.Host

	// Try PNG first, then ICO
	extensions := []string{".png", ".ico"}
	for _, ext := range extensions {
		iconPath := filepath.Join(iconDir, host+ext)
		if _, err := os.Stat(iconPath); err == nil {
			return iconPath
		}
	}

	return ""
}
