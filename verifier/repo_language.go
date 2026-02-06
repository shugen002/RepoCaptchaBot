package verifier

import (
	"context"
	"encoding/json"

	"github.com/shugen002/RepoCaptchaBot/models"
	"github.com/shugen002/RepoCaptchaBot/utils"
)

func (v *Verifier) questionRepoLanguage(ctx context.Context, cfg models.ChatConfig, i18n *utils.I18n) (Question, bool, error) {
	repo, err := v.gh.GetRepo(ctx, cfg.Repo)
	if err != nil {
		return Question{}, false, err
	}
	if repo.Language == "" {
		return Question{}, false, nil
	}
	payload, _ := json.Marshal(map[string]string{"type": "repo_language"})
	return Question{
		Prompt:  utils.FormatMarkdown(i18n, "question.repo_language", map[string]string{"repo": utils.FormatRepoLink(cfg.Repo)}),
		Answer:  repo.Language,
		Type:    "repo_language",
		Payload: string(payload),
	}, true, nil
}
