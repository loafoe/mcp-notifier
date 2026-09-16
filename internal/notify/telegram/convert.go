package telegram

import (
	"fmt"
	"html"
	"regexp"
	"strings"
)

// maxMessageLength is Telegram's sendMessage text limit, in UTF-16 code
// units per Telegram's docs; we approximate conservatively using runes.
const maxMessageLength = 4096

var (
	reHeading    = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)
	reBlockquote = regexp.MustCompile(`(?m)^>\s*(.*)$`)
	reLink       = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	reBoldStar   = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reBoldUnder  = regexp.MustCompile(`__(.+?)__`)
	reItalic     = regexp.MustCompile(`(?:^|[^*])\*([^*]+)\*(?:[^*]|$)`)
	reStrike     = regexp.MustCompile(`~~(.+?)~~`)
	reListItem   = regexp.MustCompile(`(?m)^[-*]\s+`)
	reCodeBlock  = regexp.MustCompile("(?s)```[\\w]*\\n?(.*?)```")
	reInlineCode = regexp.MustCompile("`([^`]+)`")
	reRawURL     = regexp.MustCompile(`https?://[^\s<]+`)

	markdownTableRe = regexp.MustCompile(`(?m)^(\|[^\n]+\|)\n(\|[-:\|\s]+\|)\n((?:\|[^\n]+\|\n?)+)`)
	reHTMLTag       = regexp.MustCompile(`<[^>]+>`)
)

// contentSegment represents either a text block or a table in the message content.
type contentSegment struct {
	content string
	isTable bool
}

// messageChunk is a single sendMessage-sized piece of a message, kept in
// both HTML and plain-text form so the caller can fall back to plain text
// if Telegram rejects the HTML entities.
type messageChunk struct {
	html  string
	plain string
}

// buildChunks converts Markdown content into one or more Telegram-ready
// chunks, each within Telegram's message length limit.
func buildChunks(content string) []messageChunk {
	var htmlParts []string
	for _, seg := range splitContentWithTables(content) {
		if seg.isTable {
			htmlParts = append(htmlParts, renderTableHTML(seg.content))
			continue
		}
		text := strings.TrimSpace(seg.content)
		if text == "" {
			continue
		}
		htmlParts = append(htmlParts, markdownToTelegramHTML(text))
	}

	full := strings.Join(htmlParts, "\n\n")
	if full == "" {
		full = html.EscapeString(content)
	}

	parts := splitHTML(full, maxMessageLength)
	if len(parts) == 0 {
		parts = []string{full}
	}

	chunks := make([]messageChunk, len(parts))
	for i, part := range parts {
		chunks[i] = messageChunk{html: part, plain: stripHTMLTags(part)}
	}
	return chunks
}

// stripHTMLTags produces a plain-text fallback from an already-built HTML
// chunk: it removes tags and unescapes entities, so it always corresponds
// exactly to the HTML it's a fallback for (used if Telegram rejects the
// HTML entities).
func stripHTMLTags(s string) string {
	return html.UnescapeString(reHTMLTag.ReplaceAllString(s, ""))
}

// splitHTML breaks HTML text into chunks no longer than maxLen runes,
// preferring to split on newlines so words and tags aren't cut mid-way, and
// re-wrapping any <pre><code> block that straddles a chunk boundary so each
// chunk stays valid HTML.
func splitHTML(text string, maxLen int) []string {
	runes := []rune(text)
	if len(runes) <= maxLen {
		return []string{text}
	}

	const preOpen = "<pre><code>"
	const preClose = "</code></pre>"
	preOpenLen := len([]rune(preOpen))
	preCloseLen := len([]rune(preClose))

	var chunks []string
	inPre := false

	for len(runes) > 0 {
		prefixLen := 0
		if inPre {
			prefixLen = preOpenLen
		}
		budget := maxLen - prefixLen - preCloseLen
		if budget <= 0 {
			budget = maxLen
		}

		splitAt := len(runes)
		if splitAt > budget {
			splitAt = findSplitPoint(runes, budget)
			if splitAt <= 0 || splitAt > budget {
				splitAt = budget
			}
		}

		body := string(runes[:splitAt])
		endsInPre := inPre != (strings.Count(body, preOpen) != strings.Count(body, preClose))

		if inPre && !strings.HasPrefix(strings.TrimSpace(body), "<pre>") {
			body = preOpen + body
		}
		if endsInPre {
			body = strings.TrimSuffix(body, "\n") + preClose
		}

		chunks = append(chunks, body)
		inPre = endsInPre
		runes = runes[splitAt:]
	}

	return chunks
}

func findSplitPoint(runes []rune, maxLen int) int {
	if len(runes) <= maxLen {
		return len(runes)
	}
	window := string(runes[:maxLen])

	if idx := strings.LastIndex(window, "\n"); idx > 0 {
		return len([]rune(window[:idx])) + 1
	}
	if idx := strings.LastIndex(window, " "); idx > 0 {
		return len([]rune(window[:idx])) + 1
	}
	return maxLen
}

// markdownToTelegramHTML converts common Markdown syntax to Telegram's HTML
// message format. Adapted from picoclaw's telegram channel.
func markdownToTelegramHTML(text string) string {
	if text == "" {
		return ""
	}

	codeBlocks := extractCodeBlocks(text)
	text = codeBlocks.text

	inlineCodes := extractInlineCodes(text)
	text = inlineCodes.text

	links := extractLinks(text)
	text = links.text

	rawURLs := extractRawURLs(text)
	text = rawURLs.text

	text = reHeading.ReplaceAllString(text, "$1")
	text = reBlockquote.ReplaceAllString(text, "$1")

	text = html.EscapeString(text)

	text = reBoldStar.ReplaceAllString(text, "<b>$1</b>")
	text = reBoldUnder.ReplaceAllString(text, "<b>$1</b>")

	text = reItalic.ReplaceAllStringFunc(text, func(s string) string {
		match := reItalic.FindStringSubmatch(s)
		if len(match) < 2 {
			return s
		}
		return "<i>" + match[1] + "</i>"
	})

	text = reStrike.ReplaceAllString(text, "<s>$1</s>")
	text = reListItem.ReplaceAllString(text, "• ")

	for i, lnk := range links.links {
		label := html.EscapeString(lnk[0])
		url := html.EscapeString(lnk[1])
		text = strings.ReplaceAll(text, fmt.Sprintf("\x00LK%d\x00", i), fmt.Sprintf(`<a href="%s">%s</a>`, url, label))
	}
	for i, rawURL := range rawURLs.urls {
		escaped := html.EscapeString(rawURL)
		text = strings.ReplaceAll(text, fmt.Sprintf("\x00RU%d\x00", i), fmt.Sprintf(`<a href="%s">%s</a>`, escaped, escaped))
	}
	for i, code := range inlineCodes.codes {
		text = strings.ReplaceAll(text, fmt.Sprintf("\x00IC%d\x00", i), fmt.Sprintf("<code>%s</code>", html.EscapeString(code)))
	}
	for i, code := range codeBlocks.codes {
		text = strings.ReplaceAll(text, fmt.Sprintf("\x00CB%d\x00", i), fmt.Sprintf("<pre><code>%s</code></pre>", html.EscapeString(code)))
	}

	return text
}

type linkMatch struct {
	text  string
	links [][2]string
}

func extractLinks(text string) linkMatch {
	matches := reLink.FindAllStringSubmatch(text, -1)
	extracted := make([][2]string, 0, len(matches))
	for _, match := range matches {
		extracted = append(extracted, [2]string{match[1], match[2]})
	}
	i := 0
	text = reLink.ReplaceAllStringFunc(text, func(string) string {
		placeholder := fmt.Sprintf("\x00LK%d\x00", i)
		i++
		return placeholder
	})
	return linkMatch{text: text, links: extracted}
}

type codeBlockMatch struct {
	text  string
	codes []string
}

func extractCodeBlocks(text string) codeBlockMatch {
	matches := reCodeBlock.FindAllStringSubmatch(text, -1)
	codes := make([]string, 0, len(matches))
	for _, match := range matches {
		codes = append(codes, match[1])
	}
	i := 0
	text = reCodeBlock.ReplaceAllStringFunc(text, func(string) string {
		placeholder := fmt.Sprintf("\x00CB%d\x00", i)
		i++
		return placeholder
	})
	return codeBlockMatch{text: text, codes: codes}
}

type rawURLMatch struct {
	text string
	urls []string
}

func extractRawURLs(text string) rawURLMatch {
	matches := reRawURL.FindAllString(text, -1)
	urls := make([]string, 0, len(matches))
	urls = append(urls, matches...)
	i := 0
	text = reRawURL.ReplaceAllStringFunc(text, func(string) string {
		placeholder := fmt.Sprintf("\x00RU%d\x00", i)
		i++
		return placeholder
	})
	return rawURLMatch{text: text, urls: urls}
}

type inlineCodeMatch struct {
	text  string
	codes []string
}

func extractInlineCodes(text string) inlineCodeMatch {
	matches := reInlineCode.FindAllStringSubmatch(text, -1)
	codes := make([]string, 0, len(matches))
	for _, match := range matches {
		codes = append(codes, match[1])
	}
	i := 0
	text = reInlineCode.ReplaceAllStringFunc(text, func(string) string {
		placeholder := fmt.Sprintf("\x00IC%d\x00", i)
		i++
		return placeholder
	})
	return inlineCodeMatch{text: text, codes: codes}
}

// splitContentWithTables splits content into alternating text and table segments.
func splitContentWithTables(content string) []contentSegment {
	var segments []contentSegment

	var codeBlocks []string
	blockIdx := 0
	protected := reCodeBlock.ReplaceAllStringFunc(content, func(match string) string {
		codeBlocks = append(codeBlocks, match)
		placeholder := fmt.Sprintf("\x00CODEBLOCK_%d\x00", blockIdx)
		blockIdx++
		return placeholder
	})

	matches := markdownTableRe.FindAllStringSubmatchIndex(protected, -1)
	if len(matches) == 0 {
		return []contentSegment{{content: content, isTable: false}}
	}

	restore := func(s string) string {
		for i, block := range codeBlocks {
			placeholder := fmt.Sprintf("\x00CODEBLOCK_%d\x00", i)
			s = strings.Replace(s, placeholder, block, 1)
		}
		return s
	}

	lastEnd := 0
	for _, match := range matches {
		if match[0] > lastEnd {
			segments = append(segments, contentSegment{content: restore(protected[lastEnd:match[0]]), isTable: false})
		}
		segments = append(segments, contentSegment{content: restore(protected[match[0]:match[1]]), isTable: true})
		lastEnd = match[1]
	}
	if lastEnd < len(protected) {
		segments = append(segments, contentSegment{content: restore(protected[lastEnd:]), isTable: false})
	}

	return segments
}

// renderTableHTML converts a markdown table into an HTML-escaped, aligned
// monospace table wrapped in <pre>.
func renderTableHTML(tableStr string) string {
	lines := strings.Split(strings.TrimSpace(tableStr), "\n")
	if len(lines) < 2 {
		return "<pre>" + html.EscapeString(tableStr) + "</pre>"
	}

	var allRows [][]string
	maxCols := 0
	for i, line := range lines {
		if i == 1 && isSeparatorRow(line) {
			continue
		}
		cells := parseTableRow(line)
		if len(cells) > 0 {
			allRows = append(allRows, cells)
			if len(cells) > maxCols {
				maxCols = len(cells)
			}
		}
	}
	if len(allRows) == 0 {
		return "<pre>" + html.EscapeString(tableStr) + "</pre>"
	}

	colWidths := make([]int, maxCols)
	for _, row := range allRows {
		for i, cell := range row {
			if runeLen := len([]rune(cell)); runeLen > colWidths[i] {
				colWidths[i] = runeLen
			}
		}
	}

	var result strings.Builder
	for i, row := range allRows {
		var padded []string
		for j, cell := range row {
			if j < len(colWidths) {
				padded = append(padded, padRight(cell, colWidths[j]))
			} else {
				padded = append(padded, cell)
			}
		}
		result.WriteString(strings.Join(padded, " | "))
		result.WriteString("\n")

		if i == 0 {
			var sepParts []string
			for _, w := range colWidths {
				sepParts = append(sepParts, strings.Repeat("-", w))
			}
			result.WriteString(strings.Join(sepParts, "-|-"))
			result.WriteString("\n")
		}
	}

	return "<pre>" + html.EscapeString(strings.TrimSuffix(result.String(), "\n")) + "</pre>"
}

func padRight(s string, width int) string {
	runeLen := len([]rune(s))
	if runeLen >= width {
		return s
	}
	return s + strings.Repeat(" ", width-runeLen)
}

func isSeparatorRow(line string) bool {
	cleaned := strings.ReplaceAll(line, "|", "")
	cleaned = strings.ReplaceAll(cleaned, " ", "")
	cleaned = strings.ReplaceAll(cleaned, "-", "")
	cleaned = strings.ReplaceAll(cleaned, ":", "")
	return cleaned == ""
}

func parseTableRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	if line == "" {
		return nil
	}
	parts := strings.Split(line, "|")
	var cells []string
	for _, p := range parts {
		cells = append(cells, strings.TrimSpace(p))
	}
	return cells
}
