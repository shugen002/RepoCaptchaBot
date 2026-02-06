package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/go-telegram/bot"
	_ "modernc.org/sqlite"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := LoadConfig()
	if err != nil {
		logger.Error("配置加载失败", "error", err)
		os.Exit(1)
	}

	db, err := sql.Open("sqlite", cfg.DBPath)
	if err != nil {
		logger.Error("打开数据库失败", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	store := NewStore(db)
	if err := store.Init(ctx); err != nil {
		logger.Error("初始化数据库失败", "error", err)
		os.Exit(1)
	}

	ghClient := NewGitHubClient(cfg.GithubToken)
	verifier := NewVerifier(ghClient)
	handler := NewBotHandler(cfg, store, verifier)

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyFromEnvironment
	transport.TLSHandshakeTimeout = 30 * time.Second

	opts := []bot.Option{
		bot.WithHTTPClient(30*time.Second, &http.Client{
			Timeout:   35 * time.Second,
			Transport: transport,
		}),
		bot.WithDefaultHandler(handler.HandleUpdate),
	}

	b, err := bot.New(cfg.BotToken, opts...)
	if err != nil {
		panic(err)
	}

	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := store.CleanupExpired(ctx, time.Now()); err != nil {
					logger.Warn("清理过期记录失败", "error", err)
				}
			}
		}
	}()

	b.Start(ctx)
}
