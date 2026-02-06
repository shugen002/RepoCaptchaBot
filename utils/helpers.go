package utils

import (
	"strconv"
	"strings"
	"time"
)

func NormalizeAnswer(text string) string {
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "\u00a0", " ")
	text = strings.ToLower(text)
	return strings.TrimSpace(text)
}

func FormatDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	if d%time.Hour == 0 {
		return strconv.FormatInt(int64(d/time.Hour), 10) + "h"
	}
	if d%time.Minute == 0 {
		return strconv.FormatInt(int64(d/time.Minute), 10) + "m"
	}
	if d%time.Second == 0 {
		return strconv.FormatInt(int64(d/time.Second), 10) + "s"
	}
	return d.String()
}

func FormatTypeList(types []string) string {
	if len(types) == 0 {
		return FormatCode("-")
	}
	formatted := make([]string, 0, len(types))
	for _, t := range types {
		formatted = append(formatted, FormatCode(t))
	}
	return strings.Join(formatted, ", ")
}
