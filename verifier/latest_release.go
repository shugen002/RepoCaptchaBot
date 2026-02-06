package verifier

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/shugen002/RepoCaptchaBot/models"
	"github.com/shugen002/RepoCaptchaBot/utils"
	githubapi "github.com/shugen002/RepoCaptchaBot/utils/github"
)

func (v *Verifier) questionLatestRelease(ctx context.Context, cfg models.ChatConfig, i18n *utils.I18n) (Question, bool, error) {
	release, err := v.gh.GetLatestRelease(ctx, cfg.Repo)
	if err != nil {
		if errors.Is(err, githubapi.ErrGitHubNotFound) {
			return Question{}, false, nil
		}
		return Question{}, false, err
	}
	if release.Tag == "" {
		return Question{}, false, nil
	}
	payload, _ := json.Marshal(map[string]string{"type": "latest_release"})
	return Question{
		Prompt:  utils.FormatMarkdown(i18n, "question.latest_release", map[string]string{"repo": utils.FormatRepoLink(cfg.Repo)}),
		Answer:  release.Tag,
		Type:    "latest_release",
		Payload: string(payload),
	}, true, nil
}
