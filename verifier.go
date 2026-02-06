package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type Question struct {
	Prompt  string
	Answer  string
	Type    string
	Payload string
}

var ErrUnknownQuestionType = errors.New("unknown question type")

type Verifier struct {
	gh  *GitHubClient
	db  *Store
	rnd *rand.Rand
}

func NewVerifier(gh *GitHubClient, store *Store) *Verifier {
	return &Verifier{
		gh:  gh,
		db:  store,
		rnd: rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), uint64(time.Now().UnixNano())+1)),
	}
}

type questionGenerator func(context.Context, ChatConfig, *I18n) (Question, bool, error)

type questionGeneratorInfo struct {
	Type string
	Gen  questionGenerator
}

func (v *Verifier) questionGenerators() []questionGeneratorInfo {
	return []questionGeneratorInfo{
		{Type: "latest_commit_author", Gen: v.questionLatestCommitAuthor},
		{Type: "repo_language", Gen: v.questionRepoLanguage},
		{Type: "latest_commit_message", Gen: v.questionLatestCommitMessage},
		{Type: "latest_release", Gen: v.questionLatestRelease},
		{Type: "file_line", Gen: v.questionFileLine},
	}
}

func (v *Verifier) GenerateQuestion(ctx context.Context, cfg ChatConfig, i18n *I18n) (Question, error) {
	if strings.TrimSpace(cfg.Repo) == "" {
		return Question{}, errors.New("repo 未配置")
	}
	generators := v.questionGenerators()

	available := make([]Question, 0, len(generators))
	for _, gen := range generators {
		q, ok, err := gen.Gen(ctx, cfg, i18n)
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

func (v *Verifier) GenerateQuestionByType(ctx context.Context, cfg ChatConfig, i18n *I18n, qType string) (Question, bool, error) {
	qType = strings.TrimSpace(strings.ToLower(qType))
	if qType == "" {
		return Question{}, false, errors.New("type 不能为空")
	}
	for _, gen := range v.questionGenerators() {
		if gen.Type != qType {
			continue
		}
		q, ok, err := gen.Gen(ctx, cfg, i18n)
		if err != nil {
			return Question{}, false, err
		}
		return q, ok, nil
	}
	return Question{}, false, ErrUnknownQuestionType
}

func (v *Verifier) SupportedQuestionTypes() []string {
	gens := v.questionGenerators()
	types := make([]string, 0, len(gens))
	for _, gen := range gens {
		types = append(types, gen.Type)
	}
	return types
}

func (v *Verifier) AvailableQuestionTypes(ctx context.Context, cfg ChatConfig, i18n *I18n) ([]string, error) {
	generators := v.questionGenerators()
	available := make([]string, 0, len(generators))
	for _, gen := range generators {
		_, ok, err := gen.Gen(ctx, cfg, i18n)
		if err != nil {
			continue
		}
		if ok {
			available = append(available, gen.Type)
		}
	}
	return available, nil
}

func (v *Verifier) questionLatestCommitAuthor(ctx context.Context, cfg ChatConfig, i18n *I18n) (Question, bool, error) {
	commit, err := v.gh.GetLatestCommit(ctx, cfg.Repo)
	if err != nil {
		return Question{}, false, err
	}
	payload, _ := json.Marshal(map[string]string{"type": "latest_commit_author"})
	return Question{
		Prompt:  formatMarkdown(i18n, "question.latest_commit_author", map[string]string{"repo": formatRepoLink(cfg.Repo)}),
		Answer:  commit.AuthorName,
		Type:    "latest_commit_author",
		Payload: string(payload),
	}, true, nil
}

func (v *Verifier) questionRepoLanguage(ctx context.Context, cfg ChatConfig, i18n *I18n) (Question, bool, error) {
	repo, err := v.gh.GetRepo(ctx, cfg.Repo)
	if err != nil {
		return Question{}, false, err
	}
	if repo.Language == "" {
		return Question{}, false, nil
	}
	payload, _ := json.Marshal(map[string]string{"type": "repo_language"})
	return Question{
		Prompt:  formatMarkdown(i18n, "question.repo_language", map[string]string{"repo": formatRepoLink(cfg.Repo)}),
		Answer:  repo.Language,
		Type:    "repo_language",
		Payload: string(payload),
	}, true, nil
}

func (v *Verifier) questionLatestCommitMessage(ctx context.Context, cfg ChatConfig, i18n *I18n) (Question, bool, error) {
	commit, err := v.gh.GetLatestCommit(ctx, cfg.Repo)
	if err != nil {
		return Question{}, false, err
	}
	payload, _ := json.Marshal(map[string]string{"type": "latest_commit_message"})
	return Question{
		Prompt:  formatMarkdown(i18n, "question.latest_commit_message", map[string]string{"repo": formatRepoLink(cfg.Repo)}),
		Answer:  commit.Message,
		Type:    "latest_commit_message",
		Payload: string(payload),
	}, true, nil
}

func (v *Verifier) questionLatestRelease(ctx context.Context, cfg ChatConfig, i18n *I18n) (Question, bool, error) {
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
		Prompt:  formatMarkdown(i18n, "question.latest_release", map[string]string{"repo": formatRepoLink(cfg.Repo)}),
		Answer:  release.Tag,
		Type:    "latest_release",
		Payload: string(payload),
	}, true, nil
}

func (v *Verifier) questionFileLine(ctx context.Context, cfg ChatConfig, i18n *I18n) (Question, bool, error) {
	files, commitHash, err := v.getRepoFiles(ctx, cfg.Repo)
	if err != nil {
		return Question{}, false, err
	}
	if len(files) == 0 {
		return Question{}, false, nil
	}

	v.rnd.Shuffle(len(files), func(i, j int) {
		files[i], files[j] = files[j], files[i]
	})

	var lastErr error
	for _, path := range files {
		content, err := v.gh.GetFileContent(ctx, cfg.Repo, path)
		if err != nil {
			lastErr = err
			continue
		}
		candidates := extractValidLines(content)
		if len(candidates) == 0 {
			continue
		}
		picked := candidates[v.rnd.IntN(len(candidates))]
		payload, _ := json.Marshal(map[string]interface{}{
			"type":        "file_line",
			"path":        path,
			"line":        picked.Line,
			"commit_hash": commitHash,
		})
		return Question{
			Prompt:  formatMarkdown(i18n, "question.file_line", map[string]string{"repo": formatRepoLink(cfg.Repo), "path": formatCode(path), "line": formatCode(strconv.Itoa(picked.Line))}),
			Answer:  strings.TrimSpace(picked.Text),
			Type:    "file_line",
			Payload: string(payload),
		}, true, nil
	}

	if lastErr != nil {
		return Question{}, false, lastErr
	}
	return Question{}, false, nil
}

type lineCandidate struct {
	Line int
	Text string
}

func extractValidLines(content string) []lineCandidate {
	rawLines := strings.Split(content, "\n")
	candidates := make([]lineCandidate, 0, len(rawLines))
	for idx, line := range rawLines {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if isSymbolOnly(trimmed) {
			continue
		}
		candidates = append(candidates, lineCandidate{Line: idx + 1, Text: trimmed})
	}
	return candidates
}

func isSymbolOnly(text string) bool {
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func (v *Verifier) getRepoFiles(ctx context.Context, repo string) ([]string, string, error) {
	commitHash, err := v.gh.GetLatestCommitSHA(ctx, repo)
	if err != nil {
		return nil, "", err
	}

	if v.db != nil {
		cached, err := v.db.GetRepoFileCache(ctx, repo)
		if err == nil && cached.CommitHash == commitHash && len(cached.Files) > 0 {
			return cached.Files, commitHash, nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, "", err
		}
	}

	files, err := v.gh.GetRepoFileList(ctx, repo, commitHash, 2)
	if err != nil {
		return nil, "", err
	}

	if v.db != nil {
		_ = v.db.UpsertRepoFileCache(ctx, RepoFileCache{
			Repo:       repo,
			CommitHash: commitHash,
			Files:      files,
			UpdatedAt:  time.Now(),
		})
	}

	return files, commitHash, nil
}
