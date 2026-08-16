package adapters

import (
	"sort"
	"unicode/utf8"
)

func clampHistoryLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 40 {
		return 40
	}
	return limit
}

func truncateRunes(s string, max int) string {
	if max <= 0 || s == "" {
		return s
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	if len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

func takeLastHistory(msgs []*HistoryMessage, limit int) []*HistoryMessage {
	limit = clampHistoryLimit(limit)
	if len(msgs) <= limit {
		return msgs
	}
	return msgs[len(msgs)-limit:]
}

func sortHistoryMessages(messages []*HistoryMessage) {
	sort.SliceStable(messages, func(i, j int) bool {
		return messages[i].Timestamp < messages[j].Timestamp
	})
}
