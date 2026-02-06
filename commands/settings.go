package commands

import (
	"context"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func HandleSettings(ctx context.Context, b *bot.Bot, env *Context, msg *models.Message) {
	targetChatID, replyChatID, i18n, ok := resolveTargetChat(ctx, b, env, msg)
	if !ok {
		return
	}
	chatCfg, ok := getChatConfigForUpdate(ctx, b, env, targetChatID, msg.From, replyChatID)
	if !ok {
		return
	}
	chatCfg = env.ApplyChatDefaults(chatCfg)
	if msg.Chat.Type != models.ChatTypePrivate {
		i18n = env.I18nForChat(chatCfg)
	}
	env.SendReply(ctx, b, replyChatID, buildSettingsText(i18n, chatCfg))
}
