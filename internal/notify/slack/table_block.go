package slack

import "strings"

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
				row[j] = rawTextCell(cell)
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

func rawTextCell(text string) map[string]any {
	return map[string]any{
		"type": "raw_text",
		"text": text,
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
