package acp

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

func TestRequestDeferredDrainPreservesFIFOAndErrors(t *testing.T) {
	for _, rpcFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "summary", true: "rpc-error"}[rpcFailure], func(t *testing.T) {
			reader, input := io.Pipe()
			client := NewClient(&recordingWriter{})
			client.wait = func() error { return nil }
			go client.read(reader)
			defer client.Close()
			defer input.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			type completion struct {
				drain   func(context.Context) error
				err     error
				summary string
			}
			returned := make(chan completion, 1)
			go func() {
				var result struct{ Summary string }
				drain, err := client.RequestWithDeferredEventDrain(ctx, "session/prompt", map[string]string{}, &result)
				returned <- completion{drain, err, result.Summary}
			}()
			for {
				if _, exists := client.pending.Load(uint64(1)); exists {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("request not admitted")
				default:
				}
			}
			response := `{"id":1,"result":{"summary":"explicit"}}`
			if rpcFailure {
				response = `{"id":1,"error":{"code":-32000,"message":"synthetic failure"}}`
			}
			go func() { _, _ = io.WriteString(input, "{\"method\":\"first\"}\n{\"method\":\"last\"}\n"+response+"\n") }()
			var got completion
			select {
			case got = <-returned:
			case <-ctx.Done():
				t.Fatal("deferred request incorrectly waited for consumer")
			}
			if got.drain == nil {
				t.Fatal("deferred drain lost its FIFO fence")
			}
			var rpcErr *RPCError
			if rpcFailure {
				if !errors.As(got.err, &rpcErr) || rpcErr.Code != -32000 {
					t.Fatal("RPC error was changed")
				}
			} else if got.err != nil || got.summary != "explicit" {
				t.Fatal("explicit summary was changed")
			}
			for _, method := range []string{"first", "last"} {
				message := <-client.Events()
				if message.Method != method || message.AcknowledgeEventBarrier() {
					t.Fatal("preceding notification order changed")
				}
			}
			barrier := <-client.Events()
			canceled, cancelWait := context.WithCancel(ctx)
			cancelWait()
			if !errors.Is(got.drain(canceled), context.Canceled) {
				t.Fatal("unacknowledged deferred wait ignored cancellation")
			}
			if !barrier.AcknowledgeEventBarrier() {
				t.Fatal("native FIFO fence was not present")
			}
			if err := got.drain(ctx); err != nil {
				t.Fatal("acknowledged deferred wait failed")
			}
		})
	}
}
