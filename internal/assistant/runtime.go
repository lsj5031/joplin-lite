package assistant

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/MoeForge/moejs"
	"github.com/MoeForge/moejs/eventloop"
	"github.com/MoeForge/moejs/webapi"
)

const memoryLimit = 128 << 20

// agentRuntime owns one event-loop goroutine and one moejs runtime. Runtime
// values stay on that goroutine; host workers only receive detached JSON and
// settle results back through Loop.RunOnLoop.
type agentRuntime struct {
	manager *Manager
	loop    *eventloop.Loop
	api     *webapi.API
	module  *moejs.Module
	client  *http.Client

	mu       sync.Mutex
	run      *Run
	ready    chan struct{}
	readyErr error
	done     chan struct{}
	closed   bool
	resetCh  chan error

	readyOnce sync.Once
	closeOnce sync.Once
	closedCh  chan struct{}
}

var (
	moduleOnce sync.Once
	module     *moejs.Module
	moduleErr  error
)

func compiledAgent() (*moejs.Module, error) {
	moduleOnce.Do(func() {
		module, moduleErr = moejs.Compile("agent.js", embeddedAgentSource)
	})
	return module, moduleErr
}

func (r *agentRuntime) bindRun(run *Run) {
	r.mu.Lock()
	r.run = run
	r.mu.Unlock()
}

func (r *agentRuntime) clearRun() {
	r.mu.Lock()
	r.run = nil
	r.mu.Unlock()
}

// clearRunIf only clears the run it was asked about. A newer run may bind
// between a terminal event and cleanup; that newer run must keep its host
// promises.
func (r *agentRuntime) clearRunIf(run *Run) {
	r.mu.Lock()
	if r.run == run {
		r.run = nil
	}
	r.mu.Unlock()
}

func (r *agentRuntime) currentRun() *Run {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.run
}

func (r *agentRuntime) markReady() {
	r.readyOnce.Do(func() { close(r.ready) })
}

func (r *agentRuntime) markClosed() {
	r.closeOnce.Do(func() { close(r.closedCh) })
}

func (r *agentRuntime) isClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

func (r *agentRuntime) finishReset(err error) {
	r.mu.Lock()
	ch := r.resetCh
	r.resetCh = nil
	r.mu.Unlock()
	if ch != nil {
		select {
		case ch <- err:
		default:
		}
	}
}

func (r *agentRuntime) failReady(err error) {
	r.mu.Lock()
	if r.readyErr == nil {
		r.readyErr = err
	}
	r.mu.Unlock()
	r.markReady()
}

func (r *agentRuntime) open(ctx context.Context, settings Settings, conversationID string) error {
	ok := r.loop.RunOnLoop(func(rt *moejs.Runtime) {
		hook, err := r.module.Hook("open")
		if err != nil {
			r.failReady(err)
			return
		}
		config := map[string]any{
			"apiVersion":     APIVersion,
			"conversationId": conversationID,
			"model": map[string]any{
				"baseUrl":       settings.BaseURL,
				"modelId":       settings.ModelID,
				"contextWindow": settings.ContextWindow,
				"maxTokens":     settings.MaxOutput,
			},
			"timeoutMs": settings.TimeoutMS,
		}
		arg, err := rt.FromGo(config)
		if err != nil {
			r.failReady(err)
			return
		}
		if _, err := rt.Call(hook, arg); err != nil {
			r.failReady(err)
		}
	})
	if !ok {
		return errors.New("the agent loop is not running")
	}
	select {
	case <-r.ready:
		r.mu.Lock()
		err := r.readyErr
		r.mu.Unlock()
		return err
	case <-r.done:
		r.mu.Lock()
		err := r.readyErr
		r.mu.Unlock()
		if err == nil {
			err = errors.New("the agent runtime stopped while opening")
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *agentRuntime) loopRunning() bool {
	select {
	case <-r.done:
		return false
	default:
		return true
	}
}

func (r *agentRuntime) resetConversation(ctx context.Context) error {
	ch := make(chan error, 1)
	r.mu.Lock()
	r.resetCh = ch
	r.mu.Unlock()
	hook, err := r.module.Hook("reset")
	if err != nil {
		r.finishReset(err)
		return err
	}
	ok := r.loop.RunOnLoop(func(rt *moejs.Runtime) {
		if _, err := rt.Call(hook); err != nil {
			r.finishReset(err)
		}
	})
	if !ok {
		r.finishReset(errors.New("the agent loop is not running"))
		return errors.New("the agent loop is not running")
	}
	select {
	case err := <-ch:
		return err
	case <-r.done:
		return errors.New("the agent runtime stopped while resetting")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *agentRuntime) submit(run *Run) error {
	hook, err := r.module.Hook("submit")
	if err != nil {
		return err
	}
	payload := map[string]any{"requestId": run.ID, "text": run.Text}
	ok := r.loop.RunOnLoop(func(rt *moejs.Runtime) {
		arg, err := rt.FromGo(payload)
		if err != nil {
			r.manager.failRun(run, "invalid_request", err.Error())
			return
		}
		if _, err := rt.Call(hook, arg); err != nil {
			r.manager.failRun(run, "runtime_failed", err.Error())
		}
	})
	if !ok {
		return errors.New("the agent loop is not running")
	}
	return nil
}

func (r *agentRuntime) abort(reason string) {
	r.loop.RunOnLoop(func(rt *moejs.Runtime) {
		hook, err := r.module.Hook("abort")
		if err != nil {
			return
		}
		arg, err := rt.FromGo(map[string]any{"reason": reason})
		if err != nil {
			return
		}
		_, _ = rt.Call(hook, arg)
	})
}

func (r *agentRuntime) snapshot() (string, error) {
	if !r.loopRunning() {
		return "", errors.New("the agent runtime is not running")
	}
	result := make(chan string, 1)
	ok := r.loop.RunOnLoop(func(rt *moejs.Runtime) {
		hook, err := r.module.Hook("snapshot")
		if err != nil {
			result <- ""
			return
		}
		value, err := rt.Call(hook)
		if err != nil {
			result <- ""
			return
		}
		text, err := rt.ToGo(value)
		if err != nil {
			result <- ""
			return
		}
		result <- fmt.Sprint(text)
	})
	if !ok {
		return "", errors.New("the agent loop is not running")
	}
	select {
	case value := <-result:
		return value, nil
	case <-time.After(5 * time.Second):
		return "", errors.New("the agent did not answer the snapshot request")
	}
}

func (r *agentRuntime) close(ctx context.Context) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	r.mu.Unlock()

	// Post the JavaScript close and give it a bounded chance to flush Pi
	// state. Closing the loop alone is not a cleanup protocol.
	if r.loopRunning() {
		if hook, err := r.module.Hook("close"); err == nil {
			r.loop.RunOnLoop(func(rt *moejs.Runtime) {
				_, _ = rt.Call(hook)
			})
		}
		select {
		case <-r.closedCh:
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
			slog.Warn("ask runtime did not close gracefully")
		}
	}
	r.api.Close()
	r.loop.StopNoWait()
	select {
	case <-r.done:
	case <-time.After(5 * time.Second):
		r.loop.Terminate()
	}
}

func newAgentRuntime(m *Manager, settings Settings, conversationID string) (*agentRuntime, error) {
	mod, err := compiledAgent()
	if err != nil {
		return nil, fmt.Errorf("compile embedded agent: %w", err)
	}
	target, err := url.Parse(settings.BaseURL)
	if err != nil {
		return nil, err
	}
	client, err := newEndpointClient(settings)
	if err != nil {
		return nil, err
	}
	rt := moejs.NewRuntime(moejs.Options{MemoryLimit: memoryLimit})
	loop, err := eventloop.New(rt, eventloop.Options{
		OnError: func(err error) {
			slog.Error("ask runtime loop error", "error", err)
			m.loopError(err)
		},
	})
	if err != nil {
		return nil, err
	}
	api, err := webapi.Install(loop, webapi.Options{Client: client, Allow: allowModelRequest(target)})
	if err != nil {
		return nil, err
	}
	r := &agentRuntime{
		manager:  m,
		loop:     loop,
		api:      api,
		module:   mod,
		client:   client,
		ready:    make(chan struct{}),
		done:     make(chan struct{}),
		closedCh: make(chan struct{}),
	}
	if err := r.installHost(); err != nil {
		api.Close()
		return nil, err
	}
	loaded := make(chan error, 1)
	go func() {
		err := loop.Run(func(rt *moejs.Runtime) error {
			err := rt.Load(mod)
			loaded <- err
			return err
		})
		r.mu.Lock()
		if err != nil && r.readyErr == nil {
			r.readyErr = err
		}
		r.mu.Unlock()
		r.markReady()
		close(r.done)
	}()
	select {
	case err := <-loaded:
		if err != nil {
			api.Close()
			return nil, fmt.Errorf("load embedded agent: %w", err)
		}
	case <-time.After(10 * time.Second):
		api.Close()
		loop.StopNoWait()
		return nil, errors.New("timed out loading the embedded agent")
	}
	return r, nil
}

func (r *agentRuntime) installHost() error {
	rt := r.loop.Runtime()
	host := map[string]any{
		"apiVersion": APIVersion,
		"emit": moejs.NativeFunc(func(realm *moejs.Realm, this moejs.Value, args []moejs.Value) (moejs.Value, error) {
			defer r.recoverHost("emit")
			if len(args) == 0 {
				return moejs.Undefined(), nil
			}
			raw, err := json.Marshal(realm.ToGo(args[0]))
			if err != nil {
				return moejs.Undefined(), err
			}
			var event map[string]any
			if err := json.Unmarshal(raw, &event); err != nil {
				return moejs.Undefined(), err
			}
			r.manager.handleAgentEvent(r, event)
			return moejs.Undefined(), nil
		}),
		"notesSearch": moejs.NativeFunc(func(realm *moejs.Realm, this moejs.Value, args []moejs.Value) (moejs.Value, error) {
			var input SearchInput
			if err := decodeArg(realm, args, &input); err != nil {
				return r.resolvedError(toolErr("invalid_request", "The search arguments are invalid."))
			}
			return r.promise(func(ctx context.Context, run *Run) (any, error) {
				return r.manager.searchNotes(ctx, run, input)
			})
		}),
		"notesRead": moejs.NativeFunc(func(realm *moejs.Realm, this moejs.Value, args []moejs.Value) (moejs.Value, error) {
			var input ReadInput
			if err := decodeArg(realm, args, &input); err != nil {
				return r.resolvedError(toolErr("invalid_request", "The read arguments are invalid."))
			}
			return r.promise(func(ctx context.Context, run *Run) (any, error) {
				return r.manager.readNote(ctx, run, input)
			})
		}),
		"notesListNotebooks": moejs.NativeFunc(func(realm *moejs.Realm, this moejs.Value, args []moejs.Value) (moejs.Value, error) {
			return r.promise(func(ctx context.Context, run *Run) (any, error) {
				return r.manager.listNotebooks(ctx, run)
			})
		}),
		"notesListTags": moejs.NativeFunc(func(realm *moejs.Realm, this moejs.Value, args []moejs.Value) (moejs.Value, error) {
			return r.promise(func(ctx context.Context, run *Run) (any, error) {
				return r.manager.listTags(ctx, run)
			})
		}),
		"dbCall": moejs.NativeFunc(func(realm *moejs.Realm, this moejs.Value, args []moejs.Value) (moejs.Value, error) {
			var request dbRequest
			if err := decodeArg(realm, args, &request); err != nil {
				return r.resolvedError(toolErr("invalid_request", "The database arguments are invalid."))
			}
			// Durable storage work outlives one run: Pi may flush or read just
			// after a terminal event, before the next run binds. It is still a
			// runtime-owned operation, so it must not depend on currentRun().
			return r.promiseContext(context.Background(), func(ctx context.Context) (any, error) {
				// Do not cancel this context after BEGIN returns: database/sql
				// rolls the transaction back when its context is canceled, and
				// the transaction must outlive the begin request.
				return r.manager.asst.execute(ctx, request)
			})
		}),
		"randomHex": moejs.NativeFunc(func(realm *moejs.Realm, this moejs.Value, args []moejs.Value) (moejs.Value, error) {
			n := 16
			if len(args) > 0 {
				switch value := realm.ToGo(args[0]).(type) {
				case int64:
					if value > 0 && value <= 4096 {
						n = int(value)
					}
				case float64:
					if value > 0 && value <= 4096 {
						n = int(value)
					}
				}
			}
			b := make([]byte, n)
			if _, err := rand.Read(b); err != nil {
				return moejs.Undefined(), err
			}
			return moejs.String(hex.EncodeToString(b)), nil
		}),
		"randomUUID": moejs.NativeFunc(func(realm *moejs.Realm, this moejs.Value, args []moejs.Value) (moejs.Value, error) {
			return moejs.String(uuid()), nil
		}),
		"log": moejs.NativeFunc(func(realm *moejs.Realm, this moejs.Value, args []moejs.Value) (moejs.Value, error) {
			if len(args) > 0 {
				message := fmt.Sprint(realm.ToGo(args[0]))
				if len(message) > 2000 {
					message = message[:2000]
				}
				slog.Info("ask agent", "message", message)
			}
			return moejs.Undefined(), nil
		}),
	}
	return rt.SetGlobal("joplinHost", host)
}

func (r *agentRuntime) recoverHost(name string) {
	if recovered := recover(); recovered != nil {
		slog.Error("ask host callback panicked", "callback", name, "panic", recovered)
		r.manager.loopError(fmt.Errorf("host callback %s panicked", name))
	}
}

func decodeArg(realm *moejs.Realm, args []moejs.Value, target any) error {
	if len(args) == 0 {
		return errors.New("missing argument")
	}
	raw, err := json.Marshal(realm.ToGo(args[0]))
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}

// promise creates a loop promise for a run-scoped tool. Storage calls use
// promiseContext directly because they are runtime-owned, not run-owned.
func (r *agentRuntime) promise(work func(context.Context, *Run) (any, error)) (moejs.Value, error) {
	run := r.currentRun()
	if run == nil {
		return r.resolvedError(toolErr("runtime_failed", "No Ask run is active."))
	}
	return r.promiseContext(run.ctx, func(ctx context.Context) (any, error) {
		return work(ctx, run)
	})
}

// promiseContext creates a loop promise and runs the work on a detached
// goroutine. The conversion callback is the only code that touches runtime
// values.
func (r *agentRuntime) promiseContext(ctx context.Context, work func(context.Context) (any, error)) (moejs.Value, error) {
	p, settle := r.loop.NewPromise()
	go func() {
		var payload any
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					payload = map[string]any{"error": ToolError{Code: "runtime_failed", Message: "The Ask host crashed."}}
					slog.Error("ask host worker panicked", "panic", recovered)
				}
			}()
			value, err := work(ctx)
			if err != nil {
				code, message := "runtime_failed", err.Error()
				var structured *ToolError
				if errors.As(err, &structured) {
					code, message = structured.Code, structured.Message
				}
				payload = map[string]any{"error": ToolError{Code: code, Message: message}}
			} else {
				payload = value
			}
		}()
		raw, err := json.Marshal(payload)
		if err != nil {
			raw = []byte(`{"error":{"code":"runtime_failed","message":"The host could not encode its response."}}`)
		}
		text := string(raw)
		r.loop.RunOnLoop(func(rt *moejs.Runtime) {
			value, err := rt.FromGo(text)
			if err != nil {
				value = moejs.Undefined()
			}
			settle(func(*moejs.Runtime) (moejs.Value, error) { return value, nil })
		})
	}()
	return p, nil
}

func (r *agentRuntime) resolvedError(err error) (moejs.Value, error) {
	raw, _ := json.Marshal(map[string]any{"error": err})
	value, convErr := r.loop.Runtime().FromGo(string(raw))
	if convErr != nil {
		return moejs.Undefined(), convErr
	}
	return value, nil
}

func uuid() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func requestHost(raw map[string]any) string {
	if value, ok := raw["requestId"].(string); ok {
		return value
	}
	return ""
}

func stringValue(raw map[string]any, key string) string {
	if value, ok := raw[key].(string); ok {
		return value
	}
	return ""
}

func intValue(raw map[string]any, key string) int {
	switch value := raw[key].(type) {
	case float64:
		return int(value)
	case int64:
		return int(value)
	case int:
		return value
	}
	return 0
}

func activityFrom(raw map[string]any) *Activity {
	value, ok := raw["activity"].(map[string]any)
	if !ok {
		return nil
	}
	return &Activity{
		Kind:   stringValue(value, "kind"),
		Tool:   stringValue(value, "tool"),
		Label:  stringValue(value, "label"),
		Status: stringValue(value, "status"),
		Active: intValue(value, "active"),
	}
}

func normalizeCode(code string) string {
	switch code {
	case "invalid_request", "out_of_scope", "note_missing", "note_changed", "encrypted_content",
		"budget_exceeded", "provider_auth", "provider_rate_limit", "provider_unavailable",
		"storage_failed", "cancelled", "runtime_failed", "busy":
		return code
	default:
		return "runtime_failed"
	}
}

func trimLog(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 500 {
		return value[:500]
	}
	return value
}
