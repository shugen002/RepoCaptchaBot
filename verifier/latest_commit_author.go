package verifier

import (
	"context"
	"encoding/json"

	"github.com/shugen002/RepoCaptchaBot/models"
	"github.com/shugen002/RepoCaptchaBot/utils"
)

func (v *Verifier) questionLatestCommitAuthor(ctx context.Context, cfg models.ChatConfig, i18n *utils.I18n) (Question, bool, error) {
	commit, err := v.gh.GetLatestCommit(ctx, cfg.Repo)
	if err != nil {
		return Question{}, false, err
	}
	payload, _ := json.Marshal(map[string]string{"type": "latest_commit_author"})
	return Question{
		Prompt:  utils.FormatMarkdown(i18n, "question.latest_commit_author", map[string]string{"repo": utils.FormatRepoLink(cfg.Repo)}),
		Answer:  commit.AuthorName,
		Type:    "latest_commit_author",
		Payload: string(payload),
	}, true, nil
}
