package web

import (
	"embed"
	"net/http"
	"strings"
)

//go:embed index.html app.js app.css
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
