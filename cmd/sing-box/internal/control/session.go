// Package control implements Anti-Tapir's private, inherited stdio protocol.
package control

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"
)

const Protocol = 1
const Version = "0.1.0"
const MaxMessage = 4096

type Message struct {
	Protocol int    `json:"protocol"`
	Type     string `json:"type"`
	Version  string `json:"version,omitempty"`
	Code     string `json:"code,omitempty"`
}

// Start must honor context cancellation and return a cleanup function even
// after partial startup. The callback emits only non-secret lifecycle events.
type Start func(context.Context, func(string) error) (func() error, error)

type result struct {
	close func() error
	err   error
}

func Serve(input io.Reader, output io.Writer, start Start, timeout time.Duration) error {
	var lock sync.Mutex
	emit := func(kind, code string) error {
		lock.Lock()
		defer lock.Unlock()
		return json.NewEncoder(output).Encode(Message{Protocol: Protocol, Type: kind, Version: Version, Code: code})
	}
	fail := func(code string) error { _ = emit("error", code); return errors.New(code) }
	commands := make(chan Message, 1)
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		scanner := bufio.NewScanner(input)
		scanner.Buffer(make([]byte, MaxMessage), MaxMessage)
		for scanner.Scan() {
			var msg Message
			if json.Unmarshal(scanner.Bytes(), &msg) != nil {
				return
			}
			select {
			case commands <- msg:
			case <-time.After(timeout):
				return
			}
		}
	}()
	if emit("hello", "") != nil {
		return errors.New("channel_closed")
	}
	// A pipe can contain the final complete message followed immediately by EOF.
	next := func() (Message, bool) {
		select {
		case msg := <-commands:
			return msg, true
		default:
		}
		select {
		case msg := <-commands:
			return msg, true
		case <-ended:
			select {
			case msg := <-commands:
				return msg, true
			default:
				return Message{}, false
			}
		case <-time.After(timeout):
			return Message{}, false
		}
	}
	msg, ok := next()
	if !ok {
		return fail("start_timeout_or_channel_closed")
	}
	if msg.Protocol != Protocol {
		return fail("protocol_mismatch")
	}
	if msg.Type == "stop" {
		_ = emit("stopping", "")
		return emit("stopped", "")
	}
	if msg.Type != "start" {
		return fail("invalid_command")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan result, 1)
	go func() {
		closeEngine, err := start(ctx, func(kind string) error { return emit(kind, "") })
		started <- result{closeEngine, err}
	}()
	var engine result
	received := false
	stop := func() error {
		_ = emit("stopping", "")
		cancel()
		if !received {
			select {
			case engine = <-started:
				received = true
			case <-time.After(timeout):
				return fail("shutdown_timeout")
			}
		}
		closed := make(chan error, 1)
		go func() {
			if engine.close != nil {
				closed <- engine.close()
			} else {
				closed <- nil
			}
		}()
		select {
		case err := <-closed:
			if err != nil {
				return fail("shutdown_failed")
			}
			return emit("stopped", "")
		case <-time.After(timeout):
			return fail("shutdown_timeout")
		}
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case engine = <-started:
		received = true
		if engine.err != nil {
			// Startup errors are deliberately codes, never raw config/parser text.
			_ = stop()
			return fail("startup_failed")
		}
		if emit("ready", "") != nil {
			return stop()
		}
	case msg = <-commands:
		if msg.Protocol != Protocol || msg.Type != "stop" {
			_ = stop()
			return fail("invalid_command")
		}
		return stop()
	case <-ended:
		return stop()
	case <-timer.C:
		_ = stop()
		return fail("startup_timeout")
	}
	select {
	case msg = <-commands:
		if msg.Protocol != Protocol || msg.Type != "stop" {
			_ = stop()
			return fail("invalid_command")
		}
		return stop()
	case <-ended:
		return stop()
	}
}
