package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type BotHandler struct {
	cfg      Config
	store    *Store
	verifier *Verifier
	i18n     *I18n
	warnMu   sync.Mutex
	lastWarn map[int64]time.Time
	botMu    sync.Mutex
	botName  string
	connMu   sync.RWMutex
	connMap  map[int64]int64
	i18nMu   sync.RWMutex
	i18nMap  map[string]*I18n
}

func NewBotHandler(cfg Config, store *Store, verifier *Verifier, i18n *I18n) *BotHandler {
	return &BotHandler{
		cfg:      cfg,
		store:    store,
		verifier: verifier,
		i18n:     i18n,
		lastWarn: make(map[int64]time.Time),
		connMap:  make(map[int64]int64),
		i18nMap:  make(map[string]*I18n),
	}
}

func (h *BotHandler) HandleUpdate(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update == nil {
		return
	}
	if update.MyChatMember != nil {
		h.handleMyChatMember(ctx, b, update.MyChatMember)
		return
	}
	if update.ChatJoinRequest != nil {
		h.handleJoinRequest(ctx, b, update.ChatJoinRequest)
		return
	}
	if update.ChatMember != nil {
		h.handleChatMemberUpdated(ctx, b, update.ChatMember)
		return
	}
	if update.Message != nil {
		h.handleMessage(ctx, b, update.Message)
		return
	}
}

func (h *BotHandler) handleJoinRequest(ctx context.Context, b *bot.Bot, req *models.ChatJoinRequest) {
	if req == nil {
		return
	}
	chatID := req.Chat.ID
	if chatID == 0 {
		return
	}
	chatCfg, ok := h.ensureChatConfig(ctx, b, chatID)
	if !ok {
		return
	}
	chatCfg = h.applyChatDefaults(chatCfg)
	userI18n := h.i18nForUser(&req.From, chatCfg)

	q, err := h.verifier.GenerateQuestion(ctx, chatCfg, userI18n)
	if err != nil {
		slog.Error("生成题目失败", "error", err)
		return
	}

	qid, err := h.store.InsertQuestion(ctx, StoredQuestion{
		Repo:      chatCfg.Repo,
		Type:      q.Type,
		Prompt:    q.Prompt,
		Payload:   q.Payload,
		Answer:    q.Answer,
		CreatedAt: time.Now(),
	})
	if err != nil {
		slog.Error("保存题目失败", "error", err)
		return
	}

	expiresAt := time.Now().Add(chatCfg.QuestionTTL)
	if err := h.store.UpsertPending(ctx, PendingMember{
		TelegramID:   req.From.ID,
		ChatID:       req.Chat.ID,
		QuestionID:   qid,
		ExpiresAt:    expiresAt,
		AttemptsLeft: chatCfg.MaxAttempts,
	}); err != nil {
		slog.Error("保存待验证用户失败", "error", err)
		return
	}
	slog.Info("触发入群验证", "trigger", "join_request", "chat_id", chatID, "chat_title", req.Chat.Title, "user_id", req.From.ID, "username", req.From.Username, "question_id", qid, "question_type", q.Type, "question_prompt", q.Prompt)

	text := formatMarkdown(userI18n, "verify.prompt", map[string]string{
		"ttl":      formatCode(formatDuration(chatCfg.QuestionTTL)),
		"question": q.Prompt,
		"repo":     formatRepoLink(chatCfg.Repo),
	})

	_, err = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    req.From.ID,
		Text:      text,
		ParseMode: "MarkdownV2",
	})
	if err != nil {
		slog.Warn("私聊发送失败，可能需要用户先点击 Start", "error", err)
		_ = h.store.InsertAudit(ctx, "send_failed", "bot", err.Error())
	}
}

func (h *BotHandler) handleChatMemberUpdated(ctx context.Context, b *bot.Bot, upd *models.ChatMemberUpdated) {
	if upd == nil || upd.ViaJoinRequest {
		return
	}
	if !isNewMember(upd.OldChatMember, upd.NewChatMember) {
		return
	}
	chatID := upd.Chat.ID
	if chatID == 0 {
		return
	}

	userID, ok := extractUserID(upd.NewChatMember)
	if !ok {
		return
	}
	username := ""
	if user := chatMemberUser(upd.NewChatMember); user != nil {
		username = user.Username
	}

	chatCfg, hasCfg := h.ensureChatConfig(ctx, b, chatID)
	if !hasCfg {
		return
	}
	chatCfg = h.applyChatDefaults(chatCfg)
	userI18n := h.i18nForUser(chatMemberUser(upd.NewChatMember), chatCfg)

	q, err := h.verifier.GenerateQuestion(ctx, chatCfg, userI18n)
	if err != nil {
		slog.Error("生成题目失败", "error", err)
		return
	}

	qid, err := h.store.InsertQuestion(ctx, StoredQuestion{
		Repo:      chatCfg.Repo,
		Type:      q.Type,
		Prompt:    q.Prompt,
		Payload:   q.Payload,
		Answer:    q.Answer,
		CreatedAt: time.Now(),
	})
	if err != nil {
		slog.Error("保存题目失败", "error", err)
		return
	}

	expiresAt := time.Now().Add(chatCfg.QuestionTTL)
	if err := h.store.UpsertPending(ctx, PendingMember{
		TelegramID:   userID,
		ChatID:       upd.Chat.ID,
		QuestionID:   qid,
		ExpiresAt:    expiresAt,
		AttemptsLeft: chatCfg.MaxAttempts,
	}); err != nil {
		slog.Error("保存待验证用户失败", "error", err)
		return
	}
	slog.Info("触发入群验证", "trigger", "group_join", "chat_id", chatID, "chat_title", upd.Chat.Title, "user_id", userID, "username", username, "question_id", qid, "question_type", q.Type, "question_prompt", q.Prompt)

	text := formatMarkdown(userI18n, "verify.prompt", map[string]string{
		"ttl":      formatCode(formatDuration(chatCfg.QuestionTTL)),
		"question": q.Prompt,
		"repo":     formatRepoLink(chatCfg.Repo),
	})
	_, err = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    userID,
		Text:      text,
		ParseMode: "MarkdownV2",
	})
	if err != nil {
		slog.Warn("私聊发送失败，可能需要用户先点击 Start", "error", err)
		_ = h.store.InsertAudit(ctx, "send_failed", "bot", err.Error())
	}
}

func (h *BotHandler) handleMessage(ctx context.Context, b *bot.Bot, msg *models.Message) {
	if msg == nil || msg.From == nil {
		return
	}
	if msg.Text == "" {
		return
	}

	switch msg.Chat.Type {
	case models.ChatTypePrivate:
		h.handlePrivateMessage(ctx, b, msg)
	case models.ChatTypeGroup, models.ChatTypeSupergroup:
		h.handleGroupMessage(ctx, b, msg)
	}
}

func (h *BotHandler) handlePrivateMessage(ctx context.Context, b *bot.Bot, msg *models.Message) {
	if strings.HasPrefix(strings.TrimSpace(msg.Text), "/") {
		if h.handlePrivateCommand(ctx, b, msg) {
			return
		}
	}
	pending, err := h.store.GetPendingByTelegram(ctx, msg.From.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		return
	}

	question, err := h.store.GetQuestion(ctx, pending.QuestionID)
	if err != nil {
		slog.Error("读取题目失败", "error", err, "question_id", pending.QuestionID)
		return
	}
	chatCfg, cfgErr := h.store.GetChatConfig(ctx, pending.ChatID)
	if cfgErr == nil {
		chatCfg = h.applyChatDefaults(chatCfg)
	} else {
		chatCfg = h.applyChatDefaults(ChatConfig{ChatID: pending.ChatID, Repo: question.Repo})
	}
	userI18n := h.i18nForUser(msg.From, chatCfg)
	expired := time.Now().After(pending.ExpiresAt)
	username := msg.From.Username

	if strings.HasPrefix(msg.Text, "/start") {
		if expired {
			slog.Info("验证超时", "chat_id", pending.ChatID, "user_id", pending.TelegramID, "username", username, "question_id", question.ID, "question_type", question.Type, "question_prompt", question.Prompt)
			_ = h.reject(ctx, b, pending, formatMarkdown(userI18n, "verify.timeout", nil))
			return
		}
		slog.Info("重新发送验证题目", "chat_id", pending.ChatID, "user_id", pending.TelegramID, "username", username, "question_id", question.ID, "question_type", question.Type, "question_prompt", question.Prompt)
		remaining := time.Until(pending.ExpiresAt)
		if remaining < 0 {
			remaining = 0
		}
		h.sendQuestionAgain(ctx, b, msg.Chat.ID, pending.QuestionID, remaining, userI18n)
		return
	}

	if expired {
		slog.Info("验证超时", "chat_id", pending.ChatID, "user_id", pending.TelegramID, "username", username, "question_id", question.ID, "question_type", question.Type, "question_prompt", question.Prompt)
		_ = h.reject(ctx, b, pending, formatMarkdown(userI18n, "verify.timeout", nil))
		return
	}

	provided := strings.TrimSpace(msg.Text)

	if normalizeAnswer(msg.Text) == normalizeAnswer(question.Answer) {
		slog.Info("验证通过", "chat_id", pending.ChatID, "user_id", pending.TelegramID, "username", username, "question_id", question.ID, "question_type", question.Type, "question_prompt", question.Prompt)
		if err := h.approve(ctx, b, pending); err != nil {
			slog.Warn("审批失败", "error", err)
		}
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    msg.Chat.ID,
			Text:      formatMarkdown(userI18n, "verify.success", nil),
			ParseMode: "MarkdownV2",
		})
		return
	}

	if pending.AttemptsLeft <= 0 {
		pending.AttemptsLeft = chatCfg.MaxAttempts
	}
	attemptsLeft := pending.AttemptsLeft - 1
	if attemptsLeft <= 0 {
		slog.Info("验证失败", "chat_id", pending.ChatID, "user_id", pending.TelegramID, "username", username, "question_id", question.ID, "question_type", question.Type, "question_prompt", question.Prompt, "provided_answer", provided)
		_ = h.reject(ctx, b, pending, formatMarkdown(userI18n, "verify.wrong_answer", nil))
		return
	}
	_ = h.store.UpdatePendingAttempts(ctx, pending.TelegramID, pending.ChatID, attemptsLeft)
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    msg.Chat.ID,
		Text:      formatMarkdown(userI18n, "verify.wrong_answer_retry", map[string]string{"tries": formatCode(strconv.Itoa(attemptsLeft))}),
		ParseMode: "MarkdownV2",
	})
}

func (h *BotHandler) handleGroupMessage(ctx context.Context, b *bot.Bot, msg *models.Message) {
	chatID := msg.Chat.ID
	if chatID == 0 {
		return
	}
	text := strings.TrimSpace(msg.Text)
	if text == "" || !strings.HasPrefix(text, "/") {
		return
	}
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return
	}
	commandToken := strings.TrimPrefix(fields[0], "/")
	parts := strings.SplitN(commandToken, "@", 2)
	cmd := parts[0]
	if cmd == "" {
		return
	}
	if len(parts) == 2 {
		if !h.matchBotMention(ctx, b, parts[1]) {
			return
		}
	}
	arg := ""
	if len(fields) > 1 {
		arg = fields[1]
	}
	switch strings.ToLower(cmd) {
	case "setrepo":
		h.commandSetRepo(ctx, b, msg, arg)
	case "setttl":
		h.commandSetTTL(ctx, b, msg, arg)
	case "settries":
		h.commandSetTries(ctx, b, msg, arg)
	case "setlang":
		h.commandSetLang(ctx, b, msg, arg)
	case "settings":
		h.commandSettings(ctx, b, msg)
	case "connect":
		h.commandConnectGroup(ctx, b, msg)
	}
}

func (h *BotHandler) handlePrivateCommand(ctx context.Context, b *bot.Bot, msg *models.Message) bool {
	text := strings.TrimSpace(msg.Text)
	if text == "" || !strings.HasPrefix(text, "/") {
		return false
	}
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return false
	}
	commandToken := strings.TrimPrefix(fields[0], "/")
	parts := strings.SplitN(commandToken, "@", 2)
	cmd := strings.ToLower(parts[0])
	if cmd == "" || cmd == "start" {
		return false
	}
	if len(parts) == 2 {
		if !h.matchBotMention(ctx, b, parts[1]) {
			return false
		}
	}
	arg := ""
	if len(fields) > 1 {
		arg = fields[1]
	}
	switch cmd {
	case "connect":
		h.commandConnectPrivate(ctx, b, msg, arg)
		return true
	case "disconnect":
		h.commandDisconnectPrivate(ctx, b, msg)
		return true
	case "settings":
		h.commandSettingsPrivate(ctx, b, msg)
		return true
	case "setrepo":
		h.commandSetRepoPrivate(ctx, b, msg, arg)
		return true
	case "setttl":
		h.commandSetTTLPrivate(ctx, b, msg, arg)
		return true
	case "settries":
		h.commandSetTriesPrivate(ctx, b, msg, arg)
		return true
	case "setlang":
		h.commandSetLangPrivate(ctx, b, msg, arg)
		return true
	default:
		return false
	}
}

func (h *BotHandler) i18nForUser(user *models.User, chatCfg ChatConfig) *I18n {
	lang := ""
	if user != nil {
		lang = NormalizeLang(user.LanguageCode)
	}
	if lang == "" {
		lang = NormalizeLang(chatCfg.DefaultLang)
	}
	if lang == "" {
		lang = NormalizeLang(h.cfg.Language)
	}
	return h.getI18n(lang)
}

func (h *BotHandler) i18nForChat(chatCfg ChatConfig) *I18n {
	lang := NormalizeLang(chatCfg.DefaultLang)
	if lang == "" {
		lang = NormalizeLang(h.cfg.Language)
	}
	return h.getI18n(lang)
}

func (h *BotHandler) getI18n(lang string) *I18n {
	if lang == "" {
		return h.i18n
	}
	h.i18nMu.RLock()
	if cached, ok := h.i18nMap[lang]; ok {
		h.i18nMu.RUnlock()
		return cached
	}
	h.i18nMu.RUnlock()
	loaded, err := LoadI18n(lang)
	if err != nil {
		return h.i18n
	}
	h.i18nMu.Lock()
	h.i18nMap[lang] = loaded
	h.i18nMu.Unlock()
	return loaded
}

func (h *BotHandler) applyChatDefaults(cfg ChatConfig) ChatConfig {
	if cfg.QuestionTTL == 0 {
		cfg.QuestionTTL = h.cfg.QuestionTTL
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = h.cfg.MaxAttempts
	}
	if strings.TrimSpace(cfg.DefaultLang) == "" {
		cfg.DefaultLang = h.cfg.Language
	}
	return cfg
}

func (h *BotHandler) getConnectedChat(userID int64) (int64, bool) {
	h.connMu.RLock()
	chatID, ok := h.connMap[userID]
	h.connMu.RUnlock()
	return chatID, ok
}

func (h *BotHandler) setConnectedChat(userID, chatID int64) {
	h.connMu.Lock()
	h.connMap[userID] = chatID
	h.connMu.Unlock()
}

func (h *BotHandler) clearConnectedChat(userID int64) {
	h.connMu.Lock()
	delete(h.connMap, userID)
	h.connMu.Unlock()
}

func (h *BotHandler) commandSetRepo(ctx context.Context, b *bot.Bot, msg *models.Message, repo string) {
	chatCfg, _ := h.store.GetChatConfig(ctx, msg.Chat.ID)
	chatCfg = h.applyChatDefaults(chatCfg)
	i18n := h.i18nForChat(chatCfg)
	h.commandSetRepoForChat(ctx, b, msg.From, msg.Chat.ID, repo, msg.Chat.ID, i18n)
}

func (h *BotHandler) commandSetRepoForChat(ctx context.Context, b *bot.Bot, actor *models.User, targetChatID int64, repo string, replyChatID int64, i18n *I18n) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		h.sendGroupReply(ctx, b, replyChatID, formatMarkdown(i18n, "group.setrepo.usage", nil))
		return
	}
	if _, _, err := parseRepo(repo); err != nil {
		h.sendGroupReply(ctx, b, replyChatID, formatMarkdown(i18n, "group.setrepo.invalid", nil))
		return
	}
	if actor == nil || !h.isGroupAdmin(ctx, b, targetChatID, actor.ID) {
		h.sendGroupReply(ctx, b, replyChatID, formatMarkdown(i18n, "group.setrepo.admin_only", nil))
		return
	}
	cfg := ChatConfig{ChatID: targetChatID, Repo: repo, UpdatedAt: time.Now()}
	existing, err := h.store.GetChatConfig(ctx, targetChatID)
	if err == nil {
		cfg.FilePath = existing.FilePath
		cfg.FileLine = existing.FileLine
		cfg.QuestionTTL = existing.QuestionTTL
		cfg.MaxAttempts = existing.MaxAttempts
		cfg.DefaultLang = existing.DefaultLang
	} else if errors.Is(err, sql.ErrNoRows) {
		cfg.FilePath = h.cfg.FilePath
		cfg.FileLine = h.cfg.FileLine
		cfg.QuestionTTL = h.cfg.QuestionTTL
		cfg.MaxAttempts = h.cfg.MaxAttempts
		cfg.DefaultLang = h.cfg.Language
	} else {
		slog.Error("读取群配置失败", "error", err, "chat_id", targetChatID)
		h.sendGroupReply(ctx, b, replyChatID, formatMarkdown(i18n, "group.save_failed", nil))
		return
	}
	if err := h.store.UpsertChatConfig(ctx, cfg); err != nil {
		slog.Error("写入群配置失败", "error", err, "chat_id", targetChatID)
		h.sendGroupReply(ctx, b, replyChatID, formatMarkdown(i18n, "group.save_failed", nil))
		return
	}
	h.clearWarn(targetChatID)
	actorName := "admin:" + strconv.FormatInt(actor.ID, 10)
	_ = h.store.InsertAudit(ctx, "set_repo", actorName, repo)
	h.sendGroupReply(ctx, b, replyChatID, formatMarkdown(i18n, "group.setrepo.success", map[string]string{"repo": formatRepoLink(repo)}))
}

func (h *BotHandler) commandSetTTL(ctx context.Context, b *bot.Bot, msg *models.Message, ttlText string) {
	i18n := h.i18nForChat(ChatConfig{DefaultLang: h.cfg.Language})
	chatCfg, ok := h.getChatConfigForUpdate(ctx, b, msg.Chat.ID, msg.From, msg.Chat.ID)
	if !ok {
		return
	}
	i18n = h.i18nForChat(chatCfg)
	if strings.TrimSpace(ttlText) == "" {
		h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "group.setttl.usage", nil))
		return
	}
	ttl, err := time.ParseDuration(ttlText)
	if err != nil || ttl <= 0 {
		h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "group.setttl.invalid", nil))
		return
	}
	chatCfg.QuestionTTL = ttl
	chatCfg.UpdatedAt = time.Now()
	if err := h.store.UpsertChatConfig(ctx, chatCfg); err != nil {
		slog.Error("写入群配置失败", "error", err, "chat_id", msg.Chat.ID)
		h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "group.save_failed", nil))
		return
	}
	_ = h.store.InsertAudit(ctx, "set_ttl", "admin:"+strconv.FormatInt(msg.From.ID, 10), ttl.String())
	h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "group.setttl.success", map[string]string{"ttl": formatCode(formatDuration(ttl))}))
}

func (h *BotHandler) commandSetTries(ctx context.Context, b *bot.Bot, msg *models.Message, triesText string) {
	i18n := h.i18nForChat(ChatConfig{DefaultLang: h.cfg.Language})
	chatCfg, ok := h.getChatConfigForUpdate(ctx, b, msg.Chat.ID, msg.From, msg.Chat.ID)
	if !ok {
		return
	}
	i18n = h.i18nForChat(chatCfg)
	triesText = strings.TrimSpace(triesText)
	if triesText == "" {
		h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "group.settries.usage", nil))
		return
	}
	tries, err := strconv.Atoi(triesText)
	if err != nil || tries <= 0 {
		h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "group.settries.invalid", nil))
		return
	}
	chatCfg.MaxAttempts = tries
	chatCfg.UpdatedAt = time.Now()
	if err := h.store.UpsertChatConfig(ctx, chatCfg); err != nil {
		slog.Error("写入群配置失败", "error", err, "chat_id", msg.Chat.ID)
		h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "group.save_failed", nil))
		return
	}
	_ = h.store.InsertAudit(ctx, "set_tries", "admin:"+strconv.FormatInt(msg.From.ID, 10), strconv.Itoa(tries))
	h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "group.settries.success", map[string]string{"tries": formatCode(strconv.Itoa(tries))}))
}

func (h *BotHandler) commandSetLang(ctx context.Context, b *bot.Bot, msg *models.Message, lang string) {
	i18n := h.i18nForChat(ChatConfig{DefaultLang: h.cfg.Language})
	chatCfg, ok := h.getChatConfigForUpdate(ctx, b, msg.Chat.ID, msg.From, msg.Chat.ID)
	if !ok {
		return
	}
	i18n = h.i18nForChat(chatCfg)
	lang = NormalizeLang(lang)
	if lang == "" {
		h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "group.setlang.usage", nil))
		return
	}
	if !IsLangAvailable(lang) {
		h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "group.setlang.invalid", map[string]string{"lang": formatCode(lang)}))
		return
	}
	chatCfg.DefaultLang = lang
	chatCfg.UpdatedAt = time.Now()
	if err := h.store.UpsertChatConfig(ctx, chatCfg); err != nil {
		slog.Error("写入群配置失败", "error", err, "chat_id", msg.Chat.ID)
		h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "group.save_failed", nil))
		return
	}
	_ = h.store.InsertAudit(ctx, "set_lang", "admin:"+strconv.FormatInt(msg.From.ID, 10), lang)
	h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "group.setlang.success", map[string]string{"lang": formatCode(lang)}))
}

func (h *BotHandler) commandSettings(ctx context.Context, b *bot.Bot, msg *models.Message) {
	i18n := h.i18nForChat(ChatConfig{DefaultLang: h.cfg.Language})
	chatCfg, ok := h.getChatConfigForUpdate(ctx, b, msg.Chat.ID, msg.From, msg.Chat.ID)
	if !ok {
		return
	}
	chatCfg = h.applyChatDefaults(chatCfg)
	i18n = h.i18nForChat(chatCfg)
	path := chatCfg.FilePath
	line := "-"
	if path == "" || chatCfg.FileLine <= 0 {
		path = "-"
	} else {
		line = strconv.Itoa(chatCfg.FileLine)
	}
	text := formatMarkdown(i18n, "group.settings", map[string]string{
		"repo":  formatRepoLink(chatCfg.Repo),
		"path":  formatCode(path),
		"line":  formatCode(line),
		"ttl":   formatCode(formatDuration(chatCfg.QuestionTTL)),
		"tries": formatCode(strconv.Itoa(chatCfg.MaxAttempts)),
		"lang":  formatCode(chatCfg.DefaultLang),
	})
	h.sendGroupReply(ctx, b, msg.Chat.ID, text)
}

func (h *BotHandler) commandConnectGroup(ctx context.Context, b *bot.Bot, msg *models.Message) {
	if !h.isGroupAdmin(ctx, b, msg.Chat.ID, msg.From.ID) {
		h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(h.i18n, "group.setrepo.admin_only", nil))
		return
	}
	text := formatMarkdown(h.i18n, "group.connect.hint", map[string]string{"chat_id": formatCode(strconv.FormatInt(msg.Chat.ID, 10))})
	h.sendGroupReply(ctx, b, msg.Chat.ID, text)
}

func (h *BotHandler) commandConnectPrivate(ctx context.Context, b *bot.Bot, msg *models.Message, chatIDText string) {
	i18n := h.i18nForUser(msg.From, ChatConfig{DefaultLang: h.cfg.Language})
	chatIDText = strings.TrimSpace(chatIDText)
	if chatIDText == "" {
		h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "private.connect.usage", nil))
		return
	}
	chatID, err := strconv.ParseInt(chatIDText, 10, 64)
	if err != nil || chatID == 0 {
		h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "private.connect.usage", nil))
		return
	}
	if !h.isGroupAdmin(ctx, b, chatID, msg.From.ID) {
		h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "private.connect.not_admin", nil))
		return
	}
	h.setConnectedChat(msg.From.ID, chatID)
	h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "private.connect.success", map[string]string{"chat_id": formatCode(chatIDText)}))
}

func (h *BotHandler) commandDisconnectPrivate(ctx context.Context, b *bot.Bot, msg *models.Message) {
	i18n := h.i18nForUser(msg.From, ChatConfig{DefaultLang: h.cfg.Language})
	if _, ok := h.getConnectedChat(msg.From.ID); !ok {
		h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "private.disconnect.none", nil))
		return
	}
	h.clearConnectedChat(msg.From.ID)
	h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "private.disconnect.success", nil))
}

func (h *BotHandler) commandSettingsPrivate(ctx context.Context, b *bot.Bot, msg *models.Message) {
	chatID, ok := h.getConnectedChat(msg.From.ID)
	i18n := h.i18nForUser(msg.From, ChatConfig{DefaultLang: h.cfg.Language})
	if !ok {
		h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "private.need_connect", nil))
		return
	}
	chatCfg, ok := h.getChatConfigForUpdate(ctx, b, chatID, msg.From, msg.Chat.ID)
	if !ok {
		return
	}
	chatCfg = h.applyChatDefaults(chatCfg)
	path := chatCfg.FilePath
	line := "-"
	if path == "" || chatCfg.FileLine <= 0 {
		path = "-"
	} else {
		line = strconv.Itoa(chatCfg.FileLine)
	}
	text := formatMarkdown(i18n, "group.settings", map[string]string{
		"repo":  formatRepoLink(chatCfg.Repo),
		"path":  formatCode(path),
		"line":  formatCode(line),
		"ttl":   formatCode(formatDuration(chatCfg.QuestionTTL)),
		"tries": formatCode(strconv.Itoa(chatCfg.MaxAttempts)),
		"lang":  formatCode(chatCfg.DefaultLang),
	})
	h.sendGroupReply(ctx, b, msg.Chat.ID, text)
}

func (h *BotHandler) commandSetRepoPrivate(ctx context.Context, b *bot.Bot, msg *models.Message, repo string) {
	chatID, ok := h.getConnectedChat(msg.From.ID)
	i18n := h.i18nForUser(msg.From, ChatConfig{DefaultLang: h.cfg.Language})
	if !ok {
		h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "private.need_connect", nil))
		return
	}
	h.commandSetRepoForChat(ctx, b, msg.From, chatID, repo, msg.Chat.ID, i18n)
}

func (h *BotHandler) commandSetTTLPrivate(ctx context.Context, b *bot.Bot, msg *models.Message, ttlText string) {
	chatID, ok := h.getConnectedChat(msg.From.ID)
	i18n := h.i18nForUser(msg.From, ChatConfig{DefaultLang: h.cfg.Language})
	if !ok {
		h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "private.need_connect", nil))
		return
	}
	chatCfg, ok := h.getChatConfigForUpdate(ctx, b, chatID, msg.From, msg.Chat.ID)
	if !ok {
		return
	}
	if strings.TrimSpace(ttlText) == "" {
		h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "group.setttl.usage", nil))
		return
	}
	ttl, err := time.ParseDuration(ttlText)
	if err != nil || ttl <= 0 {
		h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "group.setttl.invalid", nil))
		return
	}
	chatCfg.QuestionTTL = ttl
	chatCfg.UpdatedAt = time.Now()
	if err := h.store.UpsertChatConfig(ctx, chatCfg); err != nil {
		slog.Error("写入群配置失败", "error", err, "chat_id", chatID)
		h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "group.save_failed", nil))
		return
	}
	_ = h.store.InsertAudit(ctx, "set_ttl", "admin:"+strconv.FormatInt(msg.From.ID, 10), ttl.String())
	h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "group.setttl.success", map[string]string{"ttl": formatCode(formatDuration(ttl))}))
}

func (h *BotHandler) commandSetTriesPrivate(ctx context.Context, b *bot.Bot, msg *models.Message, triesText string) {
	chatID, ok := h.getConnectedChat(msg.From.ID)
	i18n := h.i18nForUser(msg.From, ChatConfig{DefaultLang: h.cfg.Language})
	if !ok {
		h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "private.need_connect", nil))
		return
	}
	chatCfg, ok := h.getChatConfigForUpdate(ctx, b, chatID, msg.From, msg.Chat.ID)
	if !ok {
		return
	}
	triesText = strings.TrimSpace(triesText)
	if triesText == "" {
		h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "group.settries.usage", nil))
		return
	}
	tries, err := strconv.Atoi(triesText)
	if err != nil || tries <= 0 {
		h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "group.settries.invalid", nil))
		return
	}
	chatCfg.MaxAttempts = tries
	chatCfg.UpdatedAt = time.Now()
	if err := h.store.UpsertChatConfig(ctx, chatCfg); err != nil {
		slog.Error("写入群配置失败", "error", err, "chat_id", chatID)
		h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "group.save_failed", nil))
		return
	}
	_ = h.store.InsertAudit(ctx, "set_tries", "admin:"+strconv.FormatInt(msg.From.ID, 10), strconv.Itoa(tries))
	h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "group.settries.success", map[string]string{"tries": formatCode(strconv.Itoa(tries))}))
}

func (h *BotHandler) commandSetLangPrivate(ctx context.Context, b *bot.Bot, msg *models.Message, lang string) {
	chatID, ok := h.getConnectedChat(msg.From.ID)
	i18n := h.i18nForUser(msg.From, ChatConfig{DefaultLang: h.cfg.Language})
	if !ok {
		h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "private.need_connect", nil))
		return
	}
	chatCfg, ok := h.getChatConfigForUpdate(ctx, b, chatID, msg.From, msg.Chat.ID)
	if !ok {
		return
	}
	lang = NormalizeLang(lang)
	if lang == "" {
		h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "group.setlang.usage", nil))
		return
	}
	if !IsLangAvailable(lang) {
		h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "group.setlang.invalid", map[string]string{"lang": formatCode(lang)}))
		return
	}
	chatCfg.DefaultLang = lang
	chatCfg.UpdatedAt = time.Now()
	if err := h.store.UpsertChatConfig(ctx, chatCfg); err != nil {
		slog.Error("写入群配置失败", "error", err, "chat_id", chatID)
		h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "group.save_failed", nil))
		return
	}
	_ = h.store.InsertAudit(ctx, "set_lang", "admin:"+strconv.FormatInt(msg.From.ID, 10), lang)
	h.sendGroupReply(ctx, b, msg.Chat.ID, formatMarkdown(i18n, "group.setlang.success", map[string]string{"lang": formatCode(lang)}))
}

func (h *BotHandler) getChatConfigForUpdate(ctx context.Context, b *bot.Bot, targetChatID int64, actor *models.User, replyChatID int64) (ChatConfig, bool) {
	if actor == nil || !h.isGroupAdmin(ctx, b, targetChatID, actor.ID) {
		h.sendGroupReply(ctx, b, replyChatID, formatMarkdown(h.i18n, "group.setrepo.admin_only", nil))
		return ChatConfig{}, false
	}
	chatCfg, err := h.store.GetChatConfig(ctx, targetChatID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			h.sendGroupReply(ctx, b, replyChatID, formatMarkdown(h.i18n, "group.config_missing", nil))
			return ChatConfig{}, false
		}
		slog.Error("读取群配置失败", "error", err, "chat_id", targetChatID)
		h.sendGroupReply(ctx, b, replyChatID, formatMarkdown(h.i18n, "group.save_failed", nil))
		return ChatConfig{}, false
	}
	return h.applyChatDefaults(chatCfg), true
}

func (h *BotHandler) isGroupAdmin(ctx context.Context, b *bot.Bot, chatID, userID int64) bool {
	member, err := b.GetChatMember(ctx, &bot.GetChatMemberParams{ChatID: chatID, UserID: userID})
	if err != nil {
		slog.Warn("获取群成员信息失败", "error", err, "chat_id", chatID)
		return false
	}
	if member == nil {
		return false
	}
	switch member.Type {
	case models.ChatMemberTypeAdministrator, models.ChatMemberTypeOwner:
		return true
	default:
		return false
	}
}

func (h *BotHandler) sendGroupReply(ctx context.Context, b *bot.Bot, chatID int64, text string) {
	_, err := b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: text, ParseMode: "MarkdownV2"})
	if err != nil && !errors.Is(err, context.Canceled) {
		slog.Warn("发送群消息失败", "error", err, "chat_id", chatID)
	}
}

func (h *BotHandler) matchBotMention(ctx context.Context, b *bot.Bot, mention string) bool {
	username := h.ensureBotUsername(ctx, b)
	if username == "" {
		return false
	}
	mention = strings.TrimPrefix(mention, "@")
	username = strings.TrimPrefix(username, "@")
	return strings.EqualFold(mention, username)
}

func (h *BotHandler) ensureBotUsername(ctx context.Context, b *bot.Bot) string {
	h.botMu.Lock()
	defer h.botMu.Unlock()
	if h.botName != "" {
		return h.botName
	}
	me, err := b.GetMe(ctx)
	if err != nil {
		slog.Warn("获取机器人信息失败", "error", err)
		return ""
	}
	h.botName = me.Username
	return h.botName
}

func (h *BotHandler) clearWarn(chatID int64) {
	h.warnMu.Lock()
	delete(h.lastWarn, chatID)
	h.warnMu.Unlock()
}

func (h *BotHandler) handleMyChatMember(ctx context.Context, b *bot.Bot, upd *models.ChatMemberUpdated) {
	if upd == nil || upd.Chat.ID == 0 {
		return
	}
	chatID := upd.Chat.ID
	newType := upd.NewChatMember.Type
	switch newType {
	case models.ChatMemberTypeMember, models.ChatMemberTypeAdministrator:
		slog.Info("机器人加入群聊", "chat_id", chatID, "chat_title", upd.Chat.Title, "status", newType)
		h.onBotAddedToChat(ctx, b, upd)
	case models.ChatMemberTypeLeft, models.ChatMemberTypeBanned:
		slog.Info("机器人离开群聊", "chat_id", chatID, "chat_title", upd.Chat.Title, "status", newType)
		h.onBotRemovedFromChat(ctx, chatID)
	}
}

func (h *BotHandler) sendQuestionAgain(ctx context.Context, b *bot.Bot, chatID int64, questionID int64, ttl time.Duration, i18n *I18n) {
	question, err := h.store.GetQuestion(ctx, questionID)
	if err != nil {
		return
	}
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text: formatMarkdown(i18n, "verify.prompt_again", map[string]string{
			"ttl":      formatCode(formatDuration(ttl)),
			"question": question.Prompt,
			"repo":     formatRepoLink(question.Repo),
		}),
		ParseMode: "MarkdownV2",
	})
}

func (h *BotHandler) onBotAddedToChat(ctx context.Context, b *bot.Bot, upd *models.ChatMemberUpdated) {
	chatID := upd.Chat.ID
	missing := missingPermissions(upd.NewChatMember)
	if len(missing) > 0 {
		msg := formatMarkdown(h.i18n, "group.permissions_missing", map[string]string{"missing": formatCode(strings.Join(missing, "、"))})
		h.sendGroupReply(ctx, b, chatID, msg)
		slog.Warn("机器人权限不足", "chat_id", chatID, "missing", strings.Join(missing, ","))
	} else {
		slog.Info("机器人权限检查通过", "chat_id", chatID)
	}

	chatInfo, err := b.GetChat(ctx, &bot.GetChatParams{ChatID: chatID})
	if err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			slog.Warn("获取群信息失败", "error", err, "chat_id", chatID)
		}
	} else if chatInfo != nil {
		if !chatRequiresJoinApproval(chatInfo) {
			h.sendGroupReply(ctx, b, chatID, formatMarkdown(h.i18n, "group.join_request_required", nil))
			slog.Warn("群未开启加入审核", "chat_id", chatID)
		} else {
			slog.Info("群已开启加入审核", "chat_id", chatID)
		}
	}

	if _, err := h.store.GetChatConfig(ctx, chatID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			h.notifyMissingConfig(ctx, b, chatID)
		} else if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			slog.Error("读取群配置失败", "error", err, "chat_id", chatID)
		}
	}
}

func (h *BotHandler) onBotRemovedFromChat(ctx context.Context, chatID int64) {
	if chatID == 0 {
		return
	}
	if err := h.store.DeleteChatData(ctx, chatID); err != nil {
		slog.Error("清除群配置失败", "error", err, "chat_id", chatID)
		return
	}
	h.clearWarn(chatID)
	slog.Info("已清除群配置", "chat_id", chatID)
}

func (h *BotHandler) approve(ctx context.Context, b *bot.Bot, pending PendingMember) error {
	_, err := b.ApproveChatJoinRequest(ctx, &bot.ApproveChatJoinRequestParams{
		ChatID: pending.ChatID,
		UserID: pending.TelegramID,
	})
	if err != nil {
		slog.Debug("approveChatJoinRequest 失败，可能是非申请入群", "error", err)
	}
	_ = h.store.DeletePending(ctx, pending.TelegramID, pending.ChatID)
	_ = h.store.InsertAudit(ctx, "approve", "bot", "approved")
	return nil
}

func (h *BotHandler) reject(ctx context.Context, b *bot.Bot, pending PendingMember, reason string) error {
	_, err := b.DeclineChatJoinRequest(ctx, &bot.DeclineChatJoinRequestParams{
		ChatID: pending.ChatID,
		UserID: pending.TelegramID,
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		slog.Debug("declineChatJoinRequest 失败，尝试踢出成员", "error", err)
		_, banErr := b.BanChatMember(ctx, &bot.BanChatMemberParams{
			ChatID: pending.ChatID,
			UserID: pending.TelegramID,
		})
		if banErr != nil {
			slog.Warn("踢出成员失败", "error", banErr)
		}
	}
	_ = h.store.DeletePending(ctx, pending.TelegramID, pending.ChatID)
	_ = h.store.InsertAudit(ctx, "reject", "bot", reason)
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    pending.TelegramID,
		Text:      reason,
		ParseMode: "MarkdownV2",
	})
	return err
}

func (h *BotHandler) ensureChatConfig(ctx context.Context, b *bot.Bot, chatID int64) (ChatConfig, bool) {
	cfg, err := h.store.GetChatConfig(ctx, chatID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			h.notifyMissingConfig(ctx, b, chatID)
		} else if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			slog.Error("读取群配置失败", "error", err, "chat_id", chatID)
		}
		return ChatConfig{}, false
	}
	return h.applyChatDefaults(cfg), true
}

func (h *BotHandler) notifyMissingConfig(ctx context.Context, b *bot.Bot, chatID int64) {
	const cooldown = 5 * time.Minute
	h.warnMu.Lock()
	if last, ok := h.lastWarn[chatID]; ok && time.Since(last) < cooldown {
		h.warnMu.Unlock()
		return
	}
	h.lastWarn[chatID] = time.Now()
	h.warnMu.Unlock()

	text := formatMarkdown(h.i18n, "group.config_missing", nil)
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: text, ParseMode: "MarkdownV2"}); err != nil {
		slog.Warn("发送配置提醒失败", "error", err, "chat_id", chatID)
	}
}

func isNewMember(oldMember, newMember models.ChatMember) bool {
	oldStatus := oldMember.Type
	newStatus := newMember.Type
	if (oldStatus == models.ChatMemberTypeLeft || oldStatus == models.ChatMemberTypeBanned) &&
		(newStatus == models.ChatMemberTypeMember || newStatus == models.ChatMemberTypeRestricted) {
		return true
	}
	return false
}

func extractUserID(member models.ChatMember) (int64, bool) {
	if member.Type == models.ChatMemberTypeMember && member.Member != nil && member.Member.User != nil {
		return member.Member.User.ID, true
	}
	if member.Type == models.ChatMemberTypeRestricted && member.Restricted != nil && member.Restricted.User != nil {
		return member.Restricted.User.ID, true
	}
	return 0, false
}

func chatMemberUser(member models.ChatMember) *models.User {
	switch member.Type {
	case models.ChatMemberTypeMember:
		if member.Member != nil {
			return member.Member.User
		}
	case models.ChatMemberTypeRestricted:
		if member.Restricted != nil {
			return member.Restricted.User
		}
	}
	return nil
}

func missingPermissions(member models.ChatMember) []string {
	if member.Type != models.ChatMemberTypeAdministrator || member.Administrator == nil {
		return []string{"管理员权限（请将机器人设为管理员）", "邀请新成员", "封禁成员"}
	}
	admin := member.Administrator
	missing := make([]string, 0, 2)
	if !admin.CanInviteUsers {
		missing = append(missing, "邀请新成员")
	}
	if !admin.CanRestrictMembers {
		missing = append(missing, "封禁成员")
	}
	return missing
}

func chatRequiresJoinApproval(chat *models.ChatFullInfo) bool {
	if chat == nil {
		return false
	}
	if chat.JoinToSendMessages {
		return true
	}
	if chat.JoinByRequest {
		return true
	}
	return false
}

func normalizeAnswer(text string) string {
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "\u00a0", " ")
	text = strings.ToLower(text)
	return strings.TrimSpace(text)
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	if d%time.Hour == 0 {
		return strconv.FormatInt(int64(d/time.Hour), 10) + "h"
	}
	if d%time.Minute == 0 {
		return strconv.FormatInt(int64(d/time.Minute), 10) + "m"
	}
	if d%time.Second == 0 {
		return strconv.FormatInt(int64(d/time.Second), 10) + "s"
	}
	return d.String()
}
