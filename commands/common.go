package commands

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	appmodels "github.com/shugen002/RepoCaptchaBot/models"
	"github.com/shugen002/RepoCaptchaBot/utils"
)

func resolveTargetChat(ctx context.Context, b *bot.Bot, env *Context, msg *models.Message) (int64, int64, *utils.I18n, bool) {
	replyChatID := msg.Chat.ID
	if msg.Chat.Type == models.ChatTypePrivate {
		i18n := env.I18nForUser(msg.From, appmodels.ChatConfig{DefaultLang: env.DefaultLang})
		chatID, ok := env.GetConnectedChat(msg.From.ID)
		if !ok {
			env.SendReply(ctx, b, replyChatID, utils.FormatMarkdown(i18n, "private.need_connect", nil))
			return 0, replyChatID, i18n, false
		}
		return chatID, replyChatID, i18n, true
	}

	chatCfg, err := env.Store.GetChatConfig(ctx, msg.Chat.ID)
	if err == nil {
		chatCfg = env.ApplyChatDefaults(chatCfg)
		return msg.Chat.ID, replyChatID, env.I18nForChat(chatCfg), true
	}
	return msg.Chat.ID, replyChatID, env.I18nForChat(appmodels.ChatConfig{DefaultLang: env.DefaultLang}), true
}

func getChatConfigForUpdate(ctx context.Context, b *bot.Bot, env *Context, targetChatID int64, actor *models.User, replyChatID int64) (appmodels.ChatConfig, bool) {
	if actor == nil || !env.IsGroupAdmin(ctx, b, targetChatID, actor.ID) {
		env.SendReply(ctx, b, replyChatID, utils.FormatMarkdown(env.DefaultI18n, "group.setrepo.admin_only", nil))
		return appmodels.ChatConfig{}, false
	}
	chatCfg, err := env.Store.GetChatConfig(ctx, targetChatID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			env.SendReply(ctx, b, replyChatID, utils.FormatMarkdown(env.DefaultI18n, "group.config_missing", nil))
			return appmodels.ChatConfig{}, false
		}
		env.SendReply(ctx, b, replyChatID, utils.FormatMarkdown(env.DefaultI18n, "group.save_failed", nil))
		return appmodels.ChatConfig{}, false
	}
	return env.ApplyChatDefaults(chatCfg), true
}

func buildSettingsText(i18n *utils.I18n, chatCfg appmodels.ChatConfig) string {
	path := chatCfg.FilePath
	line := "-"
	if path == "" || chatCfg.FileLine <= 0 {
		path = "-"
	} else {
		line = strconv.Itoa(chatCfg.FileLine)
	}
	return utils.FormatMarkdown(i18n, "group.settings", map[string]string{
		"repo":  utils.FormatRepoLink(chatCfg.Repo),
		"path":  utils.FormatCode(path),
		"line":  utils.FormatCode(line),
		"ttl":   utils.FormatCode(utils.FormatDuration(chatCfg.QuestionTTL)),
		"tries": utils.FormatCode(strconv.Itoa(chatCfg.MaxAttempts)),
		"lang":  utils.FormatCode(chatCfg.DefaultLang),
	})
}
