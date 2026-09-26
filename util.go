package main

import "strings"

func compactBytes(raw []byte, limit int) string {
	text := strings.TrimSpace(string(raw))
	if len(text) > limit {
		return text[:limit] + "..."
	}
	return text
}
