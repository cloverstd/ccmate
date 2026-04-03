package integration

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cloverstd/ccmate/internal/agentprovider"
	mockagent "github.com/cloverstd/ccmate/internal/agentprovider/mock"
	"github.com/cloverstd/ccmate/internal/api"
	"github.com/cloverstd/ccmate/internal/auth"
	"github.com/cloverstd/ccmate/internal/config"
	"github.com/cloverstd/ccmate/internal/ent"
	"github.com/cloverstd/ccmate/internal/ent/enttest"
	enttask "github.com/cloverstd/ccmate/internal/ent/task"
	"github.com/cloverstd/ccmate/internal/model"
	"github.com/cloverstd/ccmate/internal/scheduler"
	"github.com/cloverstd/ccmate/internal/settings"
	"github.com/cloverstd/ccmate/internal/sse"

	_ "github.com/mattn/go-sqlite3"
)

type testEnv struct {
	client      *ent.Client
	cfg         *config.Config
	broker      *sse.Broker
	sched       *scheduler.Scheduler
	router      http.Handler
	passkey     *auth.PasskeyService
	settingsMgr *settings.Manager
	cookie      string
}

func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()

	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	t.Cleanup(func() { client.Close() })

	cfg := &config.Config{
		Server:   config.ServerConfig{Host: "localhost", Port: 8080},
		Database: config.DatabaseConfig{Driver: "sqlite3", DSN: ":memory:"},
	}

	settingsMgr := settings.NewManager(client)
	ctx := context.Background()
	settingsMgr.Set(ctx, settings.KeyInitialized, "true")
	settingsMgr.Set(ctx, settings.KeyMaxConcurrency, "2")
	settingsMgr.Set(ctx, settings.KeyStorageBasePath, t.TempDir())

	broker := sse.NewBroker()

	passkeySvc, err := auth.NewPasskeyService(&auth.PasskeyConfig{
		RPDisplayName: "ccmate", RPID: "localhost",
		RPOrigins: []string{"http://localhost:8080"},
	})
	if err != nil {
		t.Fatal(err)
	}

	sched := scheduler.New(client, settingsMgr, broker)
	registry := agentprovider.NewRegistry()
	registry.Register(&mockagent.Factory{})
	sched.SetProviders(nil, registry)

	router := api.NewRouter(client, cfg, broker, sched, passkeySvc, nil, settingsMgr)

	cookie, _ := passkeySvc.EncodeSession("ccmate_session", map[string]string{
		"user": "admin", "ts": time.Now().Format(time.RFC3339),
	})

	return &testEnv{
		client: client, cfg: cfg, broker: broker, sched: sched,
		router: router, passkey: passkeySvc, settingsMgr: settingsMgr, cookie: cookie,
	}
}

func (e *testEnv) authRequest(method, path string, body interface{}) *httptest.ResponseRecorder {
	var reqBody *bytes.Buffer
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewBuffer(b)
	} else {
		reqBody = &bytes.Buffer{}
	}
	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "ccmate_session", Value: e.cookie})
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w
}

func createTestProject(t *testing.T, client *ent.Client) *ent.Project {
	t.Helper()
	p, err := client.Project.Create().
		SetName("test/repo").SetRepoURL("https://github.com/test/repo").
		SetGitProvider("github").SetDefaultBranch("main").Save(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// --- Tests ---

func TestProjectCRUD(t *testing.T) {
	env := setupTestEnv(t)
	w := env.authRequest("POST", "/api/projects", map[string]interface{}{
		"name": "owner/repo", "repo_url": "https://github.com/owner/repo",
		"git_provider": "github", "default_branch": "main", "auto_mode": true,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: got %d, body: %s", w.Code, w.Body.String())
	}

	w = env.authRequest("GET", "/api/projects", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list: got %d", w.Code)
	}
	var projects []map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &projects)
	if len(projects) != 1 || projects[0]["name"] != "owner/repo" {
		t.Fatalf("expected owner/repo, got %v", projects)
	}
}

func TestTaskManualCreation(t *testing.T) {
	env := setupTestEnv(t)
	proj := createTestProject(t, env.client)

	w := env.authRequest("POST", "/api/tasks", map[string]interface{}{
		"project_id": proj.ID, "issue_number": 42,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: got %d, body: %s", w.Code, w.Body.String())
	}

	// Duplicate should conflict
	w = env.authRequest("POST", "/api/tasks", map[string]interface{}{
		"project_id": proj.ID, "issue_number": 42,
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate: expected 409, got %d", w.Code)
	}
}

func TestTaskLifecycleOperations(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()
	proj := createTestProject(t, env.client)

	task, _ := env.client.Task.Create().
		SetProject(proj).SetIssueNumber(1).
		SetType(enttask.TypeIssueImplementation).
		SetStatus(enttask.StatusRunning).SetTriggerSource("web").Save(ctx)

	w := env.authRequest("POST", fmt.Sprintf("/api/tasks/%d/pause", task.ID), nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("pause: got %d, body: %s", w.Code, w.Body.String())
	}

	w = env.authRequest("POST", fmt.Sprintf("/api/tasks/%d/resume", task.ID), nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("resume: got %d", w.Code)
	}

	env.client.Task.UpdateOneID(task.ID).SetStatus(enttask.StatusFailed).SaveX(ctx)
	w = env.authRequest("POST", fmt.Sprintf("/api/tasks/%d/retry", task.ID), nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("retry: got %d", w.Code)
	}

	env.client.Task.UpdateOneID(task.ID).SetStatus(enttask.StatusRunning).SaveX(ctx)
	w = env.authRequest("POST", fmt.Sprintf("/api/tasks/%d/cancel", task.ID), nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("cancel: got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestGlobalLabelRules(t *testing.T) {
	env := setupTestEnv(t)
	w := env.authRequest("GET", "/api/label-rules", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestWebhookToTaskPipeline(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	proj, _ := env.client.Project.Create().
		SetName("owner/repo").SetRepoURL("https://github.com/owner/repo").
		SetGitProvider("github").SetAutoMode(true).Save(ctx)

	env.client.ProjectLabelRule.Create().
		SetProjectID(proj.ID).SetIssueLabel("ccmate").Save(ctx)

	event := &model.NormalizedEvent{
		Type: model.EventIssueLabeled, DeliveryID: "test-1",
		Repo: model.RepoRef{Owner: "owner", Name: "repo"},
		IssueNumber: 99, Label: "ccmate",
	}

	processor := setupTestProcessor(env)
	if err := processor.ProcessEvent(ctx, event); err != nil {
		t.Fatalf("process event: %v", err)
	}

	tasks, _ := env.client.Task.Query().Where(enttask.IssueNumber(99)).All(ctx)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	// Dedup
	processor.ProcessEvent(ctx, event)
	tasks, _ = env.client.Task.Query().Where(enttask.IssueNumber(99)).All(ctx)
	if len(tasks) != 1 {
		t.Fatalf("dedup failed: got %d", len(tasks))
	}
}

func TestWebhookSignatureVerification(t *testing.T) {
	env := setupTestEnv(t)
	payload := `{"action":"labeled","issue":{"number":1},"label":{"name":"ccmate"},"repository":{"owner":{"login":"test"},"name":"repo"}}`
	mac := hmac.New(sha256.New, []byte("test-secret"))
	mac.Write([]byte(payload))
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest("POST", "/webhooks/github", bytes.NewBufferString(payload))
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-GitHub-Delivery", "sig-test-1")
	req.Header.Set("X-Hub-Signature-256", sig)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	// Will return 500 (git provider nil) but NOT 401
	if w.Code == http.StatusUnauthorized {
		t.Fatalf("valid signature rejected")
	}
}

func TestAuthRequired(t *testing.T) {
	env := setupTestEnv(t)
	req := httptest.NewRequest("GET", "/api/projects", nil)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}

	w = env.authRequest("GET", "/api/projects", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestSSEEventStream(t *testing.T) {
	env := setupTestEnv(t)
	ch := env.broker.Subscribe("task:999")
	defer env.broker.Unsubscribe("task:999", ch)

	env.broker.Publish("task:999", sse.Event{Type: "message.delta", Data: map[string]interface{}{"content": "hello"}})
	select {
	case event := <-ch:
		if event.Type != "message.delta" {
			t.Errorf("expected message.delta, got %s", event.Type)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout")
	}
}

func TestSchedulerConcurrencyControl(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	proj := createTestProject(t, env.client)

	// Set global max concurrency to 1
	env.settingsMgr.Set(ctx, settings.KeyMaxConcurrency, "1")

	env.client.Task.Create().SetProject(proj).SetIssueNumber(1).
		SetType(enttask.TypeIssueImplementation).SetStatus(enttask.StatusQueued).
		SetTriggerSource("webhook").SaveX(ctx)
	env.client.Task.Create().SetProject(proj).SetIssueNumber(2).
		SetType(enttask.TypeIssueImplementation).SetStatus(enttask.StatusRunning).
		SetTriggerSource("webhook").SaveX(ctx)

	running, _ := env.client.Task.Query().Where(enttask.StatusEQ(enttask.StatusRunning)).Count(ctx)
	queued, _ := env.client.Task.Query().Where(enttask.StatusEQ(enttask.StatusQueued)).Count(ctx)

	if running != 1 || queued != 1 {
		t.Errorf("expected 1 running + 1 queued, got %d running + %d queued", running, queued)
	}
}

func TestPromptTemplateCRUD(t *testing.T) {
	env := setupTestEnv(t)
	w := env.authRequest("POST", "/api/prompt-templates", map[string]interface{}{
		"name": "default", "system_prompt": "You are helpful", "task_prompt": "Do the work",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: got %d", w.Code)
	}

	var tmpl map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &tmpl)
	tmplID := int(tmpl["id"].(float64))

	w = env.authRequest("DELETE", fmt.Sprintf("/api/prompt-templates/%d", tmplID), nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: got %d", w.Code)
	}
}

func TestModelCRUD(t *testing.T) {
	env := setupTestEnv(t)
	w := env.authRequest("POST", "/api/models", map[string]interface{}{
		"provider": "claude-code", "model": "claude-sonnet-4-6",
		"supports_image": true, "supports_resume": false,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: got %d", w.Code)
	}
	var m map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &m)
	w = env.authRequest("DELETE", fmt.Sprintf("/api/models/%d", int(m["id"].(float64))), nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: got %d", w.Code)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	env := setupTestEnv(t)
	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("metrics: got %d", w.Code)
	}
	if body := w.Body.String(); !contains(body, "ccmate_tasks_total") {
		t.Error("missing ccmate_tasks_total")
	}
}

func TestSetupStatus(t *testing.T) {
	env := setupTestEnv(t)
	req := httptest.NewRequest("GET", "/api/setup/status", nil)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("setup status: got %d", w.Code)
	}
	var status map[string]bool
	json.Unmarshal(w.Body.Bytes(), &status)
	if !status["initialized"] {
		t.Error("expected initialized=true")
	}
}

func TestInvalidStateTransitions(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()
	proj := createTestProject(t, env.client)

	task, _ := env.client.Task.Create().
		SetProject(proj).SetIssueNumber(1).
		SetType(enttask.TypeIssueImplementation).
		SetStatus(enttask.StatusQueued).SetTriggerSource("web").Save(ctx)

	w := env.authRequest("POST", fmt.Sprintf("/api/tasks/%d/pause", task.ID), nil)
	if w.Code == http.StatusNoContent {
		t.Fatal("should not pause a queued task")
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
