package commands

import (
	"context"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	appmodels "github.com/shugen002/RepoCaptchaBot/models"
	"github.com/shugen002/RepoCaptchaBot/utils"
	"github.com/shugen002/RepoCaptchaBot/verifier"
)

type Context struct {
	Store    *appmodels.Store
	Verifier *verifier.Verifier

	DefaultI18n       *utils.I18n
	DefaultLang       string
	DefaultQuestionTTL time.Duration
	DefaultMaxAttempts int
	DefaultFilePath    string
	DefaultFileLine    int

	I18nForUser func(user *models.User, chatCfg appmodels.ChatConfig) *utils.I18n
	I18nForChat func(chatCfg appmodels.ChatConfig) *utils.I18n

	ApplyChatDefaults func(cfg appmodels.ChatConfig) appmodels.ChatConfig
	GetConnectedChat  func(userID int64) (int64, bool)
	SetConnectedChat  func(userID, chatID int64)
	ClearConnectedChat func(userID int64)

	IsGroupAdmin func(ctx context.Context, b *bot.Bot, chatID, userID int64) bool
	SendReply    func(ctx context.Context, b *bot.Bot, chatID int64, text string)
	ClearWarn    func(chatID int64)
}
