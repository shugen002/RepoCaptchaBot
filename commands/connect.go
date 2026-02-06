package commands

import (
	"context"
	"strconv"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	appmodels "github.com/shugen002/RepoCaptchaBot/models"
	"github.com/shugen002/RepoCaptchaBot/utils"
)

func HandleConnect(ctx context.Context, b *bot.Bot, env *Context, msg *models.Message, chatIDText string) {
	if msg.Chat.Type != models.ChatTypePrivate {
		if !env.IsGroupAdmin(ctx, b, msg.Chat.ID, msg.From.ID) {
			env.SendReply(ctx, b, msg.Chat.ID, utils.FormatMarkdown(env.DefaultI18n, "group.setrepo.admin_only", nil))
			return
		}
		text := utils.FormatMarkdown(env.DefaultI18n, "group.connect.hint", map[string]string{"chat_id": utils.FormatCode(strconv.FormatInt(msg.Chat.ID, 10))})
		env.SendReply(ctx, b, msg.Chat.ID, text)
		return
	}

	i18n := env.I18nForUser(msg.From, appmodels.ChatConfig{DefaultLang: env.DefaultLang})
	chatIDText = strings.TrimSpace(chatIDText)
	if chatIDText == "" {
		env.SendReply(ctx, b, msg.Chat.ID, utils.FormatMarkdown(i18n, "private.connect.usage", nil))
		return
	}
	chatID, err := strconv.ParseInt(chatIDText, 10, 64)
	if err != nil || chatID == 0 {
		env.SendReply(ctx, b, msg.Chat.ID, utils.FormatMarkdown(i18n, "private.connect.usage", nil))
		return
	}
	if !env.IsGroupAdmin(ctx, b, chatID, msg.From.ID) {
		env.SendReply(ctx, b, msg.Chat.ID, utils.FormatMarkdown(i18n, "private.connect.not_admin", nil))
		return
	}
	env.SetConnectedChat(msg.From.ID, chatID)
	env.SendReply(ctx, b, msg.Chat.ID, utils.FormatMarkdown(i18n, "private.connect.success", map[string]string{"chat_id": utils.FormatCode(chatIDText)}))
}
