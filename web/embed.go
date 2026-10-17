package web

import (
	"embed"
	"net/http"
	"strings"
)

//go:embed index.html app.js app.css public/* control-room/index.html control-room/app.js control-room/app.css
var files embed.FS

func Handler() http.Handler {
	server := http.FileServer(http.FS(files))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.webmanifest", "/sw.js", "/pwa.js", "/icons/icon-192.png", "/icons/icon-512.png":
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.Header().Set("Allow", "GET, HEAD")
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if r.URL.Path == "/manifest.webmanifest" {
				w.Header().Set("Content-Type", "application/manifest+json")
				w.Header().Set("Cache-Control", "no-store")
			} else if r.URL.Path == "/sw.js" {
				w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
				w.Header().Set("Cache-Control", "no-store")
			}
			assetRequest := r.Clone(r.Context())
			assetRequest.URL.Path = "/public" + r.URL.Path
			assetRequest.URL.RawPath = ""
			server.ServeHTTP(w, assetRequest)
			return
		}
		if r.URL.Path == "/" {
			data, err := files.ReadFile("index.html")
			if err != nil {
				http.Error(w, "index unavailable", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write(data)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/") {
			r.URL.Path += "index.html"
		}
		server.ServeHTTP(w, r)
	})
}

func ControlHandler() http.Handler {
	server := http.FileServer(http.FS(files))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/control-room" || r.URL.Path == "/control-room/" {
			data, err := files.ReadFile("control-room/index.html")
			if err != nil {
				http.Error(w, "control room unavailable", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(data)
			return
		}
		server.ServeHTTP(w, r)
	})
}
