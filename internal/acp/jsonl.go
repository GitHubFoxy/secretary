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
	"strings"
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
	log     io.Writer
	logMu   sync.Mutex
}

func Start(ctx context.Context, command string, arguments ...string) (*Client, error) {
	return StartWithLogEnv(ctx, nil, nil, command, arguments...)
}

func StartWithLog(ctx context.Context, rawLog io.Writer, command string, arguments ...string) (*Client, error) {
	return StartWithLogEnv(ctx, rawLog, nil, command, arguments...)
}

func StartWithLogEnv(ctx context.Context, rawLog io.Writer, environment []string, command string, arguments ...string) (*Client, error) {
	process := exec.CommandContext(ctx, command, arguments...)
	if environment != nil {
		process.Env = append(os.Environ(), environment...)
	}
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
	client := &Client{stdin: stdin, wait: process.Wait, events: make(chan Message, 64), done: make(chan struct{}), log: rawLog}
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
		var id uint64
		if len(message.ID) > 0 && json.Unmarshal(message.ID, &id) == nil {
			if pending, ok := c.pending.Load(id); ok {
				pending.(chan Message) <- message
				continue
			}
			if message.Method != "" {
				_ = c.handleServerRequest(message)
				continue
			}
		}
		select {
		case c.events <- message:
		default:
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
	switch message.Method {
	case "session/request_permission":
		var params struct {
			Options []struct {
				OptionID string `json:"optionId"`
				Kind     string `json:"kind"`
			} `json:"options"`
		}
		if err := json.Unmarshal(message.Params, &params); err != nil {
			return c.replyError(message.ID, -32602, "invalid permission request")
		}
		optionID := ""
		for _, option := range params.Options {
			kind := strings.ToLower(option.Kind)
			if option.OptionID == "" || !strings.Contains(kind, "allow") {
				continue
			}
			if optionID == "" || strings.Contains(kind, "always") {
				optionID = option.OptionID
				if strings.Contains(kind, "always") {
					break
				}
			}
		}
		if optionID == "" {
			return c.replyError(message.ID, -32010, "harness compatibility failure: no allow permission option")
		}
		return c.reply(message.ID, map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": optionID}})
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
