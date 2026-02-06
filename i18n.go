package main

import (
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

func (i *I18n) T(key string, vars map[string]string) string {
	if i == nil {
		return renderTemplate(key, vars)
	}
	text, ok := i.messages[key]
	if !ok || text == "" {
		text = key
	}
	return renderTemplate(text, vars)
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

func renderTemplate(text string, vars map[string]string) string {
	if len(vars) == 0 {
		return text
	}
	for k, v := range vars {
		text = strings.ReplaceAll(text, "{"+k+"}", v)
	}
	return text
}
