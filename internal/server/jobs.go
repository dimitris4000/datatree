package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// Jobs keep request-sized work off the browser request lifetime. Progress is
// stage based: queued, running and terminal. Cancellation prevents a later commit.
type operationJob struct {
	owner      *browserSession
	ID         string          `json:"id"`
	Status     string          `json:"status"`
	Stage      string          `json:"stage"`
	HTTPStatus int             `json:"httpStatus,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      string          `json:"error,omitempty"`
	created    time.Time
	cancel     context.CancelFunc
	committed  bool
}
type operationJobKey struct{}

func asyncOperation(path string) bool {
	switch path {
	case "/api/import", "/api/open", "/api/apply", "/api/patch", "/api/edit", "/api/history", "/api/diff-revert", "/api/convert":
		return true
	}
	return false
}
func (s *Server) jobSession(w http.ResponseWriter, r *http.Request) (*browserSession, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.session(w, r)
	for id, candidate := range s.sessions {
		if candidate == session {
			return session, id
		}
	}
	panic("registered session missing")
}
func (s *Server) startJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" || !asyncOperation(r.URL.Path) {
		fail(w, errors.New("asynchronous operation unsupported"))
		return
	}
	select {
	case s.jobSlots <- struct{}{}:
	default:
		jobFailure(w, http.StatusTooManyRequests, "four operations are already running")
		return
	}
	started := false
	defer func() {
		if !started {
			<-s.jobSlots
		}
	}()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, (int64(s.limits.MaxBytes)+1048576)*6+8192))
	if err != nil {
		fail(w, err)
		return
	}
	if r.URL.Path == "/api/convert" {
		var in struct {
			SaveFilename *string `json:"saveFilename"`
		}
		if err := json.Unmarshal(body, &in); err != nil {
			fail(w, err)
			return
		}
		if in.SaveFilename != nil {
			fail(w, errors.New("asynchronous conversion cannot save a local copy"))
			return
		}
	}
	session, id := s.jobSession(w, r)
	session.mu.Lock()
	if session.jobs == nil {
		session.jobs = map[string]*operationJob{}
	}
	active := 0
	for _, job := range session.jobs {
		if job.Status == "queued" || job.Status == "running" {
			active++
		}
	}
	if active >= 2 {
		session.mu.Unlock()
		jobFailure(w, http.StatusTooManyRequests, "two operations are already running")
		return
	}
	for len(session.jobs) >= 8 {
		oldest := ""
		var when time.Time
		for id, job := range session.jobs {
			if job.Status == "queued" || job.Status == "running" {
				continue
			}
			if oldest == "" || job.created.Before(when) {
				oldest = id
				when = job.created
			}
		}
		if oldest == "" {
			break
		}
		delete(session.jobs, oldest)
	}
	token := make([]byte, 16)
	if _, err = rand.Read(token); err != nil {
		session.mu.Unlock()
		fail(w, err)
		return
	}
	ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
	job := &operationJob{ID: hex.EncodeToString(token), Status: "queued", Stage: "queued", created: time.Now(), cancel: cancel, owner: session}
	session.jobs[job.ID] = job
	session.mu.Unlock()
	worker := r.Clone(context.WithValue(ctx, operationJobKey{}, job))
	u := *worker.URL
	worker.URL = &u
	q := u.Query()
	q.Del("async")
	worker.URL.RawQuery = q.Encode()
	worker.Body = io.NopCloser(bytes.NewReader(body))
	worker.Header = worker.Header.Clone()
	worker.Header.Del("Cookie")
	worker.AddCookie(&http.Cookie{Name: "datatree_session", Value: id})
	started = true
	go func() {
		defer func() { <-s.jobSlots }()
		defer cancel()
		session.mu.Lock()
		job.Status = "running"
		job.Stage = strings.TrimPrefix(worker.URL.Path, "/api/")
		session.mu.Unlock()
		result := &jobResponse{header: make(http.Header)}
		if ctx.Err() == nil {
			s.ServeHTTP(result, worker)
		}
		session.mu.Lock()
		defer session.mu.Unlock()
		if ctx.Err() != nil && !job.committed {
			job.Status = "cancelled"
			job.Stage = "cancelled"
			return
		}
		job.HTTPStatus = result.status
		if job.HTTPStatus == 0 {
			job.HTTPStatus = 200
		}
		if job.HTTPStatus >= 400 {
			job.Status = "failed"
			job.Stage = "failed"
			var failure struct{ Error string }
			json.Unmarshal(result.body.Bytes(), &failure)
			job.Error = failure.Error
			if job.Error == "" {
				job.Error = result.body.String()
			}
			return
		}
		job.Status = "complete"
		job.Stage = "complete"
		if json.Valid(result.body.Bytes()) {
			job.Result = bytes.Clone(result.body.Bytes())
		}
	}()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, map[string]any{"job": job.ID, "status": "queued"})
}
func (s *Server) jobsRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "POST" {
		jobFailure(w, 405, "method not allowed")
		return
	}
	id := r.URL.Query().Get("id")
	if r.Method == "POST" {
		var in struct {
			ID string `json:"id"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil {
			fail(w, err)
			return
		}
		if err := dec.Decode(new(any)); err != io.EOF {
			fail(w, errors.New("trailing request data"))
			return
		}
		id = in.ID
	}
	session, _ := s.jobSession(w, r)
	session.mu.Lock()
	defer session.mu.Unlock()
	job := session.jobs[id]
	if job == nil {
		jobFailure(w, 404, "job not found")
		return
	}
	if r.Method == "POST" {
		if job.committed || job.Status == "complete" {
			fail(w, errors.New("operation already committed"))
			return
		}
		if job.Status == "queued" || job.Status == "running" {
			job.cancel()
			job.Stage = "cancelling"
		}
	}
	writeJSON(w, job)
}

type jobResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *jobResponse) Header() http.Header { return w.header }
func (w *jobResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *jobResponse) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	return w.body.Write(b)
}

func jobFailure(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	writeJSON(w, map[string]string{"error": message})
}
