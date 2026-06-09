package handler

import (
	"io/fs"
	"net/http"

	webui "github.com/graphic/gofhir"
)

func NewStatic() http.Handler {
	sub, err := fs.Sub(webui.FS, "web/er-dashboard")
	if err != nil {
		panic("static sub: " + err.Error())
	}
	return http.StripPrefix("/static/", http.FileServer(http.FS(sub)))
}

func NewDashboard() http.HandlerFunc {
	content, err := webui.FS.ReadFile("web/er-dashboard/index.html")
	if err != nil {
		panic("dashboard read: " + err.Error())
	}
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	}
}

func NewReception() http.HandlerFunc {
	content, err := webui.FS.ReadFile("web/er-dashboard/reception.html")
	if err != nil {
		panic("reception read: " + err.Error())
	}
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	}
}