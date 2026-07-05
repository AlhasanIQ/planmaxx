package browser

import (
	"reflect"
	"testing"
	"time"
)

func TestCommandForDarwin(t *testing.T) {
	name, args := commandFor("darwin", "http://127.0.0.1:9000")

	if name != "open" {
		t.Fatalf("expected open, got %q", name)
	}
	if !reflect.DeepEqual(args, []string{"http://127.0.0.1:9000"}) {
		t.Fatalf("unexpected args %#v", args)
	}
}

func TestCommandForLinux(t *testing.T) {
	name, args := commandFor("linux", "http://127.0.0.1:9000")

	if name != "xdg-open" {
		t.Fatalf("expected xdg-open, got %q", name)
	}
	if !reflect.DeepEqual(args, []string{"http://127.0.0.1:9000"}) {
		t.Fatalf("unexpected args %#v", args)
	}
}

func TestCommandForWindows(t *testing.T) {
	name, args := commandFor("windows", "http://127.0.0.1:9000")

	if name != "rundll32" {
		t.Fatalf("expected rundll32, got %q", name)
	}
	if !reflect.DeepEqual(args, []string{"url.dll,FileProtocolHandler", "http://127.0.0.1:9000"}) {
		t.Fatalf("unexpected args %#v", args)
	}
}

func TestCommandForUnsupported(t *testing.T) {
	name, args := commandFor("plan9", "http://127.0.0.1:9000")

	if name != "" {
		t.Fatalf("expected empty command, got %q", name)
	}
	if args != nil {
		t.Fatalf("expected nil args, got %#v", args)
	}
}

func TestOpenReapsStartedProcessAsynchronously(t *testing.T) {
	started := make(chan struct{})
	wait := make(chan struct{})
	waited := make(chan struct{})
	oldStartProcess := startProcess
	startProcess = func(name string, args ...string) (process, error) {
		if name != "xdg-open" {
			t.Fatalf("expected xdg-open, got %q", name)
		}
		if !reflect.DeepEqual(args, []string{"http://127.0.0.1:9000"}) {
			t.Fatalf("unexpected args %#v", args)
		}
		close(started)
		return processFunc(func() error {
			close(waited)
			<-wait
			return nil
		}), nil
	}
	t.Cleanup(func() {
		startProcess = oldStartProcess
	})

	if err := open("linux", "http://127.0.0.1:9000"); err != nil {
		t.Fatalf("open returned error: %v", err)
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for process start")
	}
	select {
	case <-waited:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for process reap")
	}
	close(wait)
}

type processFunc func() error

func (f processFunc) Wait() error {
	return f()
}
