package sidequestions

import (
	"context"
	"errors"
	"testing"
)

type fakeAskClient struct {
	answer string
	err    error
	got    Request
	called bool
}

func (f *fakeAskClient) Ask(ctx context.Context, req Request) (string, error) {
	f.called = true
	f.got = req
	return f.answer, f.err
}

func TestServiceReturnsUnavailableWithoutThreadID(t *testing.T) {
	client := &fakeAskClient{answer: "ok"}
	service := NewService("", client)

	_, err := service.Ask(context.Background(), Request{Question: "Why?", PlanExcerpt: "Step 1"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
	if client.called {
		t.Fatal("expected client not to be called without original thread context")
	}
}

func TestServiceRequiresQuestion(t *testing.T) {
	client := &fakeAskClient{answer: "ok"}
	service := NewService("thread-1", client)

	_, err := service.Ask(context.Background(), Request{Question: "  ", PlanExcerpt: "Step 1"})
	if err == nil {
		t.Fatal("expected question required error")
	}
}

func TestServiceAsksClientWithContext(t *testing.T) {
	client := &fakeAskClient{answer: "Use Cobra because command structure matters."}
	service := NewService("thread-1", client)

	answer, err := service.Ask(context.Background(), Request{Question: "Why Cobra?", PlanExcerpt: "CLI milestone"})
	if err != nil {
		t.Fatal(err)
	}
	if answer != "Use Cobra because command structure matters." {
		t.Fatalf("unexpected answer %q", answer)
	}
}

func TestServiceInjectsCurrentThreadID(t *testing.T) {
	client := &fakeAskClient{answer: "ok"}
	service := NewService("current-thread", client)

	_, err := service.Ask(context.Background(), Request{ThreadID: "review-thread", Question: "Why?", PlanExcerpt: "Step 1"})
	if err != nil {
		t.Fatal(err)
	}
	if client.got.ThreadID != "current-thread" {
		t.Fatalf("expected injected thread id, got %q", client.got.ThreadID)
	}
}

func TestServiceReturnsUnavailableWithoutThreadIDEvenWithCopiedContext(t *testing.T) {
	service := NewService("", nil)

	_, err := service.Ask(context.Background(), Request{Question: "Why?", PlanExcerpt: "Step 1"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
}

func TestServiceReturnsUnavailableWithoutPrimaryClient(t *testing.T) {
	service := NewService("current-thread", nil)

	_, err := service.Ask(context.Background(), Request{Question: "Why?", PlanExcerpt: "Step 1"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
}

func TestServiceUsesPrimaryWithOriginalThreadContext(t *testing.T) {
	primary := &fakeAskClient{answer: "Primary answer."}
	service := NewService("current-thread", primary)

	answer, err := service.Ask(context.Background(), Request{Question: "Why?", PlanExcerpt: "Step 1"})
	if err != nil {
		t.Fatal(err)
	}
	if answer != "Primary answer." {
		t.Fatalf("unexpected answer %q", answer)
	}
	if !primary.called {
		t.Fatal("expected primary to be called")
	}
	if primary.got.ThreadID != "current-thread" {
		t.Fatalf("expected injected thread id, got %q", primary.got.ThreadID)
	}
}

func TestServiceRequiresQuestionBeforeClient(t *testing.T) {
	client := &fakeAskClient{answer: "ok"}
	service := NewService("current-thread", client)

	_, err := service.Ask(context.Background(), Request{Question: "  ", PlanExcerpt: "Step 1"})
	if err == nil {
		t.Fatal("expected question required error")
	}
	if client.called {
		t.Fatal("expected client not to be called when question is empty")
	}
}

func TestServiceReturnsPrimaryErrorWhenPrimaryFails(t *testing.T) {
	primaryErr := errors.New("read current thread: unavailable")
	primary := &fakeAskClient{err: primaryErr}
	service := NewService("current-thread", primary)

	_, err := service.Ask(context.Background(), Request{Question: "Why?", PlanExcerpt: "Step 1"})
	if !errors.Is(err, primaryErr) {
		t.Fatalf("expected primary error, got %v", err)
	}
	if !primary.called {
		t.Fatal("expected primary to be called")
	}
	if primary.got.ThreadID != "current-thread" {
		t.Fatalf("expected primary thread id injection, got %q", primary.got.ThreadID)
	}
}

func TestServiceReturnsContextErrorBeforeClientWhenCanceled(t *testing.T) {
	client := &fakeAskClient{answer: "ok"}
	service := NewService("current-thread", client)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := service.Ask(ctx, Request{Question: "Why?", PlanExcerpt: "Step 1"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
	if client.called {
		t.Fatal("expected client not to be called after context cancellation")
	}
}
