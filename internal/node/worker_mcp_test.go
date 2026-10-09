package node

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

func TestNodeDeploymentLoadsApprovedWorkerMCP(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{"server_url":"http://127.0.0.1:8081","node":"macbook","data_dir":"/tmp/node","mcp_servers":[{"name":"readonly","command":"python3","args":["/tmp/read-mcp.py"],"env":[{"name":"MCP_ENDPOINT","value":"http://127.0.0.1:8181"}]}]}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadDeploymentConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.MCPServers) != 1 || config.MCPServers[0].Env[0].Name != "MCP_ENDPOINT" {
		t.Fatalf("MCP config lost: %#v", config.MCPServers)
	}
}

func TestExecutionNodeDeliversLocalMCPAtStartAndContinuation(t *testing.T) {
	for _, kind := range []core.HarnessKind{core.HarnessClaudeCode, core.HarnessCodex} {
		t.Run(string(kind), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			path := filepath.Join(t.TempDir(), "node.json")
			store, err := OpenLocalStore(path)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			runtime := &workerMCPRuntime{requests: make(chan StartRequest, 4), results: make(chan chan Result, 4)}
			servers := []MCPServer{{Name: "readonly", Command: "python3", Args: []string{"/tmp/read.py"}, Env: []MCPEnv{{Name: "MCP_ENDPOINT", Value: "http://127.0.0.1:8181"}}}}
			execution := NewExecutionNode("macbook", runtime, store)
			supplied := []MCPServer{{Name: "readonly", Command: "python3", Args: []string{"/tmp/read.py"}, Env: []MCPEnv{{Name: "MCP_ENDPOINT", Value: "http://127.0.0.1:8181"}}}}
			if err := execution.SetMCPServers(supplied); err != nil {
				t.Fatal(err)
			}
			supplied[0].Env[0].Value = "tampered"
			if err := execution.SetMCPServers([]MCPServer{{Name: "secretary", Command: "forbidden"}}); err == nil {
				t.Fatal("reserved MCP accepted")
			}
			command := dispatchFixture("mcp-initial")
			command.Dispatch.Envelope.HarnessInstance.Kind = kind
			command.Dispatch.Envelope.Profile = workerTemplateFixture(command.Dispatch.Envelope.HarnessInstance, "", "")
			if outcome, err := execution.HandleCommand(ctx, command); err != nil || outcome.State != CommandAccepted {
				t.Fatalf("dispatch=%#v err=%v", outcome, err)
			}
			assert := func() {
				t.Helper()
				select {
				case request := <-runtime.requests:
					if !reflect.DeepEqual(request.MCPServers, servers) {
						t.Fatalf("runtime MCP=%#v", request.MCPServers)
					}
					request.MCPServers[0].Command = "runtime mutation"
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			assert()
			first := <-runtime.results
			first <- Result{Status: "succeeded", Summary: "done"}
			waitOutcome := func(expected string) {
				t.Helper()
				for {
					events, err := store.PendingEvents()
					if err != nil {
						t.Fatal(err)
					}
					for _, event := range events {
						if event.EventID == "attempt-outcome-"+expected {
							return
						}
					}
					select {
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					case <-time.After(time.Millisecond):
					}
				}
			}
			waitOutcome("attempt-1")
			followup := dispatchFixture("mcp-followup")
			followup.Dispatch.Envelope.HarnessInstance = command.Dispatch.Envelope.HarnessInstance
			followup.Dispatch.Envelope.Profile = command.Dispatch.Envelope.Profile
			followup.Dispatch.Metadata.TurnID = "turn-2"
			followup.Dispatch.Metadata.AttemptID = "attempt-2"
			followup.Dispatch.Envelope.TurnID = "turn-2"
			followup.Dispatch.Envelope.AttemptID = "attempt-2"
			followup.Dispatch.Envelope.PreviousAttemptID = "attempt-1"
			if outcome, err := execution.HandleCommand(ctx, followup); err != nil || outcome.State != CommandAccepted {
				t.Fatalf("followup=%#v err=%v", outcome, err)
			}
			assert()
			second := <-runtime.results
			second <- Result{Status: "succeeded", Summary: "followup done"}
			waitOutcome("attempt-2")
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := OpenLocalStore(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			restarted := NewExecutionNode("macbook", runtime, reopened)
			if err := restarted.SetMCPServers(servers); err != nil {
				t.Fatal(err)
			}
			metadata := command.Metadata()
			metadata.CommandID = "mcp-resume"
			resume := Command{Kind: CommandResume, Resume: &ResumeCommand{Metadata: metadata, Envelope: command.Dispatch.Envelope}}
			if outcome, err := restarted.HandleCommand(ctx, resume); err != nil || outcome.State != CommandAccepted {
				t.Fatalf("resume=%#v err=%v", outcome, err)
			}
			assert()
			recovery := NewExecutionNode("macbook", runtime, reopened)
			if err := recovery.SetMCPServers(servers); err != nil {
				t.Fatal(err)
			}
			steeringMetadata := command.Metadata()
			steeringMetadata.CommandID = "mcp-recovery"
			if outcome, err := recovery.HandleCommand(ctx, Command{Kind: CommandSteering, Steering: &SteeringCommand{Metadata: steeringMetadata, Text: "continue"}}); err != nil || outcome.State != CommandAccepted {
				t.Fatalf("recovery=%#v err=%v", outcome, err)
			}
			assert()
		})
	}
}

type workerMCPRuntime struct {
	requests chan StartRequest
	results  chan chan Result
}

func (r *workerMCPRuntime) Start(_ context.Context, request StartRequest) (Session, error) {
	r.requests <- request
	results := make(chan Result, 1)
	r.results <- results
	return &workerMCPSession{reconnectSession: newReconnectSession("mcp-native"), results: results}, nil
}
func (r *workerMCPRuntime) Resume(_ context.Context, request StartRequest, id string) (Session, error) {
	r.requests <- request
	results := make(chan Result, 1)
	r.results <- results
	return &workerMCPSession{reconnectSession: newReconnectSession(id), results: results}, nil
}

func TestNodeDeploymentRejectsUnsafeWorkerMCPWithoutSecrets(t *testing.T) {
	base := `{"server_url":"http://127.0.0.1:8081","node":"macbook","data_dir":"/tmp/node","mcp_servers":%s}`
	cases := []string{
		`[ {"name":"same","command":"read"},{"name":"same","command":"write"} ]`,
		`[{"name":"secretary","command":"tool"}]`,
		`[{"name":"bad,*","command":"tool"}]`,
		`[{"name":"read","command":""}]`,
		`[{"name":"read","command":"tool","env":[{"name":"BAD=KEY","value":"private-value"}]}]`,
		`[{"name":"read","command":"tool","env":[{"name":"KEY","value":"private-value"},{"name":"KEY","value":"other"}]}]`,
		`[{"name":"read","command":"tool","env":[{"name":"SECRETARY_MCP_CAPABILITY","value":"private-value"}]}]`,
		`[{"name":"read","name":"other","command":"tool"}]`,
		`[{"name":"read","command":"tool","args":["\u0000"]}]`,
	}
	path := filepath.Join(t.TempDir(), "config.json")
	for _, definition := range cases {
		body := strings.Replace(base, "%s", definition, 1)
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := LoadDeploymentConfig(path)
		if err == nil {
			t.Fatalf("unsafe Worker MCP accepted: %s", definition)
		}
		if strings.Contains(err.Error(), "private-value") {
			t.Fatal("validation exposed environment value")
		}
	}
}

type workerMCPSession struct {
	*reconnectSession
	results chan Result
}

func (s *workerMCPSession) Result() <-chan Result { return s.results }
func (s *workerMCPSession) Close() error          { close(s.results); return nil }
