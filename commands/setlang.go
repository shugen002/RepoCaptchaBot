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

func HandleSetLang(ctx context.Context, b *bot.Bot, env *Context, msg *models.Message, lang string) {
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
	lang = utils.NormalizeLang(lang)
	if strings.TrimSpace(lang) == "" {
		env.SendReply(ctx, b, replyChatID, utils.FormatMarkdown(i18n, "group.setlang.usage", nil))
		return
	}
	if !utils.IsLangAvailable(lang) {
		env.SendReply(ctx, b, replyChatID, utils.FormatMarkdown(i18n, "group.setlang.invalid", map[string]string{"lang": utils.FormatCode(lang)}))
		return
	}
	chatCfg.DefaultLang = lang
	chatCfg.UpdatedAt = time.Now()
	if err := env.Store.UpsertChatConfig(ctx, chatCfg); err != nil {
		env.SendReply(ctx, b, replyChatID, utils.FormatMarkdown(i18n, "group.save_failed", nil))
		return
	}
	_ = env.Store.InsertAudit(ctx, "set_lang", "admin:"+strconv.FormatInt(msg.From.ID, 10), lang)
	env.SendReply(ctx, b, replyChatID, utils.FormatMarkdown(i18n, "group.setlang.success", map[string]string{"lang": utils.FormatCode(lang)}))
}
