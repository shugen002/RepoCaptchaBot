package verifier

import (
	"context"
	"strconv"
	"strings"
	"unicode"

	"github.com/shugen002/RepoCaptchaBot/models"
	"github.com/shugen002/RepoCaptchaBot/utils"
)

func (v *Verifier) questionFileLine(ctx context.Context, cfg models.ChatConfig, i18n *utils.I18n) (Question, bool, error) {
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
		content, err := getFileContent(ctx, v.gh, cfg.Repo, path)
		if err != nil {
			lastErr = err
			continue
		}
		candidates := extractValidLines(content)
		if len(candidates) == 0 {
			continue
		}
		picked := candidates[v.rnd.IntN(len(candidates))]
		return Question{
			Generator: "file_line",
			Type:      "file_line",
			Question:  utils.FormatMarkdown(i18n, "question.file_line", map[string]string{"repo": utils.FormatRepoLink(cfg.Repo), "path": utils.FormatCode(path), "line": utils.FormatCode(strconv.Itoa(picked.Line))}),
			Data: map[string]interface{}{
				"answer":      strings.TrimSpace(picked.Text),
				"path":        path,
				"line":        picked.Line,
				"commit_hash": commitHash,
			},
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
	commitHash, err := getLatestCommitSHA(ctx, v.gh, repo)
	if err != nil {
		return nil, "", err
	}

	files, err := getRepoFileList(ctx, v.gh, repo, commitHash, 2)
	if err != nil {
		return nil, "", err
	}

	return files, commitHash, nil
}
