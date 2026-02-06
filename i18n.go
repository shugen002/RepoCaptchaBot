package main

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/ini.v1"
)

const defaultLang = "zh-CN"

type I18n struct {
	lang     string
	messages map[string]string
}

func LoadI18n(lang string) (*I18n, error) {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		lang = defaultLang
	}

	fallback, err := loadIniFile(filepath.Join("i18n", defaultLang+".ini"))
	if err != nil {
		return nil, err
	}

	messages := make(map[string]string, len(fallback))
	for k, v := range fallback {
		messages[k] = v
	}

	if lang != defaultLang {
		if langMap, err := loadIniFile(filepath.Join("i18n", lang+".ini")); err == nil {
			for k, v := range langMap {
				messages[k] = v
			}
		}
	}

	return &I18n{lang: lang, messages: messages}, nil
}

func (i *I18n) Raw(key string) string {
	if i == nil {
		return key
	}
	text, ok := i.messages[key]
	if !ok || text == "" {
		return key
	}
	return text
}

func loadIniFile(path string) (map[string]string, error) {
	cfg, err := ini.Load(path)
	if err != nil {
		return nil, err
	}
	section := cfg.Section("")
	keys := section.Keys()
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		out[key.Name()] = key.Value()
	}
	return out, nil
}

func NormalizeLang(lang string) string {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return ""
	}
	lang = strings.ReplaceAll(lang, "_", "-")
	low := strings.ToLower(lang)
	if strings.HasPrefix(low, "zh") {
		if strings.Contains(low, "tw") || strings.Contains(low, "hk") || strings.Contains(low, "hant") {
			return "zh-TW"
		}
		return "zh-CN"
	}
	if strings.HasPrefix(low, "en") {
		return "en"
	}
	return lang
}

func IsLangAvailable(lang string) bool {
	lang = NormalizeLang(lang)
	if lang == "" {
		return false
	}
	path := filepath.Join("i18n", lang+".ini")
	if _, err := os.Stat(path); err == nil {
		return true
	}
	return false
}
