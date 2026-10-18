package core

import "strings"

// Determine whether the summary includes a real clickable Markdown link to
// this exact Artifact URL, ignoring bare text, code blocks and inline code.
func hasClickableArtifactDownload(summary, url string) bool {
  inFence := false
  var fence byte
  inCode := false
  codeRun := 0
  for _, line := range strings.Split(summary, "\n") {
    if strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") { continue }
    trimmed := strings.TrimSpace(line)
    if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
      if !inFence { inFence, fence = true, trimmed[0] } else if trimmed[0] == fence { inFence = false }
      continue
    }
    if inFence { continue }
    for i := 0; i < len(line); i++ {
      if line[i] == '`' {
        j := i
        for j < len(line) && line[j] == '`' { j++ }
        if !inCode { inCode = true; codeRun = j-i } else if j-i == codeRun { inCode=false; codeRun=0 }
        i=j-1
        continue
      }
      if inCode || line[i] != '[' || markdownEscaped(line, i) { continue }
      // An image reference ![alt](url) is rendered as an image, not a
      // clickable owner-download link. Escaped ! is ordinary prose.
      if i > 0 && line[i-1] == '!' && !markdownEscaped(line, i-1) { continue }
      closing := -1
      for j := i + 1; j + 1 < len(line); j++ {
        if line[j] == ']' && line[j+1] == '(' && !markdownEscaped(line, j) {
          closing = j
          break
        }
      }
      if closing < 0 { continue }
      destination := closing + 2
      if strings.HasPrefix(line[destination:], url+")") { return true }
    }
  }
  return false
}

func markdownEscaped(line string, position int) bool {
	slashes := 0
	for i := position-1; i >= 0 && line[i] == '\\'; i-- {
		slashes++
	}
	return slashes%2 == 1
}
