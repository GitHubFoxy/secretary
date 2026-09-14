package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
)

type Message struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("acp rpc %d: %s", e.Code, e.Message) }

type RequestError struct {
	Code    int
	Message string
}

func (e *RequestError) Error() string { return e.Message }

func NewRequestError(code int, message string) error {
	return &RequestError{Code: code, Message: message}
}

func requestErrorCode(err error) int {
	var requestErr *RequestError
	if errors.As(err, &requestErr) {
		return requestErr.Code
	}
	return -32010
}

type ServerRequestHandler func(Message) (any, error)

type ServerRequestDeliveryHandler func(Message, error)

type Client struct {
	stdin    io.WriteCloser
	wait     func() error
	write    sync.Mutex
	handler  ServerRequestHandler
	delivery ServerRequestDeliveryHandler
	nextID   atomic.Uint64
	pending  sync.Map
	events   chan Message
	done     chan struct{}
	log      io.Writer
	logMu    sync.Mutex
	stop     chan struct{}
	stopOnce sync.Once
	started  bool
	closeMu  sync.Mutex
	closeErr error
	cancel   context.CancelFunc
}

func NewClient(stdin io.WriteCloser) *Client {
	return &Client{stdin: stdin, events: make(chan Message, 64), done: make(chan struct{}), stop: make(chan struct{})}
}

func (c *Client) HandleServerRequest(message Message) error {
	return c.handleServerRequest(message)
}

// RetryServerRequest repeats a server-request reply with the original native
// request ID and reports the write result through the delivery callback.
func (c *Client) RetryServerRequest(message Message, result any, handlerErr error) error {
	var deliveryErr error
	if handlerErr != nil {
		deliveryErr = c.replyError(message.ID, requestErrorCode(handlerErr), handlerErr.Error())
		if deliveryErr == nil {
			deliveryErr = handlerErr
		}
	} else {
		deliveryErr = c.reply(message.ID, result)
	}
	c.notifyServerRequestDelivery(message, deliveryErr)
	return deliveryErr
}

func Start(ctx context.Context, command string, arguments ...string) (*Client, error) {
	return StartWithLogEnv(ctx, nil, nil, command, arguments...)
}

func StartWithLog(ctx context.Context, rawLog io.Writer, command string, arguments ...string) (*Client, error) {
	return StartWithLogEnv(ctx, rawLog, nil, command, arguments...)
}

func StartWithLogEnv(ctx context.Context, rawLog io.Writer, environment []string, command string, arguments ...string) (*Client, error) {
	processCtx, cancel := context.WithCancel(ctx)
	process := exec.CommandContext(processCtx, command, arguments...)
	if environment != nil {
		process.Env = append(os.Environ(), environment...)
	}
	stdin, err := process.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := process.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := process.Start(); err != nil {
		cancel()
		return nil, err
	}
	client := &Client{stdin: stdin, wait: process.Wait, events: make(chan Message, 64), done: make(chan struct{}), stop: make(chan struct{}), started: true, cancel: cancel, log: rawLog}
	go client.read(stdout)
	return client, nil
}

func (c *Client) Events() <-chan Message { return c.events }

func (c *Client) SetServerRequestHandler(handler ServerRequestHandler) { c.handler = handler }

// SetServerRequestDeliveryHandler observes the result of writing a JSON-RPC
// response to the native harness. The callback runs after the write returns.
func (c *Client) SetServerRequestDeliveryHandler(handler ServerRequestDeliveryHandler) {
	c.delivery = handler
}

func (c *Client) Request(ctx context.Context, method string, params any, result any) error {
	id := c.nextID.Add(1)
	encoded, err := json.Marshal(params)
	if err != nil {
		return err
	}
	response := make(chan Message, 1)
	c.pending.Store(id, response)
	defer c.pending.Delete(id)
	if err := c.send(Message{JSONRPC: "2.0", ID: json.RawMessage(fmt.Appendf(nil, "%d", id)), Method: method, Params: encoded}); err != nil {
		return err
	}
	select {
	case message := <-response:
		if message.Error != nil {
			return message.Error
		}
		if result == nil {
			return nil
		}
		return json.Unmarshal(message.Result, result)
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return errors.New("acp process stopped")
	}
}

func (c *Client) Notify(method string, params any) error {
	encoded, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.send(Message{JSONRPC: "2.0", Method: method, Params: encoded})
}

func (c *Client) Close() error {
	c.stopOnce.Do(func() {
		close(c.stop)
		if c.cancel != nil {
			c.cancel()
		}
		if c.stdin != nil {
			c.closeMu.Lock()
			c.closeErr = c.stdin.Close()
			c.closeMu.Unlock()
		}
	})
	if c.started {
		<-c.done
	}
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	return c.closeErr
}

func (c *Client) send(message Message) error {
	encoded, err := json.Marshal(message)
	if err != nil {
		return err
	}
	c.write.Lock()
	defer c.write.Unlock()
	c.writeRaw(encoded)
	_, err = c.stdin.Write(append(encoded, '\n'))
	return err
}

func (c *Client) read(stdout io.Reader) {
	defer close(c.done)
	defer close(c.events)
	defer c.wait()
	if closer, ok := c.log.(io.Closer); ok {
		defer closer.Close()
	}
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		raw := append([]byte(nil), scanner.Bytes()...)
		c.writeRaw(raw)
		var message Message
		if json.Unmarshal(raw, &message) != nil {
			continue
		}
		if len(message.ID) > 0 {
			var id uint64
			if json.Unmarshal(message.ID, &id) == nil {
				if pending, ok := c.pending.Load(id); ok {
					pending.(chan Message) <- message
					continue
				}
			}
			if message.Method != "" {
				// A harness request can arrive while a client Request such as
				// session/load is waiting for its response. Handle it outside
				// the reader so the matching RPC response can still unblock the
				// client, while the typed response remains pending in the session.
				go func(request Message) { _ = c.handleServerRequest(request) }(message)
				continue
			}
		}
		select {
		case c.events <- message:
		case <-c.stop:
			return
		}
	}
}

func (c *Client) writeRaw(raw []byte) {
	if c.log == nil {
		return
	}
	c.logMu.Lock()
	defer c.logMu.Unlock()
	_, _ = c.log.Write(append(raw, '\n'))
}

func (c *Client) handleServerRequest(message Message) error {
	if c.handler != nil {
		result, handlerErr := c.handler(message)
		var deliveryErr error
		if handlerErr != nil {
			deliveryErr = c.replyError(message.ID, requestErrorCode(handlerErr), handlerErr.Error())
			if deliveryErr == nil {
				deliveryErr = handlerErr
			}
		} else {
			deliveryErr = c.reply(message.ID, result)
		}
		c.notifyServerRequestDelivery(message, deliveryErr)
		return deliveryErr
	}
	switch message.Method {
	case "session/request_permission":
		return c.replyError(message.ID, -32010, "permission request requires explicit approval policy")
	case "fs/read_text_file":
		var params struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(message.Params, &params); err != nil || params.Path == "" {
			return c.replyError(message.ID, -32602, "path is required")
		}
		content, err := os.ReadFile(params.Path)
		if err != nil {
			return c.replyError(message.ID, -32001, err.Error())
		}
		return c.reply(message.ID, map[string]string{"content": string(content)})
	case "fs/write_text_file":
		var params struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(message.Params, &params); err != nil || params.Path == "" {
			return c.replyError(message.ID, -32602, "path is required")
		}
		if err := os.MkdirAll(filepath.Dir(params.Path), 0o700); err != nil {
			return c.replyError(message.ID, -32001, err.Error())
		}
		if err := os.WriteFile(params.Path, []byte(params.Content), 0o600); err != nil {
			return c.replyError(message.ID, -32001, err.Error())
		}
		return c.reply(message.ID, map[string]any{})
	default:
		return c.replyError(message.ID, -32601, "unsupported ACP client request: "+message.Method)
	}
}

func (c *Client) notifyServerRequestDelivery(message Message, err error) {
	if c.delivery != nil {
		c.delivery(message, err)
	}
}

func (c *Client) reply(id json.RawMessage, result any) error {
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return c.send(Message{JSONRPC: "2.0", ID: append(json.RawMessage(nil), id...), Result: encoded})
}

func (c *Client) replyError(id json.RawMessage, code int, message string) error {
	return c.send(Message{JSONRPC: "2.0", ID: append(json.RawMessage(nil), id...), Error: &RPCError{Code: code, Message: message}})
}
