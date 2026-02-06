package commands

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/shugen002/RepoCaptchaBot/utils"
)

func HandleSetTTL(ctx context.Context, b *bot.Bot, env *Context, msg *models.Message, ttlText string) {
	targetChatID, replyChatID, i18n, ok := resolveTargetChat(ctx, b, env, msg)
	if !ok {
		return
	}
	chatCfg, ok := getChatConfigForUpdate(ctx, b, env, targetChatID, msg.From, replyChatID)
	if !ok {
		return
	}
	if msg.Chat.Type != models.ChatTypePrivate {
		i18n = env.I18nForChat(chatCfg)
	}
	if strings.TrimSpace(ttlText) == "" {
		env.SendReply(ctx, b, replyChatID, utils.FormatMarkdown(i18n, "group.setttl.usage", nil))
		return
	}
	ttl, err := time.ParseDuration(ttlText)
	if err != nil || ttl <= 0 {
		env.SendReply(ctx, b, replyChatID, utils.FormatMarkdown(i18n, "group.setttl.invalid", nil))
		return
	}
	chatCfg.QuestionTTL = ttl
	chatCfg.UpdatedAt = time.Now()
	if err := env.Store.UpsertChatConfig(ctx, chatCfg); err != nil {
		env.SendReply(ctx, b, replyChatID, utils.FormatMarkdown(i18n, "group.save_failed", nil))
		return
	}
	_ = env.Store.InsertAudit(ctx, "set_ttl", "admin:"+strconv.FormatInt(msg.From.ID, 10), ttl.String())
	env.SendReply(ctx, b, replyChatID, utils.FormatMarkdown(i18n, "group.setttl.success", map[string]string{"ttl": utils.FormatCode(utils.FormatDuration(ttl))}))
}
