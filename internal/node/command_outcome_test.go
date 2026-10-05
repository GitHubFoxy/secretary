package node

import "testing"

func TestRespondWorkerOutcomeErrorsDistinguishUnknownFromDenial(t *testing.T) {
	for _, code := range []string{"execution_state_unknown", "response_failed", "response_record_failed"} {
		if !uncertainRespondOutcome(CommandOutcome{State: CommandFailed, ErrorCode: code}) {
			t.Errorf("outcome %q was treated as authoritative denial", code)
		}
	}
	if !uncertainRespondOutcome(CommandOutcome{State: CommandInterrupted}) {
		t.Error("interrupted outcome was treated as authoritative denial")
	}
	if uncertainRespondOutcome(CommandOutcome{State: CommandFailed, ErrorCode: "runtime_does_not_accept_response"}) {
		t.Error("explicit pre-delivery rejection was treated as uncertain")
	}
}
