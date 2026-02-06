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

func HandleSetTries(ctx context.Context, b *bot.Bot, env *Context, msg *models.Message, triesText string) {
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
	triesText = strings.TrimSpace(triesText)
	if triesText == "" {
		env.SendReply(ctx, b, replyChatID, utils.FormatMarkdown(i18n, "group.settries.usage", nil))
		return
	}
	tries, err := strconv.Atoi(triesText)
	if err != nil || tries <= 0 {
		env.SendReply(ctx, b, replyChatID, utils.FormatMarkdown(i18n, "group.settries.invalid", nil))
		return
	}
	chatCfg.MaxAttempts = tries
	chatCfg.UpdatedAt = time.Now()
	if err := env.Store.UpsertChatConfig(ctx, chatCfg); err != nil {
		env.SendReply(ctx, b, replyChatID, utils.FormatMarkdown(i18n, "group.save_failed", nil))
		return
	}
	_ = env.Store.InsertAudit(ctx, "set_tries", "admin:"+strconv.FormatInt(msg.From.ID, 10), strconv.Itoa(tries))
	env.SendReply(ctx, b, replyChatID, utils.FormatMarkdown(i18n, "group.settries.success", map[string]string{"tries": utils.FormatCode(strconv.Itoa(tries))}))
}
