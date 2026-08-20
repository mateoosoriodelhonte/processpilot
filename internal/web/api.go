package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mateoosoriodelhonte/processpilot/internal/ai"
	"github.com/mateoosoriodelhonte/processpilot/internal/analysis"
	"github.com/mateoosoriodelhonte/processpilot/internal/app"
	"github.com/mateoosoriodelhonte/processpilot/internal/protocol"
)

const DefaultPort = 7345

type Server struct {
	service  *app.Service
	mux      *http.ServeMux
	settings Settings
}

type Settings struct {
	Interval            time.Duration
	Retention           time.Duration
	ExplanationProvider string
}

func Address(port int) (string, error) {
	if port < 1_024 || port > 65_535 {
		return "", errors.New("port must be between 1024 and 65535")
	}
	return fmt.Sprintf("127.0.0.1:%d", port), nil
}

func New(service *app.Service) *Server {
	return NewWithSettings(service, Settings{
		Interval: 2 * time.Second, Retention: 7 * 24 * time.Hour, ExplanationProvider: "No AI",
	})
}

func NewWithSettings(service *app.Service, settings Settings) *Server {
	server := &Server{service: service, mux: http.NewServeMux(), settings: settings}
	server.routes()
	server.pageRoutes()
	return server
}

func (server *Server) Handler() http.Handler {
	return securityHeaders(server.mux)
}

func (server *Server) routes() {
	server.mux.HandleFunc("GET /api/v1/system", server.system)
	server.mux.HandleFunc("GET /api/v1/processes", server.processes)
	server.mux.HandleFunc("GET /api/v1/processes/{pid}", server.process)
	server.mux.HandleFunc("GET /api/v1/applications", server.applications)
	server.mux.HandleFunc("GET /api/v1/history", server.history)
	server.mux.HandleFunc("GET /api/v1/anomalies", server.anomalies)
	server.mux.HandleFunc("GET /api/v1/events", server.events)
}

func (server *Server) system(writer http.ResponseWriter, request *http.Request) {
	if !validQuery(request, nil) {
		writeError(writer, http.StatusBadRequest, "INVALID_QUERY", "This endpoint does not accept query parameters.")
		return
	}
	state, ok := server.service.Current()
	if !ok {
		writeError(writer, http.StatusServiceUnavailable, "TELEMETRY_UNAVAILABLE", "No telemetry snapshot is available yet.")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"data": struct {
		Observed  protocol.SystemSample `json:"observed"`
		Pressure  analysis.Pressure     `json:"pressure"`
		Timestamp uint64                `json:"timestampUnixMs"`
		Demo      bool                  `json:"demo"`
	}{state.Snapshot.System, state.Pressure, state.Snapshot.TimestampUnixMS, state.Demo}})
}

func (server *Server) processes(writer http.ResponseWriter, request *http.Request) {
	page, pageSize, ok := paginationQuery(request)
	if !ok {
		writeError(writer, http.StatusBadRequest, "INVALID_QUERY", "page must be positive and pageSize must be between 1 and 200.")
		return
	}
	state, available := server.service.Current()
	if !available {
		writeError(writer, http.StatusServiceUnavailable, "TELEMETRY_UNAVAILABLE", "No telemetry snapshot is available yet.")
		return
	}
	data, pagination := paginate(state.Analysis.Processes, page, pageSize)
	writeJSON(writer, http.StatusOK, map[string]any{"data": data, "pagination": pagination, "demo": state.Demo, "processesTruncated": state.Snapshot.ProcessesTruncated})
}

func (server *Server) process(writer http.ResponseWriter, request *http.Request) {
	if !validQuery(request, nil) {
		writeError(writer, http.StatusBadRequest, "INVALID_QUERY", "This endpoint does not accept query parameters.")
		return
	}
	parsed, err := strconv.ParseUint(request.PathValue("pid"), 10, 32)
	if err != nil || parsed == 0 {
		writeError(writer, http.StatusBadRequest, "INVALID_PID", "PID must be a positive integer.")
		return
	}
	process, explanation, err := server.service.ProcessDetail(request.Context(), uint32(parsed))
	if err != nil {
		if errors.Is(err, app.ErrProcessNotFound) {
			writeError(writer, http.StatusNotFound, "PROCESS_NOT_FOUND", "The process is not in the current snapshot.")
			return
		}
		writeError(writer, http.StatusServiceUnavailable, "EXPLANATION_UNAVAILABLE", "The explanation is temporarily unavailable.")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"data": processDetail{
		Observed: process.Observed, Classification: process.Classification,
		Ownership: process.Ownership, Explanation: explanation,
	}})
}

func (server *Server) applications(writer http.ResponseWriter, request *http.Request) {
	page, pageSize, ok := paginationQuery(request)
	if !ok {
		writeError(writer, http.StatusBadRequest, "INVALID_QUERY", "page must be positive and pageSize must be between 1 and 200.")
		return
	}
	state, available := server.service.Current()
	if !available {
		writeError(writer, http.StatusServiceUnavailable, "TELEMETRY_UNAVAILABLE", "No telemetry snapshot is available yet.")
		return
	}
	data, pagination := paginate(state.Analysis.Applications, page, pageSize)
	writeJSON(writer, http.StatusOK, map[string]any{"data": data, "pagination": pagination, "demo": state.Demo, "processesTruncated": state.Snapshot.ProcessesTruncated})
}

func (server *Server) history(writer http.ResponseWriter, request *http.Request) {
	if !validQuery(request, map[string]bool{"application": true, "since": true, "limit": true}) {
		writeError(writer, http.StatusBadRequest, "INVALID_QUERY", "History accepts only application, since, and limit.")
		return
	}
	query := request.URL.Query()
	state, available := server.service.Current()
	if !available {
		writeError(writer, http.StatusServiceUnavailable, "TELEMETRY_UNAVAILABLE", "No telemetry snapshot is available yet.")
		return
	}
	since := time.UnixMilli(int64(state.Snapshot.TimestampUnixMS)).UTC().Add(-24 * time.Hour)
	if raw := query.Get("since"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(writer, http.StatusBadRequest, "INVALID_QUERY", "since must be an RFC3339 timestamp.")
			return
		}
		since = parsed
	}
	limit := 1_000
	if raw := query.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 10_000 {
			writeError(writer, http.StatusBadRequest, "INVALID_QUERY", "limit must be between 1 and 10000.")
			return
		}
		limit = parsed
	}
	history, err := server.service.History(request.Context(), query.Get("application"), since, limit)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "HISTORY_UNAVAILABLE", "History is temporarily unavailable.")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"data": history})
}

func (server *Server) anomalies(writer http.ResponseWriter, request *http.Request) {
	if !validQuery(request, nil) {
		writeError(writer, http.StatusBadRequest, "INVALID_QUERY", "This endpoint does not accept query parameters.")
		return
	}
	state, ok := server.service.Current()
	if !ok {
		writeError(writer, http.StatusServiceUnavailable, "TELEMETRY_UNAVAILABLE", "No telemetry snapshot is available yet.")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"data": state.Anomalies, "demo": state.Demo})
}

func (server *Server) events(writer http.ResponseWriter, request *http.Request) {
	if !validQuery(request, nil) {
		writeError(writer, http.StatusBadRequest, "INVALID_QUERY", "This endpoint does not accept query parameters.")
		return
	}
	flusher, ok := writer.(http.Flusher)
	if !ok {
		writeError(writer, http.StatusInternalServerError, "STREAM_UNAVAILABLE", "Live updates are unavailable.")
		return
	}
	updates, cancel, err := server.service.Subscribe()
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "STREAM_LIMIT", "Too many live dashboard connections.")
		return
	}
	defer cancel()
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Connection", "keep-alive")
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-request.Context().Done():
			return
		case state, open := <-updates:
			if !open {
				return
			}
			payload, err := json.Marshal(eventState{
				TimestampUnixMS: state.TimestampUnixMS, Pressure: state.Pressure,
				Applications: state.Applications, Anomalies: state.Anomalies, Demo: state.Demo,
				ProcessesTruncated: state.ProcessesTruncated,
			})
			if err != nil {
				return
			}
			_, _ = fmt.Fprintf(writer, "event: snapshot\ndata: %s\n\n", payload)
			flusher.Flush()
		case <-heartbeat.C:
			_, _ = fmt.Fprint(writer, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

type processDetail struct {
	Observed       protocol.ProcessSample  `json:"observed"`
	Classification analysis.Classification `json:"classification"`
	Ownership      analysis.Ownership      `json:"ownership"`
	Explanation    ai.Explanation          `json:"explanation"`
}

type eventState struct {
	TimestampUnixMS    uint64                 `json:"timestampUnixMs"`
	Pressure           analysis.Pressure      `json:"pressure"`
	Applications       []analysis.Application `json:"applications"`
	Anomalies          []analysis.Anomaly     `json:"anomalies"`
	Demo               bool                   `json:"demo"`
	ProcessesTruncated bool                   `json:"processesTruncated"`
}

type pagination struct {
	Page       int `json:"page"`
	PageSize   int `json:"pageSize"`
	TotalItems int `json:"totalItems"`
	TotalPages int `json:"totalPages"`
}

func paginate[T any](values []T, page, pageSize int) ([]T, pagination) {
	total := len(values)
	totalPages := (total + pageSize - 1) / pageSize
	if totalPages == 0 || page > totalPages {
		return []T{}, pagination{Page: page, PageSize: pageSize, TotalItems: total, TotalPages: totalPages}
	}
	start := (page - 1) * pageSize
	end := min(start+pageSize, total)
	return values[start:end], pagination{Page: page, PageSize: pageSize, TotalItems: total, TotalPages: totalPages}
}

func paginationQuery(request *http.Request) (int, int, bool) {
	if !validQuery(request, map[string]bool{"page": true, "pageSize": true}) {
		return 0, 0, false
	}
	page, pageSize := 1, 50
	var err error
	if raw := request.URL.Query().Get("page"); raw != "" {
		page, err = strconv.Atoi(raw)
		if err != nil || page < 1 {
			return 0, 0, false
		}
	}
	if raw := request.URL.Query().Get("pageSize"); raw != "" {
		pageSize, err = strconv.Atoi(raw)
		if err != nil || pageSize < 1 || pageSize > 200 {
			return 0, 0, false
		}
	}
	return page, pageSize, true
}

func validQuery(request *http.Request, allowed map[string]bool) bool {
	for key, values := range request.URL.Query() {
		if !allowed[key] || len(values) != 1 {
			return false
		}
	}
	return true
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeError(writer http.ResponseWriter, status int, code, message string) {
	writeJSON(writer, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; script-src 'self'; style-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("X-Frame-Options", "DENY")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		writer.Header().Set("Cache-Control", "no-store")
		if !isLoopbackAuthority(request.Host) || !isLoopbackOrigin(request.Header.Get("Origin")) {
			writeError(writer, http.StatusMisdirectedRequest, "LOOPBACK_REQUIRED", "ProcessPilot accepts requests only from this Mac's loopback origin.")
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func isLoopbackOrigin(origin string) bool {
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && parsed.Scheme == "http" && parsed.User == nil && parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == "" && isLoopbackAuthority(parsed.Host)
}

func isLoopbackAuthority(authority string) bool {
	host := authority
	if parsedHost, _, err := net.SplitHostPort(authority); err == nil {
		host = parsedHost
	} else if strings.Count(authority, ":") == 1 {
		return false
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
