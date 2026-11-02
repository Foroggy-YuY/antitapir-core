package control

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type harness struct {
	input  *io.PipeWriter
	events chan Message
	done   chan error
}

func newHarness(t *testing.T, start Start, timeout time.Duration) *harness {
	t.Helper()
	in, writer := io.Pipe()
	out, eventWriter := io.Pipe()
	h := &harness{writer, make(chan Message, 32), make(chan error, 1)}
	go func() {
		scanner := bufio.NewScanner(out)
		for scanner.Scan() {
			var event Message
			_ = json.Unmarshal(scanner.Bytes(), &event)
			h.events <- event
		}
	}()
	go func() { h.done <- Serve(in, eventWriter, start, timeout); _ = eventWriter.Close(); _ = in.Close() }()
	t.Cleanup(func() { _ = writer.Close(); _ = out.Close() })
	h.expect(t, "hello")
	return h
}

func (h *harness) send(t *testing.T, version int, kind string) {
	t.Helper()
	if err := json.NewEncoder(h.input).Encode(Message{Protocol: version, Type: kind}); err != nil {
		t.Fatal(err)
	}
}
func (h *harness) expect(t *testing.T, kind string) Message {
	t.Helper()
	select {
	case event := <-h.events:
		if event.Type != kind || event.Protocol != Protocol {
			t.Fatalf("expected %s, got %+v", kind, event)
		}
		return event
	case <-time.After(2 * time.Second):
		t.Fatalf("missing event %s", kind)
	}
	return Message{}
}
func (h *harness) finished(t *testing.T, wantError bool) {
	t.Helper()
	select {
	case err := <-h.done:
		if (err != nil) != wantError {
			t.Fatalf("result: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("session did not finish")
	}
}

func TestLifecycleAndEOF(t *testing.T) {
	for _, disconnect := range []bool{false, true} {
		t.Run(map[bool]string{false: "stop", true: "EOF"}[disconnect], func(t *testing.T) {
			var closed atomic.Bool
			h := newHarness(t, func(ctx context.Context, emit func(string) error, _ []byte) (func() error, error) {
				_ = emit("config_accepted")
				return func() error { closed.Store(true); return nil }, nil
			}, time.Second)
			h.send(t, Protocol, "start")
			h.expect(t, "config_accepted")
			h.expect(t, "ready")
			if disconnect {
				_ = h.input.Close()
			} else {
				h.send(t, Protocol, "stop")
			}
			h.expect(t, "stopping")
			h.expect(t, "stopped")
			h.finished(t, false)
			if !closed.Load() {
				t.Fatal("engine was not closed")
			}
		})
	}
}

func TestVersionAndInvalidCommandNeverStartEngine(t *testing.T) {
	for _, msg := range []Message{{Protocol: 2, Type: "start"}, {Protocol: 1, Type: "unexpected"}} {
		h := newHarness(t, func(context.Context, func(string) error, []byte) (func() error, error) {
			t.Error("unexpected engine startup")
			return nil, nil
		}, time.Second)
		h.send(t, msg.Protocol, msg.Type)
		h.expect(t, "error")
		h.finished(t, true)
	}
}

func TestCancelDuringStartup(t *testing.T) {
	for _, disconnect := range []bool{false, true} {
		var closed atomic.Bool
		h := newHarness(t, func(ctx context.Context, emit func(string) error, _ []byte) (func() error, error) {
			_ = emit("config_accepted")
			<-ctx.Done()
			return func() error { closed.Store(true); return nil }, ctx.Err()
		}, time.Second)
		h.send(t, Protocol, "start")
		h.expect(t, "config_accepted")
		if disconnect {
			_ = h.input.Close()
		} else {
			h.send(t, Protocol, "stop")
		}
		h.expect(t, "stopping")
		h.expect(t, "stopped")
		h.finished(t, false)
		if !closed.Load() {
			t.Fatal("partial engine not cleaned up")
		}
	}
}

func TestStartupErrorDoesNotExposeSecrets(t *testing.T) {
	h := newHarness(t, func(context.Context, func(string) error, []byte) (func() error, error) {
		return nil, errors.New("secret-password")
	}, time.Second)
	h.send(t, Protocol, "start")
	h.expect(t, "stopping")
	h.expect(t, "stopped")
	event := h.expect(t, "error")
	if event.Code != "startup_failed" {
		t.Fatalf("unsafe error %+v", event)
	}
	h.finished(t, true)
}

func TestHandshakeTimeoutAndMalformedInput(t *testing.T) {
	for _, input := range []string{"", "not-json\n", strings.Repeat("x", MaxMessage+1) + "\n"} {
		h := newHarness(t, func(context.Context, func(string) error, []byte) (func() error, error) {
			t.Error("unexpected start")
			return nil, nil
		}, 20*time.Millisecond)
		if input != "" {
			_, _ = io.WriteString(h.input, input)
		}
		h.expect(t, "error")
		h.finished(t, true)
	}
}

func TestStartupDeadlineCancelsEngine(t *testing.T) {
	h := newHarness(t, func(ctx context.Context, emit func(string) error, _ []byte) (func() error, error) {
		_ = emit("config_accepted")
		<-ctx.Done()
		return func() error { return nil }, ctx.Err()
	}, 20*time.Millisecond)
	h.send(t, Protocol, "start")
	h.expect(t, "config_accepted")
	h.expect(t, "stopping")
	h.expect(t, "stopped")
	if h.expect(t, "error").Code != "startup_timeout" {
		t.Fatal("wrong timeout error")
	}
	h.finished(t, true)
}
