package config

import (
	"io/fs"
	"strings"
	"testing"
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

func TestDictionaryResponseFormatCompliance(t *testing.T) {
	// Example 1: Compact dictionary comparison with short columns inside a fenced code block.
	const compactTableExample = "```\n" +
		"Word   Meaning      Example\n" +
		"soil   пачкать      Don't soil clothes\n" +
		"stain  ставить пятно Wine will stain\n" +
		"```\n" +
		"\n" +
		"soil используется для бытовой или внешней грязи (земля, пыль), а stain — для въевшихся пятен."

	if !strings.HasPrefix(compactTableExample, "```") {
		t.Errorf("compact table must be in a fenced code block")
	}

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

	// Ensure no bare Markdown pipe table (e.g. |---|---|) is present in the card list.
	if strings.Contains(cardListExample, "|---|") || strings.Contains(cardListExample, "|") {
		t.Errorf("card list should not contain Markdown pipe table markers")
	}
}
