package main

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"
)

type Question struct {
	Prompt  string
	Answer  string
	Type    string
	Payload string
}

type Verifier struct {
	gh  *GitHubClient
	rnd *rand.Rand
}

func NewVerifier(gh *GitHubClient) *Verifier {
	return &Verifier{
		gh:  gh,
		rnd: rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), uint64(time.Now().UnixNano())+1)),
	}
}

type questionGenerator func(context.Context, ChatConfig) (Question, bool, error)

func (v *Verifier) GenerateQuestion(ctx context.Context, cfg ChatConfig) (Question, error) {
	if strings.TrimSpace(cfg.Repo) == "" {
		return Question{}, errors.New("repo 未配置")
	}
	generators := []questionGenerator{
		v.questionLatestCommitAuthor,
		v.questionRepoLanguage,
		v.questionLatestCommitMessage,
		v.questionLatestRelease,
		v.questionFileLine,
	}

	available := make([]Question, 0, len(generators))
	for _, gen := range generators {
		q, ok, err := gen(ctx, cfg)
		if err != nil {
			continue
		}
		if ok {
			available = append(available, q)
		}
	}

	if len(available) == 0 {
		return Question{}, errors.New("无法生成题目")
	}

	idx := v.rnd.IntN(len(available))
	return available[idx], nil
}

func (v *Verifier) questionLatestCommitAuthor(ctx context.Context, cfg ChatConfig) (Question, bool, error) {
	commit, err := v.gh.GetLatestCommit(ctx, cfg.Repo)
	if err != nil {
		return Question{}, false, err
	}
	payload, _ := json.Marshal(map[string]string{"type": "latest_commit_author"})
	return Question{
		Prompt:  "最近一次提交的作者是谁？",
		Answer:  commit.AuthorName,
		Type:    "latest_commit_author",
		Payload: string(payload),
	}, true, nil
}

func (v *Verifier) questionRepoLanguage(ctx context.Context, cfg ChatConfig) (Question, bool, error) {
	repo, err := v.gh.GetRepo(ctx, cfg.Repo)
	if err != nil {
		return Question{}, false, err
	}
	if repo.Language == "" {
		return Question{}, false, nil
	}
	payload, _ := json.Marshal(map[string]string{"type": "repo_language"})
	return Question{
		Prompt:  "仓库的主要编程语言是什么？",
		Answer:  repo.Language,
		Type:    "repo_language",
		Payload: string(payload),
	}, true, nil
}

func (v *Verifier) questionLatestCommitMessage(ctx context.Context, cfg ChatConfig) (Question, bool, error) {
	commit, err := v.gh.GetLatestCommit(ctx, cfg.Repo)
	if err != nil {
		return Question{}, false, err
	}
	payload, _ := json.Marshal(map[string]string{"type": "latest_commit_message"})
	return Question{
		Prompt:  "最后一次提交的提交信息是什么？",
		Answer:  commit.Message,
		Type:    "latest_commit_message",
		Payload: string(payload),
	}, true, nil
}

func (v *Verifier) questionLatestRelease(ctx context.Context, cfg ChatConfig) (Question, bool, error) {
	release, err := v.gh.GetLatestRelease(ctx, cfg.Repo)
	if err != nil {
		if errors.Is(err, ErrGitHubNotFound) {
			return Question{}, false, nil
		}
		return Question{}, false, err
	}
	if release.Tag == "" {
		return Question{}, false, nil
	}
	payload, _ := json.Marshal(map[string]string{"type": "latest_release"})
	return Question{
		Prompt:  "最后的Release版本号是多少？",
		Answer:  release.Tag,
		Type:    "latest_release",
		Payload: string(payload),
	}, true, nil
}

func (v *Verifier) questionFileLine(ctx context.Context, cfg ChatConfig) (Question, bool, error) {
	if cfg.FilePath == "" || cfg.FileLine <= 0 {
		return Question{}, false, nil
	}
	line, err := v.gh.GetFileLine(ctx, cfg.Repo, cfg.FilePath, cfg.FileLine)
	if err != nil {
		return Question{}, false, err
	}
	payload, _ := json.Marshal(map[string]interface{}{"type": "file_line", "path": cfg.FilePath, "line": cfg.FileLine})
	return Question{
		Prompt:  "文件 " + cfg.FilePath + " 的第 " + strconv.Itoa(cfg.FileLine) + " 行内容是什么？",
		Answer:  line,
		Type:    "file_line",
		Payload: string(payload),
	}, true, nil
}
