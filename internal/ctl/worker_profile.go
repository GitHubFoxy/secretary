package ctl

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

var (
	ErrWorkerProfileUnavailable = errors.New("worker: managed Profile is unavailable")
	ErrWorkerProfileInvalid     = errors.New("worker: managed Profile is invalid")
)

func bindManagedWorkerProfile(profile node.ManagedProfile, resolution core.DispatchResolution) (node.ManagedProfile, error) {
	if profile.Version == "" || profile.Name != "worker" || strings.TrimSpace(profile.Content) == "" || profile.Hash == "" {
		return node.ManagedProfile{}, ErrWorkerProfileUnavailable
	}
	if !validProfileHarness(profile.Runtime) {
		return node.ManagedProfile{}, ErrWorkerProfileInvalid
	}
	if profile.SourceHash == "" {
		profile.SourceHash = profile.Hash
	}
	profile.Runtime = string(resolution.HarnessInstance.Kind)
	profile.Model = resolution.Snapshot.Policy.ModelPin()
	profile.Reasoning = resolution.Snapshot.Policy.Reasoning
	if profile.Runtime == string(core.HarnessOpenCode) {
		profile.Delivery = "native"
	} else {
		profile.Delivery = "workspace_instructions"
	}
	profile.Hash = profile.SnapshotHash()
	if err := profile.ValidateWorkerBinding(profile.Runtime, profile.Model, profile.Reasoning, resolution.Snapshot.Policy); err != nil {
		return node.ManagedProfile{}, ErrWorkerProfileInvalid
	}
	return profile, nil
}

func validProfileHarness(harness string) bool {
	switch core.HarnessKind(harness) {
	case core.HarnessOpenCode, core.HarnessFX, core.HarnessClaudeCode, core.HarnessCodex:
		return true
	default:
		return false
	}
}

func managedWorkerProfileForBinding(worker core.Worker, binding core.ProjectDispatch) (node.ManagedProfile, error) {
	if worker.ProfileSnapshot == "" {
		if !worker.WorkerTemplateRequired && binding.HarnessInstance.Kind == core.HarnessFX {
			return node.ManagedProfile{}, nil
		}
		return node.ManagedProfile{}, ErrWorkerProfileUnavailable
	}
	decoder := json.NewDecoder(bytes.NewBufferString(worker.ProfileSnapshot))
	decoder.DisallowUnknownFields()
	var profile node.ManagedProfile
	if err := decoder.Decode(&profile); err != nil {
		return node.ManagedProfile{}, ErrWorkerProfileInvalid
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return node.ManagedProfile{}, ErrWorkerProfileInvalid
	}
	if err := profile.ValidateWorkerBinding(string(binding.HarnessInstance.Kind), binding.Snapshot.Policy.ModelPin(), binding.Snapshot.Policy.Reasoning, binding.Snapshot.Policy); err != nil {
		return node.ManagedProfile{}, ErrWorkerProfileInvalid
	}
	return profile, nil
}
