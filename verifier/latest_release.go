package verifier

import (
	"context"

	"github.com/shugen002/RepoCaptchaBot/models"
	"github.com/shugen002/RepoCaptchaBot/utils"
)

func (v *Verifier) questionLatestRelease(ctx context.Context, cfg models.ChatConfig, i18n *utils.I18n) (Question, bool, error) {
	release, err := getLatestRelease(ctx, v.gh, cfg.Repo)
	if err != nil {
		if isGitHubNotFound(err) {
			return Question{}, false, nil
		}
		return Question{}, false, err
	}
	if release == nil || release.GetTagName() == "" {
		return Question{}, false, nil
	}
	return Question{
		Generator: "latest_release",
		Type:      "latest_release",
		Question:  utils.FormatMarkdown(i18n, "question.latest_release", map[string]string{"repo": utils.FormatRepoLink(cfg.Repo)}),
		Data: map[string]interface{}{
			"answer": release.GetTagName(),
		},
	}, true, nil
}
