package webapi

import (
	"context"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

// publicWorkerDTO is intentionally allow-listed. core.Worker contains policy,
// project and workspace snapshots that are server-only and must not cross the
// public Client API boundary.
type publicWorkerDTO struct {
	ID                string            `json:"id"`
	WorkerRef         string            `json:"worker_ref"`
	Title             string            `json:"title"`
	Intent            string            `json:"intent"`
	ProjectID         string            `json:"project_id"`
	NodeID            string            `json:"node_id"`
	HarnessInstanceID string            `json:"harness_instance_id"`
	Status            core.WorkerStatus `json:"status"`
	CurrentTurnID     string            `json:"current_turn_id,omitempty"`
	TurnStatus        core.TurnState    `json:"turn_status,omitempty"`
	LastResultSummary string            `json:"last_result_summary,omitempty"`
	Result            *publicResultDTO  `json:"result,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
	ClosedAt          *time.Time        `json:"closed_at,omitempty"`
	Archived          bool              `json:"archived"`
}

type publicResultDTO struct {
	ID            string            `json:"id"`
	WorkerID      string            `json:"worker_id"`
	WorkerRef     string            `json:"worker_ref"`
	TurnID        string            `json:"turn_id"`
	AttemptID     string            `json:"attempt_id,omitempty"`
	Status        core.ResultStatus `json:"status"`
	Summary       string            `json:"summary"`
	FailureCode   string            `json:"failure_code,omitempty"`
	CorrelationID string            `json:"correlation_id,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
}

func publicWorkerDTOFromDetails(details core.WorkerDetails) publicWorkerDTO {
	worker := details.Worker
	public := publicWorkerDTO{
		ID: worker.ID, WorkerRef: worker.WorkerRef, Title: worker.Title, Intent: worker.Intent,
		ProjectID: worker.ProjectID, NodeID: worker.NodeID, HarnessInstanceID: worker.HarnessInstanceID,
		Status: worker.Status, CurrentTurnID: worker.CurrentTurnID, LastResultSummary: worker.LastResultSummary,
		CreatedAt: worker.CreatedAt, UpdatedAt: worker.UpdatedAt, ClosedAt: worker.ClosedAt, Archived: worker.Archived,
	}
	for _, turn := range details.Turns {
		if turn.ID == worker.CurrentTurnID {
			public.TurnStatus = turn.State
		}
	}
	if len(details.Results) > 0 {
		result := details.Results[len(details.Results)-1]
		public.Result = &publicResultDTO{
			ID: result.ID, WorkerID: result.WorkerID, WorkerRef: worker.WorkerRef, TurnID: result.TurnID,
			AttemptID: result.AttemptID, Status: result.Status, Summary: result.Summary,
			FailureCode: result.FailureCode, CorrelationID: result.CorrelationID, CreatedAt: result.CreatedAt,
		}
	}
	return public
}

func (s *Server) publicWorkersForConversation(ctx context.Context, conversationID string) ([]publicWorkerDTO, error) {
	workers, err := s.store.WorkersForConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	public := make([]publicWorkerDTO, 0, len(workers))
	for _, worker := range workers {
		details, err := s.store.WorkerDetailsForConversation(ctx, conversationID, worker.WorkerRef)
		if err != nil {
			return nil, err
		}
		public = append(public, publicWorkerDTOFromDetails(details))
	}
	return public, nil
}
