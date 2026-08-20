package web

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mateoosoriodelhonte/processpilot/internal/ai"
	"github.com/mateoosoriodelhonte/processpilot/internal/analysis"
	"github.com/mateoosoriodelhonte/processpilot/internal/app"
)

//go:embed templates/*.html static/*
var webAssets embed.FS

var templates = parsePageTemplates()

type pageData struct {
	Title       string
	Navigation  string
	State       app.State
	Process     *analysis.Process
	Explanation ai.Explanation
	History     []analysis.HistoryPoint
}

func (server *Server) pageRoutes() {
	server.mux.HandleFunc("GET /{$}", server.page("overview", "Why is my Mac slow?"))
	server.mux.HandleFunc("GET /applications", server.page("applications", "Applications"))
	server.mux.HandleFunc("GET /processes", server.page("processes", "Raw processes"))
	server.mux.HandleFunc("GET /processes/{pid}", server.processPage)
	server.mux.HandleFunc("GET /history", server.page("history", "Resource history"))
	server.mux.HandleFunc("GET /anomalies", server.page("anomalies", "Anomalies"))
	server.mux.HandleFunc("GET /privacy", server.page("privacy", "Privacy"))
	server.mux.HandleFunc("GET /settings", server.page("settings", "Local settings"))
	staticFiles, err := fs.Sub(webAssets, "static")
	if err != nil {
		panic(err)
	}
	server.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFiles))))
}

func (server *Server) page(name, title string) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if !validQuery(request, nil) {
			http.Error(writer, "invalid query", http.StatusBadRequest)
			return
		}
		state, ok := server.service.Current()
		if !ok {
			http.Error(writer, "Waiting for the first telemetry snapshot.", http.StatusServiceUnavailable)
			return
		}
		data := pageData{Title: title, Navigation: name, State: state}
		if name == "history" {
			history, err := server.service.History(request.Context(), "", time.Now().UTC().Add(-24*time.Hour), 2_000)
			if err != nil {
				http.Error(writer, "History is temporarily unavailable.", http.StatusInternalServerError)
				return
			}
			data.History = history
		}
		server.render(writer, name, data)
	}
}

func (server *Server) processPage(writer http.ResponseWriter, request *http.Request) {
	if !validQuery(request, nil) {
		http.Error(writer, "invalid query", http.StatusBadRequest)
		return
	}
	parsed, err := strconv.ParseUint(request.PathValue("pid"), 10, 32)
	if err != nil || parsed == 0 {
		http.Error(writer, "PID must be a positive integer.", http.StatusBadRequest)
		return
	}
	state, ok := server.service.Current()
	if !ok {
		http.Error(writer, "Waiting for the first telemetry snapshot.", http.StatusServiceUnavailable)
		return
	}
	process, ok := server.service.Process(uint32(parsed))
	if !ok {
		http.NotFound(writer, request)
		return
	}
	explanation, err := server.service.Explain(request.Context(), uint32(parsed))
	if err != nil {
		http.Error(writer, "Explanation is temporarily unavailable.", http.StatusServiceUnavailable)
		return
	}
	server.render(writer, "process", pageData{
		Title: process.Observed.Name, Navigation: "processes", State: state,
		Process: &process, Explanation: explanation,
	})
}

func (server *Server) render(writer http.ResponseWriter, name string, data pageData) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates[name].ExecuteTemplate(writer, "base", data); err != nil {
		http.Error(writer, "Page rendering failed.", http.StatusInternalServerError)
	}
}

func parsePageTemplates() map[string]*template.Template {
	functions := template.FuncMap{
		"bytes":   formatBytes,
		"percent": func(value float64) string { return fmt.Sprintf("%.1f%%", value) },
		"time": func(milliseconds uint64) string {
			return time.UnixMilli(int64(milliseconds)).Local().Format("3:04:05 PM")
		},
		"dateTime": func(value time.Time) string { return value.Local().Format("Jan 2, 3:04 PM") },
		"lower":    func(value any) string { return strings.ToLower(fmt.Sprint(value)) },
		"category": func(value analysis.Category) string {
			return strings.ReplaceAll(strings.ToLower(string(value)), "_", " ")
		},
	}
	result := make(map[string]*template.Template)
	for _, name := range []string{"overview", "applications", "processes", "process", "history", "anomalies", "privacy", "settings"} {
		result[name] = template.Must(template.New("base").Funcs(functions).ParseFS(
			webAssets, "templates/base.html", "templates/"+name+".html",
		))
	}
	return result
}

func formatBytes(value uint64) string {
	const (
		kib = uint64(1) << 10
		mib = uint64(1) << 20
		gib = uint64(1) << 30
	)
	switch {
	case value >= gib:
		return fmt.Sprintf("%.1f GB", float64(value)/float64(gib))
	case value >= mib:
		return fmt.Sprintf("%.0f MB", float64(value)/float64(mib))
	case value >= kib:
		return fmt.Sprintf("%.0f KB", float64(value)/float64(kib))
	default:
		return fmt.Sprintf("%d B", value)
	}
}
