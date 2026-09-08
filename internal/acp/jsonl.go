package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
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

type Client struct {
	stdin   io.WriteCloser
	wait    func() error
	write   sync.Mutex
	nextID  atomic.Uint64
	pending sync.Map
	events  chan Message
	done    chan struct{}
}

func Start(ctx context.Context, command string, arguments ...string) (*Client, error) {
	process := exec.CommandContext(ctx, command, arguments...)
	stdin, err := process.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := process.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := process.Start(); err != nil {
		return nil, err
	}
	client := &Client{stdin: stdin, wait: process.Wait, events: make(chan Message, 64), done: make(chan struct{})}
	go client.read(stdout)
	return client, nil
}

func (c *Client) Events() <-chan Message { return c.events }

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
	err := c.stdin.Close()
	<-c.done
	return err
}

func (c *Client) send(message Message) error {
	encoded, err := json.Marshal(message)
	if err != nil {
		return err
	}
	c.write.Lock()
	defer c.write.Unlock()
	_, err = c.stdin.Write(append(encoded, '\n'))
	return err
}

func (c *Client) read(stdout io.Reader) {
	defer close(c.done)
	defer close(c.events)
	defer c.wait()
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		var message Message
		if json.Unmarshal(scanner.Bytes(), &message) != nil {
			continue
		}
		var id uint64
		if len(message.ID) > 0 && json.Unmarshal(message.ID, &id) == nil {
			if pending, ok := c.pending.Load(id); ok {
				pending.(chan Message) <- message
				continue
			}
		}
		select {
		case c.events <- message:
		default:
		}
	}
}
