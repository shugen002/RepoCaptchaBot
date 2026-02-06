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
	warnMu   sync.Mutex
	lastWarn map[int64]time.Time
	botMu    sync.Mutex
	botName  string
}

func NewBotHandler(cfg Config, store *Store, verifier *Verifier) *BotHandler {
	return &BotHandler{cfg: cfg, store: store, verifier: verifier, lastWarn: make(map[int64]time.Time)}
}

func (h *BotHandler) HandleUpdate(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update == nil {
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

	q, err := h.verifier.GenerateQuestion(ctx, chatCfg)
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

	expiresAt := time.Now().Add(h.cfg.QuestionTTL)
	if err := h.store.UpsertPending(ctx, PendingMember{
		TelegramID: req.From.ID,
		ChatID:     req.Chat.ID,
		QuestionID: qid,
		ExpiresAt:  expiresAt,
	}); err != nil {
		slog.Error("保存待验证用户失败", "error", err)
		return
	}

	text := "请在 " + h.cfg.QuestionTTL.String() + " 内回答以下问题以完成验证：\n\n" + q.Prompt + "\n\n" +
		"请直接回复答案。"

	_, err = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: req.From.ID,
		Text:   text,
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

	chatCfg, hasCfg := h.ensureChatConfig(ctx, b, chatID)
	if !hasCfg {
		return
	}

	q, err := h.verifier.GenerateQuestion(ctx, chatCfg)
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

	expiresAt := time.Now().Add(h.cfg.QuestionTTL)
	if err := h.store.UpsertPending(ctx, PendingMember{
		TelegramID: userID,
		ChatID:     upd.Chat.ID,
		QuestionID: qid,
		ExpiresAt:  expiresAt,
	}); err != nil {
		slog.Error("保存待验证用户失败", "error", err)
		return
	}

	text := "请在 " + h.cfg.QuestionTTL.String() + " 内回答以下问题以完成验证：\n\n" + q.Prompt + "\n\n" +
		"请直接回复答案。"
	_, err = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: userID,
		Text:   text,
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

	if strings.HasPrefix(msg.Text, "/start") {
		if time.Now().After(pending.ExpiresAt) {
			_ = h.reject(ctx, b, pending, "验证已超时，请重新申请入群")
			return
		}
		h.sendQuestionAgain(ctx, b, msg.Chat.ID, pending.QuestionID)
		return
	}

	if time.Now().After(pending.ExpiresAt) {
		_ = h.reject(ctx, b, pending, "验证已超时，请重新申请入群")
		return
	}

	question, err := h.store.GetQuestion(ctx, pending.QuestionID)
	if err != nil {
		slog.Error("读取题目失败", "error", err)
		return
	}

	if normalizeAnswer(msg.Text) == normalizeAnswer(question.Answer) {
		if err := h.approve(ctx, b, pending); err != nil {
			slog.Warn("审批失败", "error", err)
		}
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text:   "验证成功，已批准入群。",
		})
		return
	}

	_ = h.reject(ctx, b, pending, "答案不正确，已拒绝入群申请。")
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
	}
}

func (h *BotHandler) commandSetRepo(ctx context.Context, b *bot.Bot, msg *models.Message, repo string) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		h.sendGroupReply(ctx, b, msg.Chat.ID, "用法：/setrepo owner/name")
		return
	}
	if _, _, err := parseRepo(repo); err != nil {
		h.sendGroupReply(ctx, b, msg.Chat.ID, "仓库格式不正确，请使用 owner/name")
		return
	}
	if !h.isGroupAdmin(ctx, b, msg.Chat.ID, msg.From.ID) {
		h.sendGroupReply(ctx, b, msg.Chat.ID, "只有群管理员可以使用此命令。")
		return
	}
	cfg := ChatConfig{ChatID: msg.Chat.ID, Repo: repo, UpdatedAt: time.Now()}
	existing, err := h.store.GetChatConfig(ctx, msg.Chat.ID)
	if err == nil {
		cfg.FilePath = existing.FilePath
		cfg.FileLine = existing.FileLine
	} else if errors.Is(err, sql.ErrNoRows) {
		cfg.FilePath = h.cfg.FilePath
		cfg.FileLine = h.cfg.FileLine
	} else if err != nil {
		slog.Error("读取群配置失败", "error", err, "chat_id", msg.Chat.ID)
		h.sendGroupReply(ctx, b, msg.Chat.ID, "保存失败，请稍后重试。")
		return
	}
	if err := h.store.UpsertChatConfig(ctx, cfg); err != nil {
		slog.Error("写入群配置失败", "error", err, "chat_id", msg.Chat.ID)
		h.sendGroupReply(ctx, b, msg.Chat.ID, "保存失败，请稍后重试。")
		return
	}
	h.clearWarn(msg.Chat.ID)
	actor := "admin:" + strconv.FormatInt(msg.From.ID, 10)
	_ = h.store.InsertAudit(ctx, "set_repo", actor, repo)
	h.sendGroupReply(ctx, b, msg.Chat.ID, "仓库已更新为 "+repo)
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
	_, err := b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: text})
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

func (h *BotHandler) sendQuestionAgain(ctx context.Context, b *bot.Bot, chatID int64, questionID int64) {
	question, err := h.store.GetQuestion(ctx, questionID)
	if err != nil {
		return
	}
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   "请在 " + h.cfg.QuestionTTL.String() + " 内回答以下问题：\n\n" + question.Prompt,
	})
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
		ChatID: pending.TelegramID,
		Text:   reason,
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
	return cfg, true
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

	text := "尚未配置仓库，请群管理员发送 /setrepo owner/name 来设置。"
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: text}); err != nil {
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

func normalizeAnswer(text string) string {
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "\u00a0", " ")
	text = strings.ToLower(text)
	return strings.TrimSpace(text)
}
