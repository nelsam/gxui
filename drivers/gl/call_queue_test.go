package gl_test

import (
	"testing"
	"time"

	"git.sr.ht/~nelsam/gxui/drivers/gl"
)

func TestCallQueue_Closed(t *testing.T) {
	c := gl.NewCallQueue()
	c.Close()

	call, ok := c.Pop()
	if call != nil || ok {
		t.Errorf("Expected (nil, false) from c.Pop; got (%T, %v)", call, ok)
	}
	call, ok = c.PopWhenReady()
	if call != nil || ok {
		t.Errorf("Expected (nil, false) from c.Pop; got (%T, %v)", call, ok)
	}
}

func TestCallQueue_Empty(t *testing.T) {
	c := gl.NewCallQueue()
	defer c.Close()

	call, ok := c.Pop()
	if call != nil || !ok {
		t.Errorf("Expected (nil, true) from c.Pop; got (%T, %v)", call, ok)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		c.PopWhenReady()
	}()
	select {
	case <-done:
		t.Error("Expected c.PopWhenReady to block until data was available")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestCallQueue_Simple(t *testing.T) {
	c := gl.NewCallQueue()
	defer c.Close()

	called := make(chan struct{})
	c.Inject(func() { close(called) })
	call, ok := c.Pop()
	if !ok || call == nil {
		t.Errorf("Expected (non-nil, true) from c.Pop; got (%T, %v)", call, ok)
	}
	call()
	select {
	case <-called:
	default:
		t.Error("Expected c.Inject to inject the passed in call")
	}

	called = make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		call, ok = c.PopWhenReady()
	}()

	c.Inject(func() { close(called) })
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected c.PopWhenReady to return after c.Inject")
	}

	if call == nil || !ok {
		t.Errorf("Expected (non-nil, true) from c.PopWhenReady; got (%T, %v)", call, ok)
		t.FailNow()
	}
	call()
	select {
	case <-called:
	default:
		t.Error("Expected c.Inject to inject the passed in call")
	}
}

func TestCallQueue_BlockingAndOrder(t *testing.T) {
	c := gl.NewCallQueue()
	defer c.Close()

	excessive := 10000
	var callsDone []chan struct{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < excessive; i++ {
			callerDone := make(chan struct{})
			callsDone = append(callsDone, callerDone)
			c.Inject(func() { close(callerDone) })
		}
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected c.Inject to accept an indefinite number of calls")
	}
	var calls []func()
	done = make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < excessive; i++ {
			call, ok := c.PopWhenReady()
			if !ok {
				return
			}
			calls = append(calls, call)
		}
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Errorf("c.PopWhenReady returned %d calls, expected %d", len(calls), excessive)
	}
	for i, call := range calls {
		if call == nil {
			t.Fatalf("Expected call %d not to be nil; bailing early", i)
		}
		call()
		select {
		case <-callsDone[i]:
		default:
			t.Fatal("Expected calls to be injected in order; bailing early")
		}
	}
}
