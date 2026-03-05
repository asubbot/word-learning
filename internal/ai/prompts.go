package ai

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var languageCodePattern = regexp.MustCompile(`^[A-Za-z]{2,8}$`)

// LanguagePair represents a from-to language pair (e.g. EN->RU).
type LanguagePair struct {
	From string
	To   string
}

// ListAvailableLanguagePairs scans prompt_xx-yy.txt files in promptsDir and returns
// valid language pairs. Returns empty slice if dir is missing or invalid (no error).
func ListAvailableLanguagePairs(promptsDir string) []LanguagePair {
	promptsDir = strings.TrimSpace(promptsDir)
	if promptsDir == "" {
		return nil
	}
	matches, err := filepath.Glob(filepath.Join(promptsDir, "prompt_*.txt"))
	if err != nil {
		return nil
	}
	var pairs []LanguagePair
	seen := make(map[string]struct{})
	for _, m := range matches {
		base := filepath.Base(m)
		if !strings.HasPrefix(base, "prompt_") || !strings.HasSuffix(base, ".txt") {
			continue
		}
		inner := strings.TrimSuffix(strings.TrimPrefix(base, "prompt_"), ".txt")
		parts := strings.SplitN(inner, "-", 2)
		if len(parts) != 2 {
			continue
		}
		from := strings.ToUpper(strings.TrimSpace(parts[0]))
		to := strings.ToUpper(strings.TrimSpace(parts[1]))
		if !languageCodePattern.MatchString(from) || !languageCodePattern.MatchString(to) {
			continue
		}
		key := from + "-" + to
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		pairs = append(pairs, LanguagePair{From: from, To: to})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].From != pairs[j].From {
			return pairs[i].From < pairs[j].From
		}
		return pairs[i].To < pairs[j].To
	})
	return pairs
}
