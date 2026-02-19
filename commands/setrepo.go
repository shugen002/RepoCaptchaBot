package commands

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	appmodels "github.com/shugen002/RepoCaptchaBot/models"
	"github.com/shugen002/RepoCaptchaBot/utils"
)

func HandleSetRepo(ctx context.Context, b *bot.Bot, env *Context, msg *models.Message, repo string) {
	if msg == nil || msg.From == nil {
		return
	}
	targetChatID, replyChatID, i18n, ok := resolveTargetChat(ctx, b, env, msg)
	if !ok {
		return
	}
	repo = strings.TrimSpace(repo)
	if repo == "" {
		env.SendReply(ctx, b, replyChatID, utils.FormatMarkdown(i18n, "group.setrepo.usage", nil))
		return
	}
	if !isValidRepo(repo) {
		env.SendReply(ctx, b, replyChatID, utils.FormatMarkdown(i18n, "group.setrepo.invalid", nil))
		return
	}
	if !env.IsGroupAdmin(ctx, b, targetChatID, msg.From.ID) {
		env.SendReply(ctx, b, replyChatID, utils.FormatMarkdown(i18n, "group.setrepo.admin_only", nil))
		return
	}
	cfg := appmodels.ChatConfig{ChatID: targetChatID, Repo: repo, UpdatedAt: time.Now()}
	existing, err := env.Store.GetChatConfig(ctx, targetChatID)
	if err == nil {
		cfg.FilePath = existing.FilePath
		cfg.FileLine = existing.FileLine
		cfg.QuestionTTL = existing.QuestionTTL
		cfg.MaxAttempts = existing.MaxAttempts
		cfg.DefaultLang = existing.DefaultLang
	} else if errors.Is(err, sql.ErrNoRows) {
		cfg.FilePath = env.DefaultFilePath
		cfg.FileLine = env.DefaultFileLine
		cfg.QuestionTTL = env.DefaultQuestionTTL
		cfg.MaxAttempts = env.DefaultMaxAttempts
		cfg.DefaultLang = env.DefaultLang
	} else {
		env.SendReply(ctx, b, replyChatID, utils.FormatMarkdown(i18n, "group.save_failed", nil))
		return
	}
	if err := env.Store.UpsertChatConfig(ctx, cfg); err != nil {
		env.SendReply(ctx, b, replyChatID, utils.FormatMarkdown(i18n, "group.save_failed", nil))
		return
	}
	env.ClearWarn(targetChatID)
	actorName := "admin:" + strconv.FormatInt(msg.From.ID, 10)
	_ = env.Store.InsertAudit(ctx, "set_repo", actorName, repo)
	env.SendReply(ctx, b, replyChatID, utils.FormatMarkdown(i18n, "group.setrepo.success", map[string]string{"repo": utils.FormatRepoLink(repo)}))
}

func isValidRepo(repo string) bool {
	parts := strings.Split(strings.TrimSpace(repo), "/")
	if len(parts) != 2 {
		return false
	}
	return strings.TrimSpace(parts[0]) != "" && strings.TrimSpace(parts[1]) != ""
}
