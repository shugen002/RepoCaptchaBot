package main

import (
	"errors"
	"os"
	"strconv"
	"time"
)

type Config struct {
	BotToken    string
	GithubToken string
	DBPath      string
	QuestionTTL time.Duration
	FilePath    string
	FileLine    int
	Language    string
	MaxAttempts int
}

func LoadConfig() (Config, error) {
	cfg := Config{
		BotToken:    os.Getenv("BOT_TOKEN"),
		GithubToken: os.Getenv("GITHUB_TOKEN"),
		DBPath:      os.Getenv("DB_PATH"),
		FilePath:    os.Getenv("FILE_PATH"),
		Language:    os.Getenv("BOT_LANG"),
	}

	if cfg.DBPath == "" {
		cfg.DBPath = "./repo_captcha_bot.db"
	}

	if ttlStr := os.Getenv("QUESTION_TTL"); ttlStr != "" {
		if ttl, err := time.ParseDuration(ttlStr); err == nil {
			cfg.QuestionTTL = ttl
		}
	}
	if cfg.QuestionTTL == 0 {
		cfg.QuestionTTL = 120 * time.Second
	}

	if attemptsStr := os.Getenv("MAX_ATTEMPTS"); attemptsStr != "" {
		if attempts, err := strconv.Atoi(attemptsStr); err == nil {
			cfg.MaxAttempts = attempts
		}
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 1
	}

	if lineStr := os.Getenv("FILE_LINE"); lineStr != "" {
		if line, err := strconv.Atoi(lineStr); err == nil {
			cfg.FileLine = line
		}
	}

	if cfg.BotToken == "" {
		return cfg, errors.New("BOT_TOKEN 不能为空")
	}

	return cfg, nil
}
