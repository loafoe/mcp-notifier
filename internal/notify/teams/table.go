package teams

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/atc0005/go-teams-notify/v2/adaptivecard"
)

// markdownTableRe matches a markdown table block (header + separator + rows).
var markdownTableRe = regexp.MustCompile(`(?m)^(\|[^\n]+\|)\n(\|[-:\|\s]+\|)\n((?:\|[^\n]+\|\n?)+)`)

// contentSegment represents either a text block or a table in the message content.
type contentSegment struct {
	content string
	isTable bool
}

// splitContentWithTables splits content into alternating text and table segments.
func splitContentWithTables(content string) []contentSegment {
	var segments []contentSegment

	matches := markdownTableRe.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return []contentSegment{{content: content, isTable: false}}
	}

	lastEnd := 0
	for _, match := range matches {
		if match[0] > lastEnd {
			segments = append(segments, contentSegment{
				content: content[lastEnd:match[0]],
				isTable: false,
			})
		}
		segments = append(segments, contentSegment{
			content: content[match[0]:match[1]],
			isTable: true,
		})
		lastEnd = match[1]
	}

	if lastEnd < len(content) {
		segments = append(segments, contentSegment{
			content: content[lastEnd:],
			isTable: false,
		})
	}

	return segments
}

// parseMarkdownTable converts a markdown table string to an Adaptive Card Table element.
func parseMarkdownTable(tableStr string) (adaptivecard.Element, error) {
	lines := strings.Split(strings.TrimSpace(tableStr), "\n")
	if len(lines) < 2 {
		return adaptivecard.Element{}, fmt.Errorf("table must have at least header and separator rows")
	}

	var headerLengths []int
	var allRows [][]adaptivecard.TableCell
	for i, line := range lines {
		if i == 1 && isSeparatorRow(line) {
			continue
		}

		cells := parseTableRow(line)
		if len(cells) == 0 {
			continue
		}

		var tableCells []adaptivecard.TableCell
		for _, cellText := range cells {
			trimmedText := strings.TrimSpace(cellText)

			if i == 0 {
				headerLengths = append(headerLengths, len(trimmedText))
			}

			textBlock := adaptivecard.Element{
				Type: adaptivecard.TypeElementTextBlock,
				Text: trimmedText,
				Wrap: true,
			}
			cell := adaptivecard.TableCell{
				Type:  adaptivecard.TypeTableCell,
				Items: []*adaptivecard.Element{&textBlock},
			}
			tableCells = append(tableCells, cell)
		}
		allRows = append(allRows, tableCells)
	}

	if len(allRows) == 0 {
		return adaptivecard.Element{}, fmt.Errorf("no valid rows found in table")
	}

	firstRowAsHeaders := true
	showGridLines := true

	table, err := adaptivecard.NewTableFromTableCells(allRows, 0, firstRowAsHeaders, showGridLines)
	if err != nil {
		return adaptivecard.Element{}, fmt.Errorf("failed to create table: %w", err)
	}

	table.Columns = calculateColumnWidths(headerLengths)

	return table, nil
}

// calculateColumnWidths creates TableColumnDefinition entries with widths
// proportional to the max content length of each column.
func calculateColumnWidths(maxLengths []int) []adaptivecard.Column {
	if len(maxLengths) == 0 {
		return nil
	}

	columns := make([]adaptivecard.Column, len(maxLengths))
	for i, length := range maxLengths {
		weight := length
		if weight < 1 {
			weight = 1
		}
		columns[i] = adaptivecard.Column{
			Type:  "TableColumnDefinition",
			Width: weight,
		}
	}

	return columns
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
