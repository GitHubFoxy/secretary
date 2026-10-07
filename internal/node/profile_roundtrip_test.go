package node_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

// Literals pin the pre-fix unversioned SHA256 contract, independently of the
// implementation under test. Omitted old fields decode as nil; explicit []
// snapshots keep their original distinct hashes. Neither requires migration.
func TestWorkerTemplatePriorHashesSurviveJSON(t *testing.T) {
	for _, fixture := range []struct {
		name, collections, hash string
	}{
		{"omitted", "", "3730e4d5df80508df5dae9de31e208c61be570d516c947d719e573c7bd1c7849"},
		{"empty", `,"skills":[],"allow_tools":[]`, "256a49b05b1c8bd2bbf09f1a8bbca331187729079fdf3493df06dc55f606890c"},
		{"nonempty", `,"skills":[{"path":"synthetic.md","content":"Synthetic managed skill.","hash":"synthetic-skill-hash"}],"allow_tools":["read"]`, "8d71e56ad3bb39d526e9d94df9c48f01110bdafaac586303c64610602156dd9a"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			encoded := `{"version":"synthetic-v1","name":"worker","content":"Synthetic frozen instructions.","source_hash":"synthetic-source-hash","runtime":"opencode","model":"fixture/model","reasoning":"xhigh","delivery":"native","hash":"` + fixture.hash + `"` + fixture.collections + `}`
			var prior node.ManagedProfile
			if err := json.Unmarshal([]byte(encoded), &prior); err != nil {
				t.Fatal("synthetic old snapshot decoding failed")
			}
			if prior.SnapshotHash() != fixture.hash || prior.ValidateWorkerBinding("opencode", "fixture/model", "xhigh", core.ProjectPolicy{}) != nil {
				t.Fatal("prior valid hash contract changed")
			}
			wire, err := json.Marshal(prior)
			if err != nil {
				t.Fatal("snapshot encoding failed")
			}
			var reopened node.ManagedProfile
			if err := json.Unmarshal(wire, &reopened); err != nil || !reflect.DeepEqual(prior, reopened) || reopened.SnapshotHash() != fixture.hash {
				t.Fatal("wire changed a prior hash-significant representation")
			}
			// No compatibility fallback: changing a hash-significant field must
			// still fail even when nil/empty Skills mean the same prompt.
			for _, mutation := range []struct {
				name  string
				apply func(*node.ManagedProfile)
			}{
				{"instructions", func(p *node.ManagedProfile) { p.Content += " changed" }},
				{"skills", func(p *node.ManagedProfile) {
					p.Skills = []node.ManagedSkill{{Path: "added.md", Content: "added", Hash: "added"}}
				}},
				{"permissions", func(p *node.ManagedProfile) { p.AllowTools = []string{"shell"} }},
				{"hash", func(p *node.ManagedProfile) { p.Hash = "invalid" }},
				{"source", func(p *node.ManagedProfile) { p.SourceHash += " changed" }},
				{"version", func(p *node.ManagedProfile) { p.Version += " changed" }},
			} {
				t.Run(mutation.name, func(t *testing.T) {
					changed := prior
					mutation.apply(&changed)
					if changed.ValidateWorkerBinding("opencode", "fixture/model", "xhigh", core.ProjectPolicy{}) == nil {
						t.Fatal("tampered template passed integrity validation")
					}
				})
			}
		})
	}
}

func TestWorkerTemplateCollectionsRetainExactHashContract(t *testing.T) {
	profile := node.ManagedProfile{Content: "Synthetic instructions."}
	nilHash := profile.SnapshotHash()
	profile.Skills = []node.ManagedSkill{}
	if profile.EffectivePrompt() != "Synthetic instructions." || profile.SnapshotHash() == nilHash {
		t.Fatal("existing exact hash contract or empty Skills prompt semantics changed")
	}
	if node.HashProfile(profile.Content, nil, "synthetic") != node.HashProfile(profile.Content, []node.ManagedSkill{}, "synthetic") {
		t.Fatal("source Profile hash stopped treating nil/empty Skills equivalently")
	}
	profile.Skills = nil
	profile.AllowTools = []string{}
	if profile.SnapshotHash() == nilHash {
		t.Fatal("AllowTools null/[] were silently canonicalized")
	}
}
