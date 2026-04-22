package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cloverstd/ccmate/internal/ent"
	enttask "github.com/cloverstd/ccmate/internal/ent/task"
	"github.com/cloverstd/ccmate/internal/model"
	"github.com/cloverstd/ccmate/internal/notify"
	"github.com/cloverstd/ccmate/internal/settings"
)

// maxErrorRunes bounds error excerpts shown in Telegram messages.
const maxErrorRunes = 300

// botTokenPattern matches Telegram bot tokens (id:secret) for sanitization.
var botTokenPattern = regexp.MustCompile(`\d+:[A-Za-z0-9_\-]+`)

const (
	KeyEnabled        = "notify_telegram_enabled"
	KeyBotToken       = "notify_telegram_bot_token"
	KeyChatID         = "notify_telegram_chat_id"
	KeyLastUpdateID   = "notify_telegram_last_update_id"
)

// terminalStatuses are statuses where buttons are no longer needed.
var terminalStatuses = map[string]bool{
	"succeeded": true,
	"failed":    true,
	"cancelled": true,
}

// Provider sends notifications via Telegram Bot API and supports inline-button
// callbacks. A single message per task is reused via editMessageText.
type Provider struct {
	settingsMgr *settings.Manager
	client      *ent.Client
	httpClient  *http.Client
}

func New(settingsMgr *settings.Manager, client *ent.Client) *Provider {
	return &Provider{
		settingsMgr: settingsMgr,
		client:      client,
		httpClient:  &http.Client{Timeout: 35 * time.Second},
	}
}

func (p *Provider) Name() string { return "telegram" }

func (p *Provider) Send(ctx context.Context, event notify.NotifyEvent) error {
	enabled := p.settingsMgr.GetWithDefault(ctx, KeyEnabled, "false")
	if enabled != "true" {
		return nil
	}

	botToken, _ := p.settingsMgr.Get(ctx, KeyBotToken)
	chatID, _ := p.settingsMgr.Get(ctx, KeyChatID)
	if botToken == "" || chatID == "" {
		return fmt.Errorf("telegram bot_token or chat_id not configured")
	}

	text := formatMessage(event)
	keyboard := buildKeyboard(event)

	// Test events use TaskID==0; just send a fresh message.
	if event.TaskID == 0 || p.client == nil {
		_, err := p.sendMessage(ctx, botToken, chatID, text, keyboard)
		return err
	}

	t, err := p.client.Task.Get(ctx, event.TaskID)
	if err != nil {
		return fmt.Errorf("loading task: %w", err)
	}

	// If we already have a message for this task in the same chat, edit it.
	if t.TelegramMessageID != nil && t.TelegramChatID != nil && *t.TelegramChatID == chatID {
		if err := p.editMessage(ctx, botToken, chatID, *t.TelegramMessageID, text, keyboard); err == nil {
			return nil
		}
		// Fall through to a fresh send if editing fails (message deleted, etc.).
	}

	msgID, err := p.sendMessage(ctx, botToken, chatID, text, keyboard)
	if err != nil {
		return err
	}

	_, err = p.client.Task.UpdateOneID(event.TaskID).
		SetTelegramChatID(chatID).
		SetTelegramMessageID(msgID).
		Save(ctx)
	return err
}

// formatMessage produces a richer MarkdownV2 message for a task event.
func formatMessage(e notify.NotifyEvent) string {
	icon := statusIcon(e.NewStatus)
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("%s *Task \\#%d* — *%s*\n", icon, e.TaskID, escapeMarkdown(e.NewStatus)))
	if e.OldStatus != "" && e.OldStatus != e.NewStatus {
		sb.WriteString(fmt.Sprintf("_was %s_\n", escapeMarkdown(e.OldStatus)))
	}

	if e.ProjectName != "" {
		sb.WriteString(fmt.Sprintf("📁 Project: *%s*\n", escapeMarkdown(e.ProjectName)))
	}

	if e.IssueNumber > 0 {
		issueText := fmt.Sprintf("Issue \\#%d", e.IssueNumber)
		if e.IssueTitle != "" {
			issueText += " " + escapeMarkdown(e.IssueTitle)
		}
		if url := e.IssueURL(); url != "" {
			sb.WriteString(fmt.Sprintf("📝 [%s](%s)\n", issueText, url))
		} else {
			sb.WriteString("📝 " + issueText + "\n")
		}
	}

	if e.PRNumber > 0 {
		if url := e.PRURL(); url != "" {
			sb.WriteString(fmt.Sprintf("🔀 [PR \\#%d](%s)\n", e.PRNumber, url))
		} else {
			sb.WriteString(fmt.Sprintf("🔀 PR \\#%d\n", e.PRNumber))
		}
	}

	if e.BranchName != "" {
		sb.WriteString(fmt.Sprintf("🌿 `%s`\n", escapeMarkdown(e.BranchName)))
	}

	if e.TaskType != "" {
		sb.WriteString(fmt.Sprintf("⚙️ Type: %s\n", escapeMarkdown(e.TaskType)))
	}

	if !e.CreatedAt.IsZero() {
		dur := time.Since(e.CreatedAt).Round(time.Second)
		sb.WriteString(fmt.Sprintf("⏱ Elapsed: %s\n", escapeMarkdown(dur.String())))
	}

	if e.Error != "" {
		sb.WriteString(fmt.Sprintf("\n❗ Error:\n```\n%s\n```\n", escapeMarkdownCode(truncateRunes(e.Error, maxErrorRunes))))
	}

	if url := e.TaskURL(); url != "" {
		sb.WriteString(fmt.Sprintf("\n[🔗 View Task](%s)", url))
	}

	if !e.UpdatedAt.IsZero() {
		sb.WriteString(fmt.Sprintf("\n_updated %s_", escapeMarkdown(e.UpdatedAt.UTC().Format("2006-01-02 15:04:05 UTC"))))
	}

	return sb.String()
}

// buildKeyboard returns inline buttons appropriate to the current status.
func buildKeyboard(e notify.NotifyEvent) [][]inlineButton {
	if e.TaskID == 0 {
		return nil
	}
	var row []inlineButton
	switch e.NewStatus {
	case "running":
		row = append(row,
			inlineButton{Text: "⏸ Pause", CallbackData: cbData(e.TaskID, "pause")},
			inlineButton{Text: "🚫 Cancel", CallbackData: cbData(e.TaskID, "cancel")},
		)
	case "queued", "pending":
		row = append(row,
			inlineButton{Text: "🚫 Cancel", CallbackData: cbData(e.TaskID, "cancel")},
		)
	case "paused", "waiting_user":
		row = append(row,
			inlineButton{Text: "▶️ Resume", CallbackData: cbData(e.TaskID, "resume")},
			inlineButton{Text: "🚫 Cancel", CallbackData: cbData(e.TaskID, "cancel")},
		)
	case "failed":
		row = append(row,
			inlineButton{Text: "🔁 Retry", CallbackData: cbData(e.TaskID, "retry")},
		)
	}
	if url := e.TaskURL(); url != "" {
		row = append(row, inlineButton{Text: "🔗 Open", URL: url})
	}
	if len(row) == 0 {
		return nil
	}
	return [][]inlineButton{row}
}

func cbData(taskID int, action string) string {
	return fmt.Sprintf("t:%d:%s", taskID, action)
}

// ParseCallback splits "t:<id>:<action>" callback payload.
func ParseCallback(data string) (int, string, bool) {
	parts := strings.Split(data, ":")
	if len(parts) != 3 || parts[0] != "t" {
		return 0, "", false
	}
	id, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, "", false
	}
	return id, parts[2], true
}

func statusIcon(status string) string {
	switch status {
	case "succeeded":
		return "✅"
	case "failed":
		return "❌"
	case "waiting_user":
		return "⏳"
	case "cancelled":
		return "🚫"
	case "running":
		return "🚀"
	case "queued":
		return "📋"
	case "paused":
		return "⏸"
	default:
		return "📋"
	}
}

// truncateRunes safely truncates s to at most max runes (UTF-8 safe).
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// escapeMarkdownCode escapes characters that would break a MarkdownV2 code block.
func escapeMarkdownCode(s string) string {
	return strings.NewReplacer("\\", "\\\\", "`", "\\`").Replace(s)
}

// sanitizeError redacts the bot token (and any token-like substring) from an error message.
func sanitizeError(err error, botToken string) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if botToken != "" {
		msg = strings.ReplaceAll(msg, botToken, "[REDACTED]")
	}
	return botTokenPattern.ReplaceAllString(msg, "[REDACTED]")
}

// escapeMarkdown escapes MarkdownV2 special characters.
func escapeMarkdown(s string) string {
	replacer := strings.NewReplacer(
		"_", "\\_", "*", "\\*", "[", "\\[", "]", "\\]",
		"(", "\\(", ")", "\\)", "~", "\\~", "`", "\\`",
		">", "\\>", "#", "\\#", "+", "\\+", "-", "\\-",
		"=", "\\=", "|", "\\|", "{", "\\{", "}", "\\}",
		".", "\\.", "!", "\\!",
	)
	return replacer.Replace(s)
}

type inlineButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
	URL          string `json:"url,omitempty"`
}

func chatIDValue(chatID string) interface{} {
	if n, err := strconv.ParseInt(chatID, 10, 64); err == nil {
		return n
	}
	return chatID
}

func replyMarkup(buttons [][]inlineButton) interface{} {
	if buttons == nil {
		return nil
	}
	return map[string]interface{}{"inline_keyboard": buttons}
}

func (p *Provider) sendMessage(ctx context.Context, botToken, chatID, text string, buttons [][]inlineButton) (int64, error) {
	body := map[string]interface{}{
		"chat_id":    chatIDValue(chatID),
		"text":       text,
		"parse_mode": "MarkdownV2",
	}
	if rm := replyMarkup(buttons); rm != nil {
		body["reply_markup"] = rm
	}

	var resp struct {
		OK     bool   `json:"ok"`
		Desc   string `json:"description"`
		Result struct {
			MessageID int64 `json:"message_id"`
		} `json:"result"`
	}
	if err := p.callAPI(ctx, botToken, "sendMessage", body, &resp); err != nil {
		return 0, err
	}
	if !resp.OK {
		return 0, fmt.Errorf("sendMessage failed: %s", resp.Desc)
	}
	return resp.Result.MessageID, nil
}

func (p *Provider) editMessage(ctx context.Context, botToken, chatID string, messageID int64, text string, buttons [][]inlineButton) error {
	body := map[string]interface{}{
		"chat_id":    chatIDValue(chatID),
		"message_id": messageID,
		"text":       text,
		"parse_mode": "MarkdownV2",
	}
	if rm := replyMarkup(buttons); rm != nil {
		body["reply_markup"] = rm
	} else {
		// Clear inline keyboard for terminal states.
		body["reply_markup"] = map[string]interface{}{"inline_keyboard": [][]inlineButton{}}
	}

	var resp struct {
		OK   bool   `json:"ok"`
		Desc string `json:"description"`
	}
	if err := p.callAPI(ctx, botToken, "editMessageText", body, &resp); err != nil {
		return err
	}
	if !resp.OK {
		// Telegram returns "message is not modified" when text+markup unchanged.
		if strings.Contains(resp.Desc, "not modified") {
			return nil
		}
		return fmt.Errorf("editMessageText failed: %s", resp.Desc)
	}
	return nil
}

func (p *Provider) callAPI(ctx context.Context, botToken, method string, body interface{}, out interface{}) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/%s", botToken, method)
	buf, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("telegram %s: %s", method, sanitizeError(err, botToken))
	}
	defer resp.Body.Close()
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// callback handling ---------------------------------------------------------

// CallbackAction represents a parsed inline-button click.
type CallbackAction struct {
	TaskID int
	Action string // pause | resume | cancel | retry
}

// CallbackDispatcher applies a CallbackAction to scheduler/state.
type CallbackDispatcher interface {
	HandleCallback(ctx context.Context, action CallbackAction) (string, error)
}

// RunPoller long-polls Telegram for inline-button callbacks. It exits when ctx
// is done. Safe to run as a single goroutine; the offset is persisted in
// settings so restarts skip already-handled updates.
func (p *Provider) RunPoller(ctx context.Context, dispatcher CallbackDispatcher) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		enabled := p.settingsMgr.GetWithDefault(ctx, KeyEnabled, "false")
		botToken, _ := p.settingsMgr.Get(ctx, KeyBotToken)
		if enabled != "true" || botToken == "" {
			select {
			case <-ctx.Done():
				return
			case <-time.After(15 * time.Second):
			}
			continue
		}

		offset := int64(0)
		if raw, _ := p.settingsMgr.Get(ctx, KeyLastUpdateID); raw != "" {
			if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
				offset = v + 1
			}
		}

		updates, err := p.getUpdates(ctx, botToken, offset)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}

		for _, u := range updates {
			if u.CallbackQuery != nil {
				p.handleCallback(ctx, botToken, u.CallbackQuery, dispatcher)
			}
			// Persist offset on a fresh context so a shutdown-initiated cancel
			// doesn't drop the last handled update_id.
			persistCtx, persistCancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = p.settingsMgr.Set(persistCtx, KeyLastUpdateID, strconv.FormatInt(u.UpdateID, 10))
			persistCancel()
		}
	}
}

type tgUpdate struct {
	UpdateID      int64            `json:"update_id"`
	CallbackQuery *tgCallbackQuery `json:"callback_query,omitempty"`
}

type tgCallbackQuery struct {
	ID      string `json:"id"`
	Data    string `json:"data"`
	Message *struct {
		MessageID int64 `json:"message_id"`
		Chat      struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	} `json:"message"`
	From struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	} `json:"from"`
}

func (p *Provider) getUpdates(ctx context.Context, botToken string, offset int64) ([]tgUpdate, error) {
	body := map[string]interface{}{
		"timeout":         25,
		"allowed_updates": []string{"callback_query"},
	}
	if offset > 0 {
		body["offset"] = offset
	}
	var resp struct {
		OK     bool       `json:"ok"`
		Result []tgUpdate `json:"result"`
	}
	if err := p.callAPI(ctx, botToken, "getUpdates", body, &resp); err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("getUpdates not ok")
	}
	return resp.Result, nil
}

func (p *Provider) handleCallback(ctx context.Context, botToken string, q *tgCallbackQuery, dispatcher CallbackDispatcher) {
	taskID, action, ok := ParseCallback(q.Data)
	if !ok {
		p.answerCallback(ctx, botToken, q.ID, "invalid action", false)
		return
	}

	// Restrict to the configured chat ID to avoid hostile invocations.
	configuredChat, _ := p.settingsMgr.Get(ctx, KeyChatID)
	if configuredChat == "" || q.Message == nil ||
		strconv.FormatInt(q.Message.Chat.ID, 10) != configuredChat {
		p.answerCallback(ctx, botToken, q.ID, "unauthorized chat", true)
		return
	}

	msg := "ok"
	if dispatcher != nil {
		if reply, err := dispatcher.HandleCallback(ctx, CallbackAction{TaskID: taskID, Action: action}); err != nil {
			msg = "error: " + err.Error()
		} else if reply != "" {
			msg = reply
		}
	}
	p.answerCallback(ctx, botToken, q.ID, msg, false)

	// Refresh the message to reflect the new status.
	if p.client != nil {
		if t, err := p.client.Task.Query().Where(enttask.ID(taskID)).WithProject().Only(ctx); err == nil {
			ev := notify.NotifyEvent{
				TaskID:      taskID,
				IssueNumber: t.IssueNumber,
				NewStatus:   t.Status.String(),
				TaskType:    t.Type.String(),
				CreatedAt:   t.CreatedAt,
				UpdatedAt:   t.UpdatedAt,
				BranchName:  model.TaskBranchName(t.IssueNumber, taskID),
				BaseURL:     p.settingsMgr.GetWithDefault(ctx, "notify_base_url", ""),
			}
			if t.PrNumber != nil {
				ev.PRNumber = *t.PrNumber
			}
			if t.Edges.Project != nil {
				ev.ProjectName = t.Edges.Project.Name
				ev.RepoURL = t.Edges.Project.RepoURL
			}
			_ = p.Send(ctx, ev)
		}
	}
}

func (p *Provider) answerCallback(ctx context.Context, botToken, callbackID, text string, alert bool) {
	body := map[string]interface{}{
		"callback_query_id": callbackID,
		"text":              text,
		"show_alert":        alert,
	}
	_ = p.callAPI(ctx, botToken, "answerCallbackQuery", body, nil)
}
