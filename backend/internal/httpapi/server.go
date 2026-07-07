package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"aiopencad/backend/internal/config"
	"aiopencad/backend/internal/llm"
	"aiopencad/backend/internal/store"
)

type Server struct {
	store  *store.SQLite
	llm    *llm.Client
	cfg    config.Config
	jobsMu sync.Mutex
	jobs   map[string]*asyncJob
}

const apiPrefix = "/ai/open/cad"

func NewServer(store *store.SQLite, llmClient *llm.Client, cfg config.Config) *Server {
	return &Server{
		store: store,
		llm:   llmClient,
		cfg:   cfg,
		jobs:  map[string]*asyncJob{},
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	s.registerAPI(mux, apiPrefix+apiPrefix)
	s.registerAPI(mux, apiPrefix)
	s.registerAPI(mux, "")

	if s.cfg.FrontendDist != "" {
		mux.HandleFunc("/", s.handleFrontend)
	}

	return cors(mux)
}

func (s *Server) registerAPI(mux *http.ServeMux, prefix string) {
	mux.HandleFunc(prefix+"/health", s.handleHealth)
	mux.HandleFunc(prefix+"/generate-cad", s.requireMethod(http.MethodPost, s.handleGenerateCAD))
	mux.HandleFunc(prefix+"/repair-cad", s.requireMethod(http.MethodPost, s.handleRepairCAD))
	mux.HandleFunc(prefix+"/refine-cad", s.requireMethod(http.MethodPost, s.handleRefineCAD))
	mux.HandleFunc(prefix+"/generate-cad-async", s.requireMethod(http.MethodPost, s.handleGenerateCADAsync))
	mux.HandleFunc(prefix+"/repair-cad-async", s.requireMethod(http.MethodPost, s.handleRepairCADAsync))
	mux.HandleFunc(prefix+"/refine-cad-async", s.requireMethod(http.MethodPost, s.handleRefineCADAsync))
	mux.HandleFunc(prefix+"/jobs/", s.requireMethod(http.MethodGet, s.handleJobByID))
	mux.HandleFunc(prefix+"/projects", s.handleProjects)
	mux.HandleFunc(prefix+"/projects/", s.handleProjectByID)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"demoMode":    s.cfg.LLM.APIKey == "",
		"agentMode":   false,
		"llmModel":    s.cfg.LLM.Model,
		"reasoning":   s.cfg.LLM.ReasoningEffort,
		"webSearch":   s.cfg.LLM.EnableWebSearch,
		"serverTime":  time.Now().UTC(),
		"application": "ai-opencad",
	})
}

func (s *Server) handleGenerateCAD(w http.ResponseWriter, r *http.Request) {
	var req llm.GenerateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Language == "" {
		req.Language = "cascade-js"
	}
	req.Language = normalizeLanguage(req.Language)
	if req.Language != "cascade-js" {
		writeError(w, http.StatusBadRequest, "only cascade-js generation is supported in v1")
		return
	}

	resp, err := s.llm.GenerateCAD(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGenerateCADAsync(w http.ResponseWriter, r *http.Request) {
	var req llm.GenerateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Language == "" {
		req.Language = "cascade-js"
	}
	req.Language = normalizeLanguage(req.Language)
	if req.Language != "cascade-js" {
		writeError(w, http.StatusBadRequest, "only cascade-js generation is supported in v1")
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		writeError(w, http.StatusBadRequest, "prompt is required")
		return
	}

	job := s.startAsyncJob(clientID(r), "generate-cad", func(ctx context.Context) (any, error) {
		return s.llm.GenerateCAD(ctx, req)
	})
	writeJSON(w, http.StatusAccepted, job)
}

func normalizeLanguage(language string) string {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "", "cascade-js", "cascadejs", "js", "javascript":
		return "cascade-js"
	default:
		return strings.ToLower(strings.TrimSpace(language))
	}
}

func (s *Server) handleRepairCAD(w http.ResponseWriter, r *http.Request) {
	var req llm.RepairRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	resp, err := s.llm.RepairCAD(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleRepairCADAsync(w http.ResponseWriter, r *http.Request) {
	var req llm.RepairRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Code) == "" {
		writeError(w, http.StatusBadRequest, "code is required")
		return
	}

	job := s.startAsyncJob(clientID(r), "repair-cad", func(ctx context.Context) (any, error) {
		return s.llm.RepairCAD(ctx, req)
	})
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) handleRefineCAD(w http.ResponseWriter, r *http.Request) {
	var req llm.RefineRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	resp, err := s.llm.RefineCAD(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleRefineCADAsync(w http.ResponseWriter, r *http.Request) {
	var req llm.RefineRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Instruction) == "" {
		writeError(w, http.StatusBadRequest, "instruction is required")
		return
	}
	if strings.TrimSpace(req.Code) == "" {
		writeError(w, http.StatusBadRequest, "code is required")
		return
	}

	job := s.startAsyncJob(clientID(r), "refine-cad", func(ctx context.Context) (any, error) {
		return s.llm.RefineCAD(ctx, req)
	})
	writeJSON(w, http.StatusAccepted, job)
}

type asyncJob struct {
	ID        string
	OwnerID   string
	Kind      string
	Status    string
	Result    any
	Error     string
	Events    []asyncJobEvent
	CreatedAt time.Time
	UpdatedAt time.Time
}

type asyncJobEvent struct {
	Time    time.Time `json:"time"`
	Message string    `json:"message"`
}

type asyncJobSnapshot struct {
	ID        string          `json:"id"`
	OwnerID   string          `json:"ownerId,omitempty"`
	Kind      string          `json:"kind"`
	Status    string          `json:"status"`
	Result    any             `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
	Events    []asyncJobEvent `json:"events"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

func (s *Server) startAsyncJob(ownerID string, kind string, run func(context.Context) (any, error)) asyncJobSnapshot {
	now := time.Now().UTC()
	job := &asyncJob{
		ID:        newJobID(),
		OwnerID:   normalizeClientID(ownerID),
		Kind:      kind,
		Status:    "queued",
		CreatedAt: now,
		UpdatedAt: now,
	}
	job.Events = append(job.Events, asyncJobEvent{Time: now, Message: "Job queued."})

	s.jobsMu.Lock()
	s.cleanupJobsLocked(now)
	s.jobs[job.ID] = job
	s.jobsMu.Unlock()

	go s.runAsyncJob(job.ID, run)

	return snapshotJob(job)
}

func (s *Server) runAsyncJob(id string, run func(context.Context) (any, error)) {
	s.updateJob(id, func(job *asyncJob) {
		job.Status = "running"
		appendJobEvent(job, "Job started.")
	})

	timeout := s.cfg.LLM.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ctx = llm.WithProgress(ctx, func(message string) {
		s.addJobEvent(id, message)
	})

	result, err := run(ctx)
	s.updateJob(id, func(job *asyncJob) {
		if err != nil {
			job.Status = "failed"
			job.Error = err.Error()
			appendJobEvent(job, "Job failed: "+err.Error())
			return
		}
		job.Status = "done"
		job.Result = result
		appendJobEvent(job, "Job completed.")
	})
}

func (s *Server) updateJob(id string, update func(*asyncJob)) {
	s.jobsMu.Lock()
	defer s.jobsMu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return
	}
	update(job)
	job.UpdatedAt = time.Now().UTC()
}

func (s *Server) addJobEvent(id string, message string) {
	s.jobsMu.Lock()
	defer s.jobsMu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return
	}
	appendJobEvent(job, message)
	job.UpdatedAt = time.Now().UTC()
}

func (s *Server) handleJobByID(w http.ResponseWriter, r *http.Request) {
	id, stream := parseJobPath(r.URL.Path)
	if id == "" {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	if stream {
		s.handleJobStream(w, r, id)
		return
	}

	s.jobsMu.Lock()
	job, ok := s.jobs[id]
	var snapshot asyncJobSnapshot
	if ok && job.OwnerID == clientID(r) {
		snapshot = snapshotJob(job)
	}
	s.jobsMu.Unlock()

	if !ok || snapshot.ID == "" {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func parseJobPath(path string) (string, bool) {
	id := strings.TrimPrefix(path, apiPrefix+apiPrefix+"/jobs/")
	id = strings.TrimPrefix(id, apiPrefix+"/jobs/")
	id = strings.TrimPrefix(id, "/jobs/")
	id = strings.Trim(id, "/")
	stream := false
	if strings.HasSuffix(id, "/stream") {
		id = strings.TrimSuffix(id, "/stream")
		stream = true
	}
	return strings.Trim(id, "/"), stream
}

func (s *Server) handleJobStream(w http.ResponseWriter, r *http.Request, id string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	sent := 0
	for {
		snapshot, exists := s.snapshotJobByID(id, clientID(r))
		if !exists {
			writeSSE(w, "error", map[string]string{"error": "job not found"})
			flusher.Flush()
			return
		}

		for sent < len(snapshot.Events) {
			writeSSE(w, "event", snapshot.Events[sent])
			sent++
		}

		if snapshot.Status == "done" {
			writeSSE(w, "done", snapshot)
			flusher.Flush()
			return
		}
		if snapshot.Status == "failed" {
			writeSSE(w, "failed", snapshot)
			flusher.Flush()
			return
		}

		writeSSEComment(w, "heartbeat")
		flusher.Flush()

		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) snapshotJobByID(id string, ownerID string) (asyncJobSnapshot, bool) {
	s.jobsMu.Lock()
	defer s.jobsMu.Unlock()
	job, ok := s.jobs[id]
	if !ok || job.OwnerID != normalizeClientID(ownerID) {
		return asyncJobSnapshot{}, false
	}
	return snapshotJob(job), true
}

func writeSSE(w http.ResponseWriter, event string, value any) {
	raw, err := json.Marshal(value)
	if err != nil {
		raw = []byte(`{"error":"failed to encode event"}`)
	}
	_, _ = w.Write([]byte("event: " + event + "\n"))
	_, _ = w.Write([]byte("data: " + string(raw) + "\n\n"))
}

func writeSSEComment(w http.ResponseWriter, comment string) {
	_, _ = w.Write([]byte(": " + comment + "\n\n"))
}

func snapshotJob(job *asyncJob) asyncJobSnapshot {
	return asyncJobSnapshot{
		ID:        job.ID,
		OwnerID:   job.OwnerID,
		Kind:      job.Kind,
		Status:    job.Status,
		Result:    job.Result,
		Error:     job.Error,
		Events:    append([]asyncJobEvent(nil), job.Events...),
		CreatedAt: job.CreatedAt,
		UpdatedAt: job.UpdatedAt,
	}
}

func appendJobEvent(job *asyncJob, message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	const maxEvents = 80
	job.Events = append(job.Events, asyncJobEvent{
		Time:    time.Now().UTC(),
		Message: message,
	})
	if len(job.Events) > maxEvents {
		job.Events = job.Events[len(job.Events)-maxEvents:]
	}
}

func (s *Server) cleanupJobsLocked(now time.Time) {
	const keepFor = 30 * time.Minute
	for id, job := range s.jobs {
		if now.Sub(job.UpdatedAt) > keepFor {
			delete(s.jobs, id)
		}
	}
}

func newJobID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return hex.EncodeToString(raw[:])
	}
	return strings.ReplaceAll(time.Now().UTC().Format("20060102150405.000000000"), ".", "")
}

func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		projects, err := s.store.ListProjectsForOwner(r.Context(), clientID(r))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, projects)
	case http.MethodPost:
		var input store.ProjectInput
		if !decodeJSON(w, r, &input) {
			return
		}
		project, err := s.store.CreateProjectForOwner(r.Context(), clientID(r), input)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, project)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleProjectByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, apiPrefix+apiPrefix+"/projects/")
	id = strings.TrimPrefix(id, apiPrefix+"/projects/")
	id = strings.TrimPrefix(id, "/projects/")
	id = strings.Trim(id, "/")
	if id == "" {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		project, err := s.store.GetProjectForOwner(r.Context(), clientID(r), id)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, project)
	case http.MethodPut:
		var input store.ProjectInput
		if !decodeJSON(w, r, &input) {
			return
		}
		project, err := s.store.UpdateProjectForOwner(r.Context(), clientID(r), id, input)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, project)
	case http.MethodDelete:
		if err := s.store.DeleteProjectForOwner(r.Context(), clientID(r), id); err != nil {
			writeStoreError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleFrontend(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == apiPrefix || r.URL.Path == apiPrefix+apiPrefix {
		http.Redirect(w, r, r.URL.Path+"/", http.StatusFound)
		return
	}

	dist, err := filepath.Abs(s.cfg.FrontendDist)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "invalid frontend dist")
		return
	}

	clean := frontendAssetPath(r.URL.Path)
	path := filepath.Join(dist, clean)
	absPath, err := filepath.Abs(path)
	if err != nil || !isWithinDir(dist, absPath) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if clean == "." || clean == string(filepath.Separator) {
		absPath = filepath.Join(dist, "index.html")
	}
	if info, err := os.Stat(absPath); err != nil || info.IsDir() {
		absPath = filepath.Join(dist, "index.html")
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, absPath)
}

func frontendAssetPath(urlPath string) string {
	if urlPath == "/" || urlPath == apiPrefix || urlPath == apiPrefix+"/" {
		return "."
	}
	for strings.HasPrefix(urlPath, apiPrefix+"/") {
		urlPath = strings.TrimPrefix(urlPath, apiPrefix+"/")
		urlPath = "/" + urlPath
	}
	urlPath = strings.TrimPrefix(urlPath, "/")
	return filepath.Clean(urlPath)
}

func isWithinDir(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func (s *Server) requireMethod(method string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		next(w, r)
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

func clientID(r *http.Request) string {
	if value := r.URL.Query().Get("clientId"); value != "" {
		return normalizeClientID(value)
	}
	return normalizeClientID(r.Header.Get("X-AI-OpenCAD-Client-ID"))
}

func normalizeClientID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "default"
	}
	if len(value) > 128 {
		return value[:128]
	}
	return value
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-AI-OpenCAD-Client-ID")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
