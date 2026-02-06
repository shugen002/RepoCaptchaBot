package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type Store struct {
	db *sql.DB
}

type PendingMember struct {
	TelegramID int64
	ChatID     int64
	QuestionID int64
	ExpiresAt  time.Time
}

type StoredQuestion struct {
	ID        int64
	Repo      string
	Type      string
	Prompt    string
	Payload   string
	Answer    string
	CreatedAt time.Time
}

type ChatConfig struct {
	ChatID    int64
	Repo      string
	FilePath  string
	FileLine  int
	UpdatedAt time.Time
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Init(ctx context.Context) error {
	schema := []string{
		`CREATE TABLE IF NOT EXISTS questions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repo TEXT NOT NULL,
			type TEXT NOT NULL,
			prompt TEXT NOT NULL,
			payload TEXT NOT NULL,
			answer TEXT NOT NULL,
			created_at INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS pending_members (
			telegram_id INTEGER NOT NULL,
			chat_id INTEGER NOT NULL,
			question_id INTEGER NOT NULL,
			expires_at INTEGER NOT NULL,
			created_at INTEGER NOT NULL,
			PRIMARY KEY (telegram_id, chat_id)
		);`,
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			action TEXT NOT NULL,
			actor TEXT NOT NULL,
			detail TEXT NOT NULL,
			created_at INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS chat_configs (
			chat_id INTEGER PRIMARY KEY,
			repo TEXT NOT NULL,
			file_path TEXT,
			file_line INTEGER,
			updated_at INTEGER NOT NULL
		);`,
	}

	for _, stmt := range schema {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	_, _ = s.db.ExecContext(ctx, `ALTER TABLE questions ADD COLUMN prompt TEXT NOT NULL DEFAULT ''`)
	return nil
}

func (s *Store) InsertQuestion(ctx context.Context, q StoredQuestion) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO questions (repo, type, prompt, payload, answer, created_at) VALUES (?, ?, ?, ?, ?, ?)`+
			``,
		q.Repo, q.Type, q.Prompt, q.Payload, q.Answer, q.CreatedAt.Unix(),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpsertPending(ctx context.Context, p PendingMember) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO pending_members (telegram_id, chat_id, question_id, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(telegram_id, chat_id) DO UPDATE SET question_id = excluded.question_id, expires_at = excluded.expires_at`,
		p.TelegramID, p.ChatID, p.QuestionID, p.ExpiresAt.Unix(), time.Now().Unix(),
	)
	return err
}

func (s *Store) GetPendingByTelegram(ctx context.Context, telegramID int64) (PendingMember, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT telegram_id, chat_id, question_id, expires_at FROM pending_members WHERE telegram_id = ?`,
		telegramID,
	)
	var p PendingMember
	var expires int64
	if err := row.Scan(&p.TelegramID, &p.ChatID, &p.QuestionID, &expires); err != nil {
		return PendingMember{}, err
	}
	p.ExpiresAt = time.Unix(expires, 0)
	return p, nil
}

func (s *Store) GetQuestion(ctx context.Context, questionID int64) (StoredQuestion, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, repo, type, prompt, payload, answer, created_at FROM questions WHERE id = ?`,
		questionID,
	)
	var q StoredQuestion
	var created int64
	if err := row.Scan(&q.ID, &q.Repo, &q.Type, &q.Prompt, &q.Payload, &q.Answer, &created); err != nil {
		return StoredQuestion{}, err
	}
	q.CreatedAt = time.Unix(created, 0)
	return q, nil
}

func (s *Store) DeletePending(ctx context.Context, telegramID, chatID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM pending_members WHERE telegram_id = ? AND chat_id = ?`,
		telegramID, chatID,
	)
	return err
}

func (s *Store) CleanupExpired(ctx context.Context, now time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM pending_members WHERE expires_at < ?`,
		now.Unix(),
	)
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return affected, nil
}

func (s *Store) InsertAudit(ctx context.Context, action, actor, detail string) error {
	if action == "" {
		return errors.New("action 不能为空")
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_logs (action, actor, detail, created_at) VALUES (?, ?, ?, ?)`,
		action, actor, detail, time.Now().Unix(),
	)
	return err
}

func (s *Store) UpsertChatConfig(ctx context.Context, cfg ChatConfig) error {
	if cfg.ChatID == 0 {
		return errors.New("chat_id 不能为空")
	}
	if strings.TrimSpace(cfg.Repo) == "" {
		return errors.New("repo 不能为空")
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO chat_configs (chat_id, repo, file_path, file_line, updated_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(chat_id) DO UPDATE SET repo = excluded.repo, file_path = excluded.file_path, file_line = excluded.file_line, updated_at = excluded.updated_at`,
		cfg.ChatID, cfg.Repo, cfg.FilePath, cfg.FileLine, cfg.UpdatedAt.Unix(),
	)
	return err
}

func (s *Store) GetChatConfig(ctx context.Context, chatID int64) (ChatConfig, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT chat_id, repo, IFNULL(file_path, ''), IFNULL(file_line, 0), updated_at FROM chat_configs WHERE chat_id = ?`,
		chatID,
	)
	var cfg ChatConfig
	var updated int64
	if err := row.Scan(&cfg.ChatID, &cfg.Repo, &cfg.FilePath, &cfg.FileLine, &updated); err != nil {
		return ChatConfig{}, err
	}
	cfg.UpdatedAt = time.Unix(updated, 0)
	return cfg, nil
}
