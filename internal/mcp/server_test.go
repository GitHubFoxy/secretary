package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type fake struct{}

func (fake) Tools() []Tool                                                    { return []Tool{{Name: "x", InputSchema: map[string]any{"type": "object"}}} }
func (fake) Call(_ context.Context, n string, _ json.RawMessage) (any, error) { return n, nil }
func TestServerReturnsToolErrorsAsIsError(t *testing.T) {
	server := Server{Handler: handlerFunc{tools: nil, call: func(context.Context, string, json.RawMessage) (any, error) { return nil, errors.New("denied") }}}
	var out bytes.Buffer
	if err := server.Serve(context.Background(), bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"x","arguments":{}}}`+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out.Bytes(), []byte(`"isError":true`)) || bytes.Contains(out.Bytes(), []byte(`"error":`)) {
		t.Fatal(out.String())
	}
}

type handlerFunc struct {
	tools []Tool
	call  func(context.Context, string, json.RawMessage) (any, error)
}

func (h handlerFunc) Tools() []Tool { return h.tools }
func (h handlerFunc) Call(ctx context.Context, name string, args json.RawMessage) (any, error) {
	return h.call(ctx, name, args)
}

func TestServerListsAndCallsTools(t *testing.T) {
	in := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n" + `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"x","arguments":{}}}` + "\n")
	var out bytes.Buffer
	if err := (Server{Handler: fake{}}).Serve(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out.Bytes(), []byte(`"tools"`)) || !bytes.Contains(out.Bytes(), []byte(`"text":"x"`)) {
		t.Fatal(out.String())
	}
}
