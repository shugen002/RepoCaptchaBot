package verifier

import (
	"context"
	"errors"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/shugen002/RepoCaptchaBot/models"
	"github.com/shugen002/RepoCaptchaBot/utils"
	githubapi "github.com/shugen002/RepoCaptchaBot/utils/github"
)

type Question struct {
	Prompt  string
	Answer  string
	Type    string
	Payload string
}

var ErrUnknownQuestionType = errors.New("unknown question type")

type Verifier struct {
	gh  *githubapi.GitHubClient
	db  *models.Store
	rnd *rand.Rand
}

func NewVerifier(gh *githubapi.GitHubClient, store *models.Store) *Verifier {
	return &Verifier{
		gh:  gh,
		db:  store,
		rnd: rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), uint64(time.Now().UnixNano())+1)),
	}
}

type questionGenerator func(context.Context, models.ChatConfig, *utils.I18n) (Question, bool, error)

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

func (v *Verifier) GenerateQuestion(ctx context.Context, cfg models.ChatConfig, i18n *utils.I18n) (Question, error) {
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

func (v *Verifier) GenerateQuestionByType(ctx context.Context, cfg models.ChatConfig, i18n *utils.I18n, qType string) (Question, bool, error) {
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

func (v *Verifier) AvailableQuestionTypes(ctx context.Context, cfg models.ChatConfig, i18n *utils.I18n) ([]string, error) {
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
