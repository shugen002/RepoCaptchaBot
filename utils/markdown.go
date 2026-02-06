package utils

import (
	"fmt"
	"strings"
)

func EscapeMarkdownV2(text string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"_", "\\_",
		"*", "\\*",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"~", "\\~",
		"`", "\\`",
		">", "\\>",
		"#", "\\#",
		"+", "\\+",
		"-", "\\-",
		"=", "\\=",
		"|", "\\|",
		"{", "\\{",
		"}", "\\}",
		".", "\\.",
		"!", "\\!",
	)
	return replacer.Replace(text)
}

func FormatMarkdown(i18n *I18n, key string, vars map[string]string) string {
	template := ""
	if i18n != nil {
		template = i18n.Raw(key)
	}
	if template == "" {
		template = key
	}
	template = strings.ReplaceAll(template, `\n`, "\n")

	if len(vars) == 0 {
		return EscapeMarkdownV2(template)
	}
	tokens := make(map[string]string, len(vars))
	idx := 0
	for k, v := range vars {
		token := fmt.Sprintf("@@VAR%d@@", idx)
		idx++
		tokens[token] = v
		template = strings.ReplaceAll(template, "{"+k+"}", token)
	}
	escaped := EscapeMarkdownV2(template)
	for token, v := range tokens {
		escaped = strings.ReplaceAll(escaped, token, v)
	}
	return escaped
}

func FormatCode(value string) string {
	return "`" + EscapeMarkdownV2(value) + "`"
}

func FormatRepoLink(repo string) string {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return FormatCode("-")
	}
	text := EscapeMarkdownV2(repo)
	url := EscapeMarkdownV2("https://github.com/" + repo)
	return "[" + text + "](" + url + ")"
}
