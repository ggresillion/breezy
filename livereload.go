package breezy

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"time"

	_ "embed"
	"github.com/gorilla/websocket"
)

//go:embed script.html
var scriptHTML string

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins in development
	},
}

// Generate a unique server ID when the server starts
var serverStartTime = time.Now().Unix()

type responseWriter struct {
	http.ResponseWriter
	body       *bytes.Buffer
	statusCode int
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	return rw.body.Write(b)
}

func (rw *responseWriter) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
}

func (rw *responseWriter) Header() http.Header {
	return rw.ResponseWriter.Header()
}

// isHTMLResponse checks if the response looks like HTML.
// We only inject the script in root HTML documents.
func isHTMLResponse(body string, headers http.Header) bool {
	contentType := headers.Get("Content-Type")

	if strings.Contains(contentType, "text/html") {
		return true
	}

	bodyLower := strings.ToLower(body)
	return strings.Contains(bodyLower, "<html") ||
		strings.Contains(bodyLower, "<!doctype html") ||
		strings.Contains(bodyLower, "<head>") ||
		(strings.Contains(bodyLower, "<body") && strings.Contains(bodyLower, "</body>"))
}

func isDevelopmentMode(r *http.Request) bool {
	host := r.Host
	return strings.Contains(host, "localhost") ||
		strings.Contains(host, "127.0.0.1") ||
		strings.HasPrefix(host, "localhost:") ||
		strings.HasPrefix(host, "127.0.0.1:")
}

func injectScript(body string) string {
	if strings.Contains(body, "</head>") {
		return strings.Replace(body, "</head>", scriptHTML+"\n</head>", 1)
	}
	if strings.Contains(body, "</body>") {
		return strings.Replace(body, "</body>", scriptHTML+"\n</body>", 1)
	}
	if strings.Contains(body, "</html>") {
		return strings.Replace(body, "</html>", scriptHTML+"\n</html>", 1)
	}
	return body + scriptHTML
}

// handleLiveReload handles the WebSocket connection for live reload.
func handleLiveReload(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("WebSocket upgrade failed", "error", err)
		return
	}
	defer conn.Close()

	err = conn.WriteJSON(map[string]any{
		"type":      "server-info",
		"startTime": serverStartTime,
	})
	if err != nil {
		slog.Error("Failed to send server info", "error", err)
		return
	}

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

// Middleware wraps an http.Handler with live reload functionality
func Middleware(next http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws/livereload", handleLiveReload)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !isDevelopmentMode(r) {
			next.ServeHTTP(w, r)
			return
		}

		wrapper := &responseWriter{
			ResponseWriter: w,
			body:           &bytes.Buffer{},
			statusCode:     http.StatusOK,
		}

		next.ServeHTTP(wrapper, r)

		body := wrapper.body.String()

		if !isHTMLResponse(body, wrapper.Header()) {
			if wrapper.statusCode != http.StatusOK {
				w.WriteHeader(wrapper.statusCode)
			}
			w.Write(wrapper.body.Bytes())
			return
		}

		modifiedBody := injectScript(body)

		if wrapper.statusCode != http.StatusOK {
			w.WriteHeader(wrapper.statusCode)
		}

		for key, values := range wrapper.Header() {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}

		w.Write([]byte(modifiedBody))
	})

	return mux
}
