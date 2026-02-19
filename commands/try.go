package commands

import (
	"context"
	"errors"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/shugen002/RepoCaptchaBot/utils"
	"github.com/shugen002/RepoCaptchaBot/verifier"
)

func HandleTry(ctx context.Context, b *bot.Bot, env *Context, msg *models.Message, qType string) {
	targetChatID, replyChatID, i18n, ok := resolveTargetChat(ctx, b, env, msg)
	if !ok {
		return
	}
	chatCfg, ok := getChatConfigForUpdate(ctx, b, env, targetChatID, msg.From, replyChatID)
	if !ok {
		return
	}
	chatCfg = env.ApplyChatDefaults(chatCfg)

	qType = strings.TrimSpace(qType)
	if qType == "" {
		types, err := env.Verifier.AvailableQuestionTypes(ctx, chatCfg, i18n)
		if err != nil {
			env.SendReply(ctx, b, replyChatID, utils.FormatMarkdown(i18n, "group.save_failed", nil))
			return
		}
		if len(types) == 0 {
			env.SendReply(ctx, b, replyChatID, utils.FormatMarkdown(i18n, "private.try.types_empty", nil))
			return
		}
		env.SendReply(ctx, b, replyChatID, utils.FormatMarkdown(i18n, "private.try.types", map[string]string{"types": utils.FormatTypeList(types)}))
		return
	}

	q, ok, err := env.Verifier.GenerateQuestionByType(ctx, chatCfg, i18n, qType)
	if err != nil {
		if errors.Is(err, verifier.ErrUnknownQuestionType) {
			types := env.Verifier.SupportedQuestionTypes()
			env.SendReply(ctx, b, replyChatID, utils.FormatMarkdown(i18n, "private.try.invalid", map[string]string{"type": utils.FormatCode(qType), "types": utils.FormatTypeList(types)}))
			return
		}
		env.SendReply(ctx, b, replyChatID, utils.FormatMarkdown(i18n, "group.save_failed", nil))
		return
	}
	if !ok {
		env.SendReply(ctx, b, replyChatID, utils.FormatMarkdown(i18n, "private.try.unavailable", map[string]string{"type": utils.FormatCode(qType)}))
		return
	}
	answer := verifier.ExtractAnswer(q)
	text := utils.FormatMarkdown(i18n, "private.try.question", map[string]string{"question": q.Question, "answer": utils.FormatCode(answer)})
	env.SendReply(ctx, b, replyChatID, text)
}
