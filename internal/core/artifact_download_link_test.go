package core

import "testing"

func TestT09ClickableDownloadDedupDoesNotTrustRawURL(t *testing.T) {
  url := "/v1/artifacts/abc?download=1"
  cases := []struct{ name, summary string; want bool }{
    {"artifact-ref", "artifact://abc", false},
    {"bare-url", "Here it is: "+url, false},
    {"inline-code", "Printed as `"+url+"`", false},
    {"fenced-code", "~~~\n[Download]("+url+")\n~~~", false},
    {"real-markdown", "Here: [Download]("+url+")", true},
    {"other-artifact", "[Download](/v1/artifacts/xyz?download=1)", false},
    {"other-query", "[Download](/v1/artifacts/abc?download=0)", false},
    {"existing-clickable", "plain "+url+" then [Get file]("+url+")", true},
    {"image-only", "![Preview]("+url+")", false},
    {"image-and-link", "![Preview]("+url+") and [Download]("+url+")", true},
    {"escaped-close-bracket", "[not-a-link\\]("+url+")", false},
    {"escaped-close-followed-by-valid-link", "[not-a-link\\]("+url+") [Download]("+url+")", true},
    {"double-escaped-close-bracket", "[Download\\\\\\\\]("+url+")", true},
    {"escaped-open-bracket", "\\[Download]("+url+")", false},
    {"escaped-image-open-bracket", "!\\[Download]("+url+")", false},
    {"escaped-image-marker", "\\![Download]("+url+")", true},
    {"double-escaped-bracket", "\\\\[Download]("+url+")", true},
    {"indented-code", "    [Download]("+url+")", false},
    {"tab-indented-code", "\t[Download]("+url+")", false},
    {"two-tick-inline-code", "Printed as ``[Download]("+url+")``", false},
    {"three-tick-inline-code", "Printed as ```[Download]("+url+")```", false},
    {"mismatched-inline-ticks", "``[Download]("+url+")` still code", false},
    {"fenced-image", "\x60\x60\x60md\n[Download]("+url+")\n\x60\x60\x60", false},
  }
  for _, tc := range cases { t.Run(tc.name, func(t *testing.T) {
    if got := hasClickableArtifactDownload(tc.summary,url); got != tc.want {
      t.Fatalf("clickable(%q)=%t, want %t",tc.summary,got,tc.want)
    }
  }) }
}
