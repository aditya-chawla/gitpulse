package llm

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	reImage   = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	reLink    = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	reAutoURL = regexp.MustCompile(`<(https?://[^>]*)>`)
	reHTML    = regexp.MustCompile(`<[^>]*>`)
	reMention = regexp.MustCompile(`(^|[^\w@])@([A-Za-z0-9][A-Za-z0-9-]*(/[A-Za-z0-9._-]+)?)`)
	reSpaces  = regexp.MustCompile(`[ \t]+`)
	reBlank   = regexp.MustCompile(`\n{3,}`)
)

// SanitizeText makes model output safe to display or post as GitHub markdown.
// The model's input is untrusted (PR text, diffs, commit messages), so its
// output may echo hostile content. This:
//   - drops images and HTML (incl. comments, so the gitpulse marker can't be forged),
//   - turns markdown links into their text (no attacker-chosen URLs),
//   - neutralises @mentions and team mentions so nobody gets pinged,
//   - removes control characters, collapses whitespace, and caps the length.
func SanitizeText(s string, maxRunes int) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) || r == '‎' || r == '‏' || (r >= '‪' && r <= '‮') || (r >= '⁦' && r <= '⁩') {
			return -1 // incl. bidi overrides that can disguise text
		}
		return r
	}, s)
	s = reImage.ReplaceAllString(s, "")
	s = reLink.ReplaceAllString(s, "$1")
	s = reAutoURL.ReplaceAllString(s, "$1")
	s = reHTML.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "<", "‹") // any leftover '<' (e.g. "<!--" without '>')
	s = reMention.ReplaceAllString(s, "$1@​$2")
	s = reSpaces.ReplaceAllString(s, " ")
	s = reBlank.ReplaceAllString(s, "\n\n")
	s = strings.TrimSpace(s)
	if maxRunes > 0 {
		if r := []rune(s); len(r) > maxRunes {
			s = strings.TrimSpace(string(r[:maxRunes-1])) + "…"
		}
	}
	return s
}

// SanitizeLine is SanitizeText for single-line fields such as titles.
func SanitizeLine(s string, maxRunes int) string {
	return SanitizeText(strings.Join(strings.Fields(s), " "), maxRunes)
}
