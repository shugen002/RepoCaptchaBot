package verifier

import (
	"context"
	"errors"
	"strings"

	gh "github.com/google/go-github/v83/github"

	"github.com/shugen002/RepoCaptchaBot/utils"
)

const (
	commitGeneratorName   = "latest_commit"
	commitTypeAuthor      = "latest_commit_author"
	commitTypeMessage     = "latest_commit_message"
	commitQuestionAuthor  = "question.latest_commit_author"
	commitQuestionMessage = "question.latest_commit_message"
)

type commitInfoGenerator struct {
	gh   *gh.Client
	repo string
	i18n *utils.I18n
}

func (g *commitInfoGenerator) name() string {
	return commitGeneratorName
}

func (g *commitInfoGenerator) types() []string {
	return []string{commitTypeAuthor, commitTypeMessage}
}

func (g *commitInfoGenerator) generate(qType string) (Question, bool, error) {
	qType = strings.ToLower(strings.TrimSpace(qType))
	if qType == "" {
		return Question{}, false, ErrUnknownQuestionType
	}
	if g.repo == "" {
		return Question{}, false, errors.New("repo 未配置")
	}

	var key string
	switch qType {
	case commitTypeAuthor:
		key = commitQuestionAuthor
	case commitTypeMessage:
		key = commitQuestionMessage
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

func (g *commitInfoGenerator) verify(question *Question, answer string) bool {
	repo := repoFromQuestion(question)
	if repo == "" {
		return false
	}
	info, err := getLatestCommit(context.Background(), g.gh, repo)
	if err != nil {
		return false
	}

	switch strings.ToLower(strings.TrimSpace(question.Type)) {
	case commitTypeAuthor:
		author := info.GetCommit().GetAuthor().GetName()
		if author == "" {
			return false
		}
		return utils.NormalizeAnswer(answer) == utils.NormalizeAnswer(author)
	case commitTypeMessage:
		message := info.GetCommit().GetMessage()
		if message == "" {
			return false
		}
		return utils.NormalizeAnswer(answer) == utils.NormalizeAnswer(message)
	default:
		return false
	}
}
