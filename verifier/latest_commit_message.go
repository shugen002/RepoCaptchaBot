package verifier

import (
	"context"
	"encoding/json"

	"github.com/shugen002/RepoCaptchaBot/models"
	"github.com/shugen002/RepoCaptchaBot/utils"
)

func (v *Verifier) questionLatestCommitMessage(ctx context.Context, cfg models.ChatConfig, i18n *utils.I18n) (Question, bool, error) {
	commit, err := v.gh.GetLatestCommit(ctx, cfg.Repo)
	if err != nil {
		return Question{}, false, err
	}
	payload, _ := json.Marshal(map[string]string{"type": "latest_commit_message"})
	return Question{
		Prompt:  utils.FormatMarkdown(i18n, "question.latest_commit_message", map[string]string{"repo": utils.FormatRepoLink(cfg.Repo)}),
		Answer:  commit.Message,
		Type:    "latest_commit_message",
		Payload: string(payload),
	}, true, nil
}
