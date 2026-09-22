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
	return s.publicWorkerDetails(ctx, conversationID, workers)
}

func (s *Server) publicWorkersForConversationLimit(ctx context.Context, conversationID string, limit int) ([]publicWorkerDTO, error) {
	workers, err := s.store.WorkersForConversationLimit(ctx, conversationID, limit)
	if err != nil {
		return nil, err
	}
	return s.publicWorkerDetails(ctx, conversationID, workers)
}

func (s *Server) publicWorkerDetails(ctx context.Context, conversationID string, workers []core.Worker) ([]publicWorkerDTO, error) {
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

// publicWorkerStrictDTO is the Pi credential shape: worker identity, status
// and results without Node topology. Owner web sessions keep publicWorkerDTO.
type publicWorkerStrictDTO struct {
	ID                string                 `json:"id"`
	WorkerRef         string                 `json:"worker_ref"`
	Title             string                 `json:"title"`
	Intent            string                 `json:"intent"`
	ProjectID         string                 `json:"project_id"`
	Status            core.WorkerStatus      `json:"status"`
	CurrentTurnID     string                 `json:"current_turn_id,omitempty"`
	TurnStatus        core.TurnState         `json:"turn_status,omitempty"`
	LastResultSummary string                 `json:"last_result_summary,omitempty"`
	Result            *publicResultStrictDTO `json:"result,omitempty"`
	CreatedAt         time.Time              `json:"created_at"`
	UpdatedAt         time.Time              `json:"updated_at"`
	ClosedAt          *time.Time             `json:"closed_at,omitempty"`
	Archived          bool                   `json:"archived"`
}

type publicAttemptStrictDTO struct {
	ID        string            `json:"id"`
	Number    int               `json:"number"`
	State     core.AttemptState `json:"state"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

type publicWorkerDetailsStrict struct {
	Worker    publicWorkerStrictDTO    `json:"worker"`
	Turns     []publicTurnStrictDTO    `json:"turns"`
	Attempts  []publicAttemptStrictDTO `json:"attempts"`
	Outcomes  []publicOutcomeStrictDTO `json:"outcomes"`
	Results   []publicResultStrictDTO  `json:"results"`
	Approvals []publicApprovalDTO      `json:"approvals,omitempty"`
}

type publicTurnStrictDTO struct {
	ID               string         `json:"id"`
	WorkerID         string         `json:"worker_id"`
	Input            string         `json:"input"`
	State            core.TurnState `json:"state"`
	CurrentAttemptID string         `json:"current_attempt_id,omitempty"`
	ResultID         string         `json:"result_id,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

type publicOutcomeStrictDTO struct {
	ID             string                     `json:"id"`
	AttemptID      string                     `json:"attempt_id"`
	Status         core.AttemptOutcomeStatus  `json:"status"`
	Classification core.OutcomeClassification `json:"classification"`
	ErrorCode      string                     `json:"error_code,omitempty"`
	CreatedAt      time.Time                  `json:"created_at"`
}

type publicResultStrictDTO struct {
	ID          string            `json:"id"`
	TurnID      string            `json:"turn_id"`
	Status      core.ResultStatus `json:"status"`
	Summary     string            `json:"summary"`
	FailureCode string            `json:"failure_code,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
}

func publicWorkerStrictDTOFromDetails(details core.WorkerDetails) publicWorkerStrictDTO {
	worker := details.Worker
	public := publicWorkerStrictDTO{
		ID: worker.ID, WorkerRef: worker.WorkerRef, Title: worker.Title, Intent: worker.Intent,
		ProjectID: worker.ProjectID,
		Status:    worker.Status, CurrentTurnID: worker.CurrentTurnID, LastResultSummary: worker.LastResultSummary,
		CreatedAt: worker.CreatedAt, UpdatedAt: worker.UpdatedAt, ClosedAt: worker.ClosedAt, Archived: worker.Archived,
	}
	for _, turn := range details.Turns {
		if turn.ID == worker.CurrentTurnID {
			public.TurnStatus = turn.State
		}
	}
	if len(details.Results) > 0 {
		result := details.Results[len(details.Results)-1]
		public.Result = &publicResultStrictDTO{
			ID: result.ID, TurnID: result.TurnID,
			Status: result.Status, Summary: result.Summary,
			FailureCode: result.FailureCode, CreatedAt: result.CreatedAt,
		}
	}
	return public
}

func publicTurnsStrict(details core.WorkerDetails) []publicTurnStrictDTO {
	turns := make([]publicTurnStrictDTO, 0, len(details.Turns))
	for _, turn := range details.Turns {
		turns = append(turns, publicTurnStrictDTO{
			ID: turn.ID, WorkerID: turn.WorkerID, Input: turn.Input, State: turn.State,
			CurrentAttemptID: turn.CurrentAttemptID, ResultID: turn.ResultID,
			CreatedAt: turn.CreatedAt, UpdatedAt: turn.UpdatedAt,
		})
	}
	return turns
}

func publicWorkerDetailsStrictFromDetails(details core.WorkerDetails) publicWorkerDetailsStrict {
	strict := publicWorkerDetailsStrict{
		Worker: publicWorkerStrictDTOFromDetails(details),
	}
	strict.Turns = publicTurnsStrict(details)
	strict.Outcomes = make([]publicOutcomeStrictDTO, 0, len(details.Outcomes))
	for _, outcome := range details.Outcomes {
		strict.Outcomes = append(strict.Outcomes, publicOutcomeStrictDTO{
			ID: outcome.ID, AttemptID: outcome.AttemptID, Status: outcome.Status,
			Classification: outcome.Classification, ErrorCode: outcome.ErrorCode,
			CreatedAt: outcome.CreatedAt,
		})
	}
	strict.Results = make([]publicResultStrictDTO, 0, len(details.Results))
	for _, result := range details.Results {
		strict.Results = append(strict.Results, publicResultStrictDTO{
			ID: result.ID, TurnID: result.TurnID, Status: result.Status, Summary: result.Summary,
			FailureCode: result.FailureCode, CreatedAt: result.CreatedAt,
		})
	}
	strict.Attempts = make([]publicAttemptStrictDTO, 0, len(details.Attempts))
	for _, attempt := range details.Attempts {
		strict.Attempts = append(strict.Attempts, publicAttemptStrictDTO{
			ID: attempt.ID, Number: attempt.Number, State: attempt.State,
			CreatedAt: attempt.CreatedAt, UpdatedAt: attempt.UpdatedAt,
		})
	}
	for _, approval := range details.Approvals {
		strict.Approvals = append(strict.Approvals, publicApprovalDTOFromApproval(approval))
	}
	return strict
}

func (s *Server) publicWorkersStrictForConversationLimit(ctx context.Context, conversationID string, limit int) ([]publicWorkerStrictDTO, error) {
	workers, err := s.store.WorkersForConversationLimit(ctx, conversationID, limit)
	if err != nil {
		return nil, err
	}
	public := make([]publicWorkerStrictDTO, 0, len(workers))
	for _, worker := range workers {
		details, err := s.store.WorkerDetailsForConversation(ctx, conversationID, worker.WorkerRef)
		if err != nil {
			return nil, err
		}
		public = append(public, publicWorkerStrictDTOFromDetails(details))
	}
	return public, nil
}
