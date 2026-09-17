package slack

import (
	"regexp"
	"strings"
)

// cellInlineRe matches the inline Markdown styles worth preserving inside a
// table cell: **bold**, ~~strike~~, [text](url), and *italic* (checked last
// so it doesn't shadow **bold**).
var cellInlineRe = regexp.MustCompile(`\*\*([^*]+)\*\*|~~([^~]+)~~|\[([^\]]+)\]\(([^)]+)\)|\*([^*]+)\*`)

const (
	maxTableRows      = 100
	maxTableCells     = 20
	maxTableCellChars = 10000
)

// buildTableBlock converts a markdown table into a native Slack Block Kit
// "table" block (https://docs.slack.dev/reference/block-kit/blocks/table-block/).
// It returns ok=false if the table doesn't fit Slack's limits, so the caller
// can fall back to the text-rendered representation.
func buildTableBlock(tableStr string) (map[string]any, bool) {
	lines := strings.Split(strings.TrimSpace(tableStr), "\n")
	if len(lines) < 2 {
		return nil, false
	}

	var allRows [][]string
	maxCols := 0
	for i, line := range lines {
		if i == 1 && isSeparatorRow(line) {
			continue
		}
		cells := parseTableRow(line)
		if len(cells) == 0 {
			continue
		}
		allRows = append(allRows, cells)
		if len(cells) > maxCols {
			maxCols = len(cells)
		}
	}

	if len(allRows) == 0 || len(allRows) > maxTableRows || maxCols == 0 || maxCols > maxTableCells {
		return nil, false
	}

	totalChars := 0
	rows := make([][]map[string]any, len(allRows))
	for i, cells := range allRows {
		row := make([]map[string]any, len(cells))
		for j, cell := range cells {
			totalChars += len(cell)
			if i == 0 {
				row[j] = boldRichTextCell(cell)
			} else {
				row[j] = richTextCell(cell)
			}
		}
		rows[i] = row
	}

	if totalChars > maxTableCellChars {
		return nil, false
	}

	columnSettings := make([]map[string]any, maxCols)
	for i := range columnSettings {
		columnSettings[i] = map[string]any{"align": "left", "is_wrapped": true}
	}

	return map[string]any{
		"type":            "table",
		"rows":            rows,
		"column_settings": columnSettings,
	}, true
}

// richTextCell renders a table cell as a rich_text block, preserving inline
// Markdown styling (bold, italic, strikethrough, links) rather than showing
// the raw Markdown syntax the way a raw_text cell would.
func richTextCell(text string) map[string]any {
	return map[string]any{
		"type": "rich_text",
		"elements": []map[string]any{
			{
				"type":     "rich_text_section",
				"elements": parseCellInline(text),
			},
		},
	}
}

func boldRichTextCell(text string) map[string]any {
	return map[string]any{
		"type": "rich_text",
		"elements": []map[string]any{
			{
				"type": "rich_text_section",
				"elements": []map[string]any{
					{
						"type":  "text",
						"text":  text,
						"style": map[string]any{"bold": true},
					},
				},
			},
		},
	}
}

// parseCellInline splits cell text into rich_text elements, converting
// **bold**, ~~strike~~, [text](url), and *italic* spans into their rich_text
// equivalents and leaving everything else as plain text.
func parseCellInline(text string) []map[string]any {
	matches := cellInlineRe.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return []map[string]any{plainTextElement(text)}
	}

	var elements []map[string]any
	lastEnd := 0
	for _, m := range matches {
		if m[0] > lastEnd {
			elements = append(elements, plainTextElement(text[lastEnd:m[0]]))
		}
		switch {
		case m[2] >= 0:
			elements = append(elements, styledTextElement(text[m[2]:m[3]], "bold"))
		case m[4] >= 0:
			elements = append(elements, styledTextElement(text[m[4]:m[5]], "strike"))
		case m[6] >= 0:
			elements = append(elements, linkElement(text[m[6]:m[7]], text[m[8]:m[9]]))
		case m[10] >= 0:
			elements = append(elements, styledTextElement(text[m[10]:m[11]], "italic"))
		}
		lastEnd = m[1]
	}
	if lastEnd < len(text) {
		elements = append(elements, plainTextElement(text[lastEnd:]))
	}
	return elements
}

func plainTextElement(text string) map[string]any {
	return map[string]any{"type": "text", "text": text}
}

func styledTextElement(text, style string) map[string]any {
	return map[string]any{
		"type":  "text",
		"text":  text,
		"style": map[string]any{style: true},
	}
}

func linkElement(text, url string) map[string]any {
	return map[string]any{"type": "link", "text": text, "url": url}
}
