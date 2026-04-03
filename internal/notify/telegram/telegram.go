package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cloverstd/ccmate/internal/notify"
	"github.com/cloverstd/ccmate/internal/settings"
)

const (
	KeyEnabled  = "notify_telegram_enabled"
	KeyBotToken = "notify_telegram_bot_token"
	KeyChatID   = "notify_telegram_chat_id"
)

// Provider sends notifications via Telegram Bot API.
type Provider struct {
	settingsMgr *settings.Manager
	httpClient  *http.Client
}

func New(settingsMgr *settings.Manager) *Provider {
	return &Provider{
		settingsMgr: settingsMgr,
		httpClient:  &http.Client{Timeout: 15 * time.Second},
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
	return p.sendMessage(ctx, botToken, chatID, text)
}

func formatMessage(e notify.NotifyEvent) string {
	icon := statusIcon(e.NewStatus)
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("%s *Task \\#%d* — %s\n", icon, e.TaskID, escapeMarkdown(e.NewStatus)))

	if e.ProjectName != "" {
		sb.WriteString(fmt.Sprintf("Project: %s\n", escapeMarkdown(e.ProjectName)))
	}

	if e.IssueNumber > 0 {
		issueText := fmt.Sprintf("Issue \\#%d", e.IssueNumber)
		if e.IssueTitle != "" {
			issueText += " " + escapeMarkdown(e.IssueTitle)
		}
		if url := e.IssueURL(); url != "" {
			sb.WriteString(fmt.Sprintf("[%s](%s)\n", issueText, url))
		} else {
			sb.WriteString(issueText + "\n")
		}
	}

	if e.Error != "" {
		sb.WriteString(fmt.Sprintf("Error: `%s`\n", escapeMarkdown(e.Error)))
	}

	if url := e.TaskURL(); url != "" {
		sb.WriteString(fmt.Sprintf("\n[View Task](%s)", url))
	}

	return sb.String()
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

func (p *Provider) sendMessage(ctx context.Context, botToken, chatID, text string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)

	// chat_id must be a number for numeric IDs, string for @channel usernames
	var chatIDValue interface{} = chatID
	if n, err := strconv.ParseInt(chatID, 10, 64); err == nil {
		chatIDValue = n
	}

	body, _ := json.Marshal(map[string]interface{}{
		"chat_id":    chatIDValue,
		"text":       text,
		"parse_mode": "MarkdownV2",
	})

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending telegram message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		return fmt.Errorf("telegram API error %d: %v", resp.StatusCode, result["description"])
	}

	return nil
}
