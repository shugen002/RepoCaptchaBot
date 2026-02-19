package verifier

import (
	"context"
	"errors"
	"strconv"
	"strings"

	gh "github.com/google/go-github/v83/github"

	"github.com/shugen002/RepoCaptchaBot/utils"
)

const (
	repoInfoGeneratorName = "repo_info"
	repoTypeLanguage      = "repo_language"
	repoTypeOpenIssues    = "repo_open_issues"
	repoTypeDefaultBranch = "repo_default_branch"
	repoTypeStars         = "repo_stars"
	repoQuestionLanguage  = "question.repo_language"
	repoQuestionOpenIssue = "question.repo_open_issues"
	repoQuestionBranch    = "question.repo_default_branch"
	repoQuestionStars     = "question.repo_stars"
)

type repoInfoGenerator struct {
	gh   *gh.Client
	repo string
	i18n *utils.I18n
}

func (g *repoInfoGenerator) name() string {
	return repoInfoGeneratorName
}

func (g *repoInfoGenerator) types() []string {
	return []string{repoTypeLanguage, repoTypeOpenIssues, repoTypeDefaultBranch, repoTypeStars}
}

func (g *repoInfoGenerator) generate(qType string) (Question, bool, error) {
	qType = strings.ToLower(strings.TrimSpace(qType))
	if qType == "" {
		return Question{}, false, ErrUnknownQuestionType
	}
	if g.repo == "" {
		return Question{}, false, errors.New("repo 未配置")
	}

	var key string
	switch qType {
	case repoTypeLanguage:
		key = repoQuestionLanguage
	case repoTypeOpenIssues:
		key = repoQuestionOpenIssue
	case repoTypeDefaultBranch:
		key = repoQuestionBranch
	case repoTypeStars:
		key = repoQuestionStars
	default:
		return Question{}, false, ErrUnknownQuestionType
	}

	return Question{
		Generator: g.name(),
		Type:      qType,
		Question:  utils.FormatMarkdown(g.i18n, key, map[string]string{"repo": utils.FormatRepoLink(g.repo)}),
		Data: map[string]interface{}{
			"repo": g.repo,
		},
	}, true, nil
}

func (g *repoInfoGenerator) verify(question *Question, answer string) bool {
	repo := repoFromQuestion(question)
	if repo == "" {
		return false
	}
	info, err := getRepo(context.Background(), g.gh, repo)
	if err != nil {
		return false
	}

	switch strings.ToLower(strings.TrimSpace(question.Type)) {
	case repoTypeLanguage:
		if info.GetLanguage() == "" {
			return false
		}
		return utils.NormalizeAnswer(answer) == utils.NormalizeAnswer(info.GetLanguage())
	case repoTypeOpenIssues:
		return utils.NormalizeAnswer(answer) == utils.NormalizeAnswer(strconv.Itoa(info.GetOpenIssuesCount()))
	case repoTypeDefaultBranch:
		if info.GetDefaultBranch() == "" {
			return false
		}
		return utils.NormalizeAnswer(answer) == utils.NormalizeAnswer(info.GetDefaultBranch())
	case repoTypeStars:
		return utils.NormalizeAnswer(answer) == utils.NormalizeAnswer(strconv.Itoa(info.GetStargazersCount()))
	default:
		return false
	}
}
