package config

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"unicode"
)

const expectedTableInstruction = "Do not create tables unnecessarily. Never output Markdown pipe tables, as Telegram does not format them as tables. When data genuinely requires columns, output a short, compact Markdown table inside a fenced code block with short headers, short cells, and evenly aligned monospace columns. Do not put long explanations or full sentences into cells; put details below the table as separate text. For wide tables or long values that do not fit a narrow screen, use a concise list or cards instead of a table."

func TestWorkerAndSecretaryShareIdenticalCompactTableInstructions(t *testing.T) {
	workerBytes, err := fs.ReadFile(defaults, "defaults/worker.md")
	if err != nil {
		t.Fatalf("read default worker profile: %v", err)
	}
	workerPrompt := string(workerBytes)

	secretaryBytes, err := fs.ReadFile(defaults, "defaults/secretary.md")
	if err != nil {
		t.Fatalf("read default secretary profile: %v", err)
	}
	secretaryPrompt := string(secretaryBytes)

	if !strings.Contains(workerPrompt, expectedTableInstruction) {
		t.Fatalf("worker.md missing expected table instruction:\nwant: %s\ngot: %s", expectedTableInstruction, workerPrompt)
	}

	if !strings.Contains(secretaryPrompt, expectedTableInstruction) {
		t.Fatalf("secretary.md missing expected table instruction:\nwant: %s\ngot: %s", expectedTableInstruction, secretaryPrompt)
	}

	// Verify key constraints required by issue #5
	for _, requiredPhrase := range []string{
		"Do not create tables unnecessarily",
		"Never output Markdown pipe tables",
		"fenced code block",
		"short headers, short cells, and evenly aligned monospace columns",
		"Do not put long explanations or full sentences into cells",
		"put details below the table as separate text",
		"For wide tables or long values that do not fit a narrow screen, use a concise list or cards instead of a table",
	} {
		if !strings.Contains(workerPrompt, requiredPhrase) {
			t.Errorf("worker.md missing required rule phrase: %q", requiredPhrase)
		}
		if !strings.Contains(secretaryPrompt, requiredPhrase) {
			t.Errorf("secretary.md missing required rule phrase: %q", requiredPhrase)
		}
	}
}

type tableColumn struct {
	start int
	name  string
}

func parseMonospaceTable(text string, maxCellRunes, maxLineRunes int) error {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "```") {
		return errors.New("table must start with a fenced code block (```)")
	}

	startFence := strings.Index(trimmed, "```")
	rest := trimmed[startFence+3:]
	newlineIdx := strings.Index(rest, "\n")
	if newlineIdx == -1 {
		return errors.New("fenced code block not closed")
	}
	bodyAndAfter := rest[newlineIdx+1:]

	endFence := strings.Index(bodyAndAfter, "```")
	if endFence == -1 {
		return errors.New("fenced code block missing closing ```")
	}

	tableBody := bodyAndAfter[:endFence]
	postFence := strings.TrimSpace(bodyAndAfter[endFence+3:])

	if strings.Contains(tableBody, "|") {
		return errors.New("table must not contain Markdown pipe table characters ('|')")
	}

	lines := strings.Split(strings.Trim(tableBody, "\n"), "\n")
	if len(lines) < 2 {
		return errors.New("table must contain at least a header row and one data row")
	}

	headerRunes := []rune(lines[0])
	if len(headerRunes) > maxLineRunes {
		return fmt.Errorf("header row exceeds max line length (%d > %d runes)", len(headerRunes), maxLineRunes)
	}

	var cols []tableColumn
	inWord := false
	spaceCount := 0
	tokenEnd := 0

	for i, r := range headerRunes {
		if unicode.IsSpace(r) {
			if inWord {
				inWord = false
				tokenEnd = i
			}
			spaceCount++
		} else {
			if !inWord {
				if i == 0 || spaceCount >= 2 {
					if len(cols) > 0 {
						cols[len(cols)-1].name = strings.TrimSpace(string(headerRunes[cols[len(cols)-1].start:tokenEnd]))
					}
					cols = append(cols, tableColumn{start: i})
				}
				inWord = true
			}
			spaceCount = 0
		}
	}
	if inWord {
		tokenEnd = len(headerRunes)
	}
	if len(cols) > 0 {
		cols[len(cols)-1].name = strings.TrimSpace(string(headerRunes[cols[len(cols)-1].start:tokenEnd]))
	}
	if len(cols) < 2 {
		return fmt.Errorf("table must have at least 2 columns, found %d", len(cols))
	}

	for _, col := range cols {
		if len([]rune(col.name)) > maxCellRunes {
			return fmt.Errorf("header column %q exceeds max cell length (%d > %d runes)", col.name, len([]rune(col.name)), maxCellRunes)
		}
	}

	for rowIdx := 1; rowIdx < len(lines); rowIdx++ {
		rowRunes := []rune(lines[rowIdx])
		if len(rowRunes) > maxLineRunes {
			return fmt.Errorf("row %d exceeds max line length (%d > %d runes)", rowIdx, len(rowRunes), maxLineRunes)
		}

		for cIdx, col := range cols {
			if len(rowRunes) <= col.start || unicode.IsSpace(rowRunes[col.start]) {
				return fmt.Errorf("row %d column %d (%s) is misaligned: expected non-space content at offset %d", rowIdx, cIdx+1, col.name, col.start)
			}

			if cIdx > 0 {
				if col.start < 2 || !unicode.IsSpace(rowRunes[col.start-1]) || !unicode.IsSpace(rowRunes[col.start-2]) {
					return fmt.Errorf("row %d column %d (%s) has insufficient gutter: expected at least 2 spaces before offset %d", rowIdx, cIdx+1, col.name, col.start)
				}
			}

			var cellText string
			if cIdx < len(cols)-1 {
				nextStart := cols[cIdx+1].start
				if len(rowRunes) < nextStart {
					cellText = strings.TrimRight(string(rowRunes[col.start:]), " ")
				} else {
					cellText = strings.TrimRight(string(rowRunes[col.start:nextStart]), " ")
				}
			} else {
				cellText = strings.TrimRight(string(rowRunes[col.start:]), " ")
			}

			cellRuneLen := len([]rune(cellText))
			if cellRuneLen == 0 {
				return fmt.Errorf("row %d column %d (%s) has empty cell", rowIdx, cIdx+1, col.name)
			}
			if cellRuneLen > maxCellRunes {
				return fmt.Errorf("row %d column %d (%s) cell exceeds max length (%d > %d runes): %q", rowIdx, cIdx+1, col.name, cellRuneLen, maxCellRunes, cellText)
			}
		}
	}

	if len(postFence) == 0 {
		return errors.New("expected detailed explanations below the table as separate text")
	}
	if len([]rune(postFence)) < 20 {
		return errors.New("explanation below table is too short")
	}

	return nil
}

func validateCardList(text string) error {
	if strings.Contains(text, "|") {
		return errors.New("card list must not contain Markdown pipe table characters ('|')")
	}
	if strings.Contains(text, "```") {
		return errors.New("card list should be formatted as markdown list/cards, not inside fenced code block")
	}

	blocks := strings.Split(strings.TrimSpace(text), "\n\n")
	if len(blocks) < 2 {
		return fmt.Errorf("expected multiple card blocks separated by blank lines, got %d", len(blocks))
	}

	for i, block := range blocks {
		lines := strings.Split(strings.TrimSpace(block), "\n")
		if len(lines) < 2 {
			return fmt.Errorf("card %d must contain a title line and at least one detail line", i+1)
		}
		titleLine := strings.TrimSpace(lines[0])
		if !strings.HasPrefix(titleLine, "•") && !strings.HasPrefix(titleLine, "-") && !strings.HasPrefix(titleLine, "* ") {
			return fmt.Errorf("card %d must start with a bullet marker (•, -, or *): %q", i+1, titleLine)
		}
		if !strings.Contains(titleLine, "**") {
			return fmt.Errorf("card %d title must emphasize key term in bold (**term**): %q", i+1, titleLine)
		}

		hasStructuredDetail := false
		for _, line := range lines[1:] {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") || strings.HasPrefix(trimmed, "*") {
				hasStructuredDetail = true
				break
			}
		}
		if !hasStructuredDetail {
			return fmt.Errorf("card %d missing indented or labeled card fields", i+1)
		}
	}
	return nil
}

func TestDictionaryResponseFormatCompliance(t *testing.T) {
	// Example 1: Compact dictionary comparison with short columns inside a fenced code block.
	const compactTableExample = "```\n" +
		"Word   Meaning         Example\n" +
		"soil   пачкать         Don't soil clothes\n" +
		"stain  ставить пятно   Wine will stain\n" +
		"```\n" +
		"\n" +
		"soil используется для бытовой или внешней грязи (земля, пыль), а stain — для въевшихся пятен."

	// Example 2: Detailed dictionary answer with sentences formatted as cards/list instead of a wide table.
	const cardListExample = "• **soil** — испачкать, запачкать (землёй, грязью)\n" +
		"  *Контекст:* бытовое загрязнение одежды или рук.\n" +
		"  *Пример:* Don't get your clothes soiled.\n\n" +
		"• **stain** — оставить трудновыводимое пятно\n" +
		"  *Контекст:* жидкости, красители, кофе, вино.\n" +
		"  *Пример:* Red wine can stain the shirt.\n\n" +
		"• **smear** — размазать, замарать\n" +
		"  *Контекст:* жир, краска, грязь, размазанная по поверхности.\n" +
		"  *Пример:* His hands were smeared with grease."

	t.Run("ValidCompactTableExample", func(t *testing.T) {
		if err := parseMonospaceTable(compactTableExample, 25, 50); err != nil {
			t.Fatalf("compactTableExample failed validation: %v", err)
		}
	})

	t.Run("ValidCardListExample", func(t *testing.T) {
		if err := validateCardList(cardListExample); err != nil {
			t.Fatalf("cardListExample failed validation: %v", err)
		}
	})

	t.Run("CompactTableRejectsInvalidFormats", func(t *testing.T) {
		// Misaligned row (column 3 starts at offset 21 instead of 23)
		const misalignedTable = "```\n" +
			"Word   Meaning         Example\n" +
			"soil   пачкать         Don't soil clothes\n" +
			"stain  ставить пятно Wine will stain\n" +
			"```\n\nExplanation below"
		if err := parseMonospaceTable(misalignedTable, 25, 50); err == nil {
			t.Error("expected misaligned table to fail validation, but it passed")
		}

		// Pipe table instead of fenced code block table
		const pipeTable = "| Word | Meaning | Example |\n|---|---|---|\n| soil | dirt | clothes |"
		if err := parseMonospaceTable(pipeTable, 25, 50); err == nil {
			t.Error("expected pipe table to fail validation, but it passed")
		}

		// Cell with overly long explanation
		const longCellTable = "```\n" +
			"Word   Meaning\n" +
			"soil   бытовое загрязнение одежды или рук землей, пылью и грязью\n" +
			"```\n\nExplanation below"
		if err := parseMonospaceTable(longCellTable, 25, 50); err == nil {
			t.Error("expected table with long cell explanation to fail validation, but it passed")
		}

		// Missing detailed explanation below the table
		const noDetailsTable = "```\n" +
			"Word   Meaning         Example\n" +
			"soil   пачкать         Don't soil clothes\n" +
			"```"
		if err := parseMonospaceTable(noDetailsTable, 25, 50); err == nil {
			t.Error("expected table without details below to fail validation, but it passed")
		}
	})

	t.Run("CardListRejectsInvalidFormats", func(t *testing.T) {
		// Pipe table passed where card list expected
		const pipeTable = "| Word | Meaning |\n|---|---|\n| soil | dirty |"
		if err := validateCardList(pipeTable); err == nil {
			t.Error("expected pipe table to fail card list validation, but it passed")
		}

		// Fenced code block table passed where card list expected
		const fencedTable = "```\nWord   Meaning\nsoil   dirty\n```"
		if err := validateCardList(fencedTable); err == nil {
			t.Error("expected fenced table to fail card list validation, but it passed")
		}

		// Unstructured plain text
		const unstructuredText = "soil means to get dirty with mud. stain means to leave a hard spot."
		if err := validateCardList(unstructuredText); err == nil {
			t.Error("expected unstructured text to fail card list validation, but it passed")
		}

		// Flat bullet list without card structure
		const flatBullets = "• soil\n• stain\n• smear"
		if err := validateCardList(flatBullets); err == nil {
			t.Error("expected flat bullet list without card details to fail card list validation, but it passed")
		}
	})
}
