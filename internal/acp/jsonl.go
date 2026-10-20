package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"

	"github.com/beruseruko/secretary/internal/nativeexec"
)

type Message struct {
	JSONRPC      string          `json:"jsonrpc,omitempty"`
	ID           json.RawMessage `json:"id,omitempty"`
	Method       string          `json:"method,omitempty"`
	Params       json.RawMessage `json:"params,omitempty"`
	Result       json.RawMessage `json:"result,omitempty"`
	Error        *RPCError       `json:"error,omitempty"`
	eventBarrier chan struct{}
}

func (m Message) AcknowledgeEventBarrier() bool {
	if m.eventBarrier == nil {
		return false
	}
	close(m.eventBarrier)
	return true
}

type pendingRequest struct {
	response    chan Message
	drainEvents bool
}

type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("acp rpc %d: %s", e.Code, e.Message) }

func (e *RPCError) HasData() bool {
	return len(e.Data) > 0 && string(e.Data) != "null"
}

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
	return StartWithLogEnvDir(ctx, rawLog, environment, "", command, arguments...)
}

func StartWithLogEnvDir(ctx context.Context, rawLog io.Writer, environment []string, dir string, command string, arguments ...string) (*Client, error) {
	return startWithProcessEnvironment(ctx, rawLog, environment, true, dir, command, arguments...)
}

// StartWithDiagnosticStderrEnvDir captures only child stderr (not ACP frames)
// for explicitly scoped diagnostic probes. Production ACP clients remain unchanged.
func StartWithDiagnosticStderrEnvDir(ctx context.Context, stderr io.Writer, environment []string, dir string, command string, arguments ...string) (*Client, error) {
	processCtx, cancel := context.WithCancel(ctx)
	process, err := nativeexec.CommandContext(processCtx, command, arguments...)
	if err != nil { cancel(); return nil, err }
	configureACPProcess(process)
	process.Env = nativeexec.SecureWorkerEnvironment(processCtx, append(os.Environ(), environment...))
	process.Dir = dir
	process.Stderr = stderr
	stdin, err := process.StdinPipe()
	if err != nil { cancel(); return nil, err }
	stdout, err := process.StdoutPipe()
	if err != nil { cancel(); return nil, err }
	if err := process.Start(); err != nil { cancel(); return nil, err }
	client := &Client{stdin: stdin, wait: process.Wait, events: make(chan Message, 64), done: make(chan struct{}), stop: make(chan struct{}), started: true, cancel: cancel}
	go client.read(stdout)
	return client, nil
}

func StartWithLogExactEnvDir(ctx context.Context, rawLog io.Writer, environment []string, dir string, command string, arguments ...string) (*Client, error) {
	return startWithProcessEnvironment(ctx, rawLog, environment, false, dir, command, arguments...)
}

func startWithProcessEnvironment(ctx context.Context, rawLog io.Writer, environment []string, inherit bool, dir string, command string, arguments ...string) (*Client, error) {
	processCtx, cancel := context.WithCancel(ctx)
	process, err := nativeexec.CommandContext(processCtx, command, arguments...)
	if err != nil {
		cancel()
		return nil, err
	}
	configureACPProcess(process)
	if inherit && environment != nil {
		process.Env = append(os.Environ(), environment...)
	} else if !inherit {
		process.Env = append([]string{}, environment...)
	}
	if dir != "" {
		process.Dir = dir
	}
	process.Env = nativeexec.SecureWorkerEnvironment(processCtx, process.Env)
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

func (c *Client) SetServerRequestDeliveryHandler(handler ServerRequestDeliveryHandler) {
	c.delivery = handler
}

func (c *Client) Request(ctx context.Context, method string, params any, result any) error {
	return c.request(ctx, method, params, result, false)
}

func (c *Client) RequestDrainingEvents(ctx context.Context, method string, params any, result any) error {
	return c.request(ctx, method, params, result, true)
}

func (c *Client) RequestWithDeferredEventDrain(ctx context.Context, method string, params any, result any) (func(context.Context) error, error) {
	return c.requestWithEventBarrier(ctx, method, params, result, true)
}

func (c *Client) request(ctx context.Context, method string, params any, result any, drainEvents bool) error {
	drain, err := c.requestWithEventBarrier(ctx, method, params, result, drainEvents)
	if drain != nil {
		if drainErr := drain(ctx); drainErr != nil {
			return drainErr
		}
	}
	return err
}

func (c *Client) requestWithEventBarrier(ctx context.Context, method string, params any, result any, drainEvents bool) (func(context.Context) error, error) {
	id := c.nextID.Add(1)
	encoded, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	response := make(chan Message, 1)
	c.pending.Store(id, pendingRequest{response: response, drainEvents: drainEvents})
	defer c.pending.Delete(id)
	if err := c.send(Message{JSONRPC: "2.0", ID: json.RawMessage(fmt.Appendf(nil, "%d", id)), Method: method, Params: encoded}); err != nil {
		return nil, err
	}
	select {
	case message := <-response:
		var drain func(context.Context) error
		if message.eventBarrier != nil {
			drain = func(waitCtx context.Context) error {
				select {
				case <-message.eventBarrier:
					return nil
				case <-waitCtx.Done():
					return waitCtx.Err()
				case <-c.stop:
					return errors.New("acp process stopped")
				}
			}
		}
		if message.Error != nil {
			return drain, message.Error
		}
		if result == nil {
			return drain, nil
		}
		return drain, json.Unmarshal(message.Result, result)
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, errors.New("acp process stopped")
	}
}

func (c *Client) Notify(method string, params any) error {
	encoded, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.send(Message{JSONRPC: "2.0", Method: method, Params: encoded})
}

func (c *Client) NotifyContext(ctx context.Context, method string, params any) error {
	done := make(chan error, 1)
	go func() { done <- c.Notify(method, params) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		select {
		case err := <-done:
			return err
		default:
		}
		go func() { _ = c.Close() }()
		return ctx.Err()
	}
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
	c.writeRaw("tx", encoded)
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
	reader := bufio.NewReader(stdout)
	for {
		raw, _ := reader.ReadBytes('\n')
		if len(raw) == 0 {
			break
		}
		raw = bytes.TrimSuffix(raw, []byte{'\n'})
		c.writeRaw("rx", raw)
		var message Message
		if json.Unmarshal(raw, &message) != nil {
			continue
		}
		if message.Method != "" {
			if len(message.ID) > 0 {
				go func(request Message) { _ = c.handleServerRequest(request) }(message)
				continue
			}
			select {
			case c.events <- message:
			case <-c.stop:
				return
			}
			continue
		}
		if len(message.ID) > 0 {
			var id uint64
			if json.Unmarshal(message.ID, &id) == nil {
				if pending, ok := c.pending.Load(id); ok {
					request := pending.(pendingRequest)
					if request.drainEvents {
						message.eventBarrier = make(chan struct{})
						select {
						case c.events <- Message{eventBarrier: message.eventBarrier}:
						case <-c.stop:
							return
						}
					}
					request.response <- message
					continue
				}
			}
		}
		select {
		case c.events <- message:
		case <-c.stop:
			return
		}
	}
}

type frameLogger interface {
	WriteACPFrame(direction string, raw []byte) (int, error)
}

func (c *Client) writeRaw(direction string, raw []byte) {
	if c.log == nil {
		return
	}
	c.logMu.Lock()
	defer c.logMu.Unlock()
	if logger, ok := c.log.(frameLogger); ok {
		_, _ = logger.WriteACPFrame(direction, append(raw, '\n'))
		return
	}
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
