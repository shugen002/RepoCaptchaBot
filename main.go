package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/bartventer/httpcache"
	_ "github.com/bartventer/httpcache/store/memcache"
	gh "github.com/google/go-github/v83/github"

	"github.com/go-telegram/bot"
	_ "modernc.org/sqlite"

	"github.com/shugen002/RepoCaptchaBot/models"
	"github.com/shugen002/RepoCaptchaBot/utils"
	"github.com/shugen002/RepoCaptchaBot/verifier"
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

	store := models.NewStore(db)
	if err := store.Init(ctx); err != nil {
		logger.Error("初始化数据库失败", "error", err)
		os.Exit(1)
	}

	i18n, err := utils.LoadI18n(cfg.Language)
	if err != nil {
		logger.Error("加载多语言配置失败", "error", err)
		os.Exit(1)
	}

	ghHTTPClient := httpcache.NewClient("memcache://")
	ghHTTPClient.Timeout = 20 * time.Second
	ghClient := gh.NewClient(ghHTTPClient)
	if strings.TrimSpace(cfg.GithubToken) != "" {
		ghClient = ghClient.WithAuthToken(cfg.GithubToken)
	}
	verifier := verifier.NewVerifier(ghClient, store)
	handler := NewBotHandler(cfg, store, verifier, i18n)

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
	if me, err := b.GetMe(ctx); err != nil {
		logger.Warn("获取机器人信息失败", "error", err)
	} else {
		logger.Info("机器人已登录", "username", me.Username, "id", me.ID, "name", me.FirstName)
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
