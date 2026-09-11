package web

import (
	"embed"
	"net/http"
	"strings"
)

//go:embed index.html app.js app.css control-room/index.html control-room/app.js control-room/app.css
var files embed.FS

func Handler() http.Handler {
	server := http.FileServer(http.FS(files))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			data, err := files.ReadFile("index.html")
			if err != nil {
				http.Error(w, "index unavailable", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(data)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/") {
			r.URL.Path += "index.html"
		}
		server.ServeHTTP(w, r)
	})
}

// ControlHandler serves the debug-only operator console. The daemon decides
// whether the route is reachable; this handler only serves embedded assets.
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
