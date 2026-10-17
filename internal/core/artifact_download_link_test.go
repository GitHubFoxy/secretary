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
  }
  for _, tc := range cases { t.Run(tc.name, func(t *testing.T) {
    if got := hasClickableArtifactDownload(tc.summary,url); got != tc.want {
      t.Fatalf("clickable(%q)=%t, want %t",tc.summary,got,tc.want)
    }
  }) }
}
