package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cloverstd/ccmate/internal/config"
	"github.com/cloverstd/ccmate/internal/ent"
	"github.com/cloverstd/ccmate/internal/ent/project"
	enttask "github.com/cloverstd/ccmate/internal/ent/task"
	"github.com/cloverstd/ccmate/internal/model"
	"github.com/cloverstd/ccmate/internal/scheduler"
	"github.com/cloverstd/ccmate/internal/sse"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type TaskHandler struct {
	client *ent.Client
	cfg    *config.Config
	broker *sse.Broker
	sched  *scheduler.Scheduler
}

func NewTaskHandler(client *ent.Client, cfg *config.Config, broker *sse.Broker, sched *scheduler.Scheduler) *TaskHandler {
	return &TaskHandler{client: client, cfg: cfg, broker: broker, sched: sched}
}

func (h *TaskHandler) List(w http.ResponseWriter, r *http.Request) {
	query := h.client.Task.Query().
		Order(ent.Desc(enttask.FieldCreatedAt)).
		WithProject()

	if status := r.URL.Query().Get("status"); status != "" {
		query = query.Where(enttask.StatusEQ(enttask.Status(status)))
	}

	if projectID := r.URL.Query().Get("project_id"); projectID != "" {
		if id, err := strconv.Atoi(projectID); err == nil {
			query = query.Where(enttask.HasProjectWith(project.ID(id)))
		}
	}

	tasks, err := query.All(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to list tasks"}`, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, tasks)
}

type CreateTaskRequest struct {
	ProjectID   int    `json:"project_id"`
	IssueNumber int    `json:"issue_number"`
	TaskType    string `json:"type"`
}

// Create manually creates a task for an issue (P1-06).
func (h *TaskHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.ProjectID == 0 || req.IssueNumber == 0 {
		http.Error(w, `{"error":"project_id and issue_number are required"}`, http.StatusBadRequest)
		return
	}

	// Check project exists
	proj, err := h.client.Project.Get(r.Context(), req.ProjectID)
	if err != nil {
		http.Error(w, `{"error":"project not found"}`, http.StatusNotFound)
		return
	}

	// Check no active task for this issue
	exists, err := h.client.Task.Query().
		Where(
			enttask.HasProjectWith(project.ID(proj.ID)),
			enttask.IssueNumber(req.IssueNumber),
			enttask.StatusIn(
				enttask.StatusQueued, enttask.StatusRunning,
				enttask.StatusPaused, enttask.StatusWaitingUser,
			),
		).Exist(r.Context())
	if err != nil {
		http.Error(w, `{"error":"query failed"}`, http.StatusInternalServerError)
		return
	}
	if exists {
		http.Error(w, `{"error":"active task already exists for this issue"}`, http.StatusConflict)
		return
	}

	taskType := enttask.TypeIssueImplementation
	if req.TaskType == "manual_followup" {
		taskType = enttask.TypeManualFollowup
	}

	t, err := h.client.Task.Create().
		SetProject(proj).
		SetIssueNumber(req.IssueNumber).
		SetType(taskType).
		SetStatus(enttask.StatusQueued).
		SetTriggerSource(string(model.TriggerSourceWeb)).
		Save(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to create task"}`, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, t)
}

func (h *TaskHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}

	t, err := h.client.Task.Query().
		Where(enttask.ID(id)).
		WithProject().
		WithSessions().
		Only(r.Context())
	if err != nil {
		if ent.IsNotFound(err) {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"failed to get task"}`, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, t)
}

func (h *TaskHandler) Pause(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))
	if err := h.sched.PauseTask(r.Context(), id); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *TaskHandler) Resume(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))
	if err := h.sched.ResumeTask(r.Context(), id); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *TaskHandler) Retry(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))
	if err := h.sched.RetryTask(r.Context(), id); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *TaskHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))
	if err := h.sched.CancelTask(r.Context(), id); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type SendMessageRequest struct {
	Content     string `json:"content"`
	ContentType string `json:"content_type"`
}

func (h *TaskHandler) SendMessage(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))

	var req SendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	t, err := h.client.Task.Get(r.Context(), id)
	if err != nil {
		http.Error(w, `{"error":"task not found"}`, http.StatusNotFound)
		return
	}

	if t.CurrentSessionID == nil || *t.CurrentSessionID == 0 {
		http.Error(w, `{"error":"no active session"}`, http.StatusBadRequest)
		return
	}

	contentType := req.ContentType
	if contentType == "" {
		contentType = "text"
	}

	msg, err := h.client.SessionMessage.Create().
		SetSessionID(*t.CurrentSessionID).
		SetRole("user").
		SetContentType(contentType).
		SetContent(req.Content).
		Save(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to save message"}`, http.StatusInternalServerError)
		return
	}

	h.broker.Publish(fmt.Sprintf("task:%d", id), sse.Event{
		Type: "message.created",
		Data: msg,
	})

	if t.Status == enttask.StatusWaitingUser {
		_ = h.sched.HandleUserInput(r.Context(), id, model.AgentEvent{
			Type:    model.AgentEventMessageDelta,
			Payload: map[string]interface{}{"content": req.Content},
		})
	}

	writeJSON(w, http.StatusCreated, msg)
}

var allowedMimeTypes = map[string]bool{
	"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true,
}

// UploadAttachment handles file upload for a task (P1-04).
func (h *TaskHandler) UploadAttachment(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))

	maxSize := int64(h.cfg.Limits.MaxAttachmentSizeMB) * 1024 * 1024
	r.Body = http.MaxBytesReader(w, r.Body, maxSize)

	if err := r.ParseMultipartForm(maxSize); err != nil {
		http.Error(w, `{"error":"file too large"}`, http.StatusRequestEntityTooLarge)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, `{"error":"no file provided"}`, http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Validate MIME type
	mimeType := header.Header.Get("Content-Type")
	if !allowedMimeTypes[mimeType] {
		http.Error(w, `{"error":"unsupported file type"}`, http.StatusBadRequest)
		return
	}

	// Sanitize filename (prevent directory traversal)
	fileName := filepath.Base(header.Filename)
	fileName = strings.ReplaceAll(fileName, "..", "")

	// Generate storage path
	storageName := fmt.Sprintf("%s_%s", uuid.New().String()[:8], fileName)
	storageDir := filepath.Join(h.cfg.Storage.BasePath, h.cfg.Storage.AttachmentsDir)
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		http.Error(w, `{"error":"storage error"}`, http.StatusInternalServerError)
		return
	}
	storagePath := filepath.Join(storageDir, storageName)

	dst, err := os.Create(storagePath)
	if err != nil {
		http.Error(w, `{"error":"storage error"}`, http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	written, err := io.Copy(dst, file)
	if err != nil {
		os.Remove(storagePath)
		http.Error(w, `{"error":"write error"}`, http.StatusInternalServerError)
		return
	}

	attachment, err := h.client.Attachment.Create().
		SetTaskID(id).
		SetFileName(fileName).
		SetMimeType(mimeType).
		SetSize(written).
		SetStoragePath(storagePath).
		Save(r.Context())
	if err != nil {
		os.Remove(storagePath)
		http.Error(w, `{"error":"failed to save attachment"}`, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, attachment)
}

func (h *TaskHandler) EventStream(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	topic := fmt.Sprintf("task:%s", id)
	h.broker.ServeHTTP(w, r, topic)
}
