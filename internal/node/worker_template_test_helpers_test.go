package node

import "github.com/beruseruko/secretary/internal/core"

func workerTemplateFixture(instance core.HarnessInstance, model, reasoning string) ManagedProfile {
	delivery := "workspace_instructions"
	if instance.Kind == core.HarnessOpenCode {
		delivery = "native"
	}
	profile := ManagedProfile{
		Version: "synthetic-worker-template-v1", Name: "worker", Content: "Synthetic Worker template fixture.",
		AllowTools: []string{"read"}, SourceHash: "synthetic-worker-template-source", Runtime: string(instance.Kind),
		Model: model, Reasoning: reasoning, Delivery: delivery,
	}
	profile.Hash = profile.SnapshotHash()
	return profile
}
