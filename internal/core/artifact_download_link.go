package core

import "strings"

// Determine whether the summary includes a real clickable Markdown link to
// this exact Artifact URL, ignoring bare text, code blocks and inline code.
func hasClickableArtifactDownload(summary, url string) bool {
  inFence := false
  var fence byte
  for _, line := range strings.Split(summary, "\n") {
    trimmed := strings.TrimSpace(line)
    if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
      if !inFence { inFence, fence = true, trimmed[0] } else if trimmed[0] == fence { inFence = false }
      continue
    }
    if inFence { continue }
    inCode := false
    for i := 0; i < len(line); i++ {
      if line[i] == '`' { inCode = !inCode; continue }
      if inCode || line[i] != '[' || markdownEscaped(line, i) { continue }
      // An image reference ![alt](url) is rendered as an image, not a
      // clickable owner-download link. Escaped ! is ordinary prose.
      if i > 0 && line[i-1] == '!' && !markdownEscaped(line, i-1) { continue }
      closing := strings.Index(line[i+1:], "](")
      if closing < 0 { continue }
      destination := i + 1 + closing + 2
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
