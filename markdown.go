package main

import (
	"fmt"
	"strings"
)

func escapeMarkdownV2(text string) string {
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

func formatMarkdown(i18n *I18n, key string, vars map[string]string) string {
	template := ""
	if i18n != nil {
		template = i18n.Raw(key)
	}
	if template == "" {
		template = key
	}
	template = strings.ReplaceAll(template, `\n`, "\n")

	if len(vars) == 0 {
		return escapeMarkdownV2(template)
	}
	tokens := make(map[string]string, len(vars))
	idx := 0
	for k, v := range vars {
		token := fmt.Sprintf("@@VAR%d@@", idx)
		idx++
		tokens[token] = v
		template = strings.ReplaceAll(template, "{"+k+"}", token)
	}
	escaped := escapeMarkdownV2(template)
	for token, v := range tokens {
		escaped = strings.ReplaceAll(escaped, token, v)
	}
	return escaped
}

func formatCode(value string) string {
	return "`" + escapeMarkdownV2(value) + "`"
}

func formatRepoLink(repo string) string {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return formatCode("-")
	}
	text := escapeMarkdownV2(repo)
	url := escapeMarkdownV2("https://github.com/" + repo)
	return "[" + text + "](" + url + ")"
}
