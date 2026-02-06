package commands

import (
	"context"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	appmodels "github.com/shugen002/RepoCaptchaBot/models"
	"github.com/shugen002/RepoCaptchaBot/utils"
)

func HandleDisconnect(ctx context.Context, b *bot.Bot, env *Context, msg *models.Message) {
	i18n := env.I18nForUser(msg.From, appmodels.ChatConfig{DefaultLang: env.DefaultLang})
	if _, ok := env.GetConnectedChat(msg.From.ID); !ok {
		env.SendReply(ctx, b, msg.Chat.ID, utils.FormatMarkdown(i18n, "private.disconnect.none", nil))
		return
	}
	env.ClearConnectedChat(msg.From.ID)
	env.SendReply(ctx, b, msg.Chat.ID, utils.FormatMarkdown(i18n, "private.disconnect.success", nil))
}
