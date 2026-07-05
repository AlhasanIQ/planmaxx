package appserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AlhasanIQ/planmaxx/internal/sidequestions"
)

func TestInitializeWritesExperimentalCapabilities(t *testing.T) {
	var writes bytes.Buffer
	client := NewClient(bufio.NewReader(strings.NewReader(`{"id":1,"result":{"userAgent":"Codex","codexHome":"/tmp/codex","platformFamily":"unix","platformOs":"macos"}}`+"\n")), &writes)

	res, err := client.Initialize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.UserAgent != "Codex" {
		t.Fatalf("unexpected user agent %q", res.UserAgent)
	}
	if res.CodexHome != "/tmp/codex" {
		t.Fatalf("unexpected codex home %q", res.CodexHome)
	}

	req := decodeRequest(t, writes.Bytes())
	if req["method"] != "initialize" {
		t.Fatalf("expected initialize method, got %v", req["method"])
	}
	params := req["params"].(map[string]any)
	clientInfo := params["clientInfo"].(map[string]any)
	if clientInfo["name"] != "planmaxx" {
		t.Fatalf("expected planmaxx client name, got %v", clientInfo["name"])
	}
	caps := params["capabilities"].(map[string]any)
	if caps["experimentalApi"] != true {
		t.Fatal("expected experimentalApi capability")
	}
	if caps["requestAttestation"] != false {
		t.Fatal("expected requestAttestation false")
	}
	optOut, ok := caps["optOutNotificationMethods"].([]any)
	if !ok {
		t.Fatalf("expected optOutNotificationMethods array, got %T", caps["optOutNotificationMethods"])
	}
	if len(optOut) != 0 {
		t.Fatalf("expected no opt-out notification methods, got %+v", optOut)
	}
}

func TestForkThreadSendsEphemeralRequest(t *testing.T) {
	var writes bytes.Buffer
	client := NewClient(bufio.NewReader(strings.NewReader(`{"id":1,"result":{"thread":{"id":"fork-1","sessionId":"s","forkedFromId":"parent","preview":"","ephemeral":true,"modelProvider":"openai","createdAt":1,"updatedAt":1,"status":{"type":"idle"},"path":null,"cwd":"/repo","cliVersion":"0.133.0","source":"vscode","threadSource":null,"agentNickname":null,"agentRole":null,"gitInfo":null,"name":null,"turns":[]},"model":"gpt-5","modelProvider":"openai","serviceTier":null,"cwd":"/repo","runtimeWorkspaceRoots":["/repo"],"instructionSources":[],"approvalPolicy":"never","approvalsReviewer":"user","sandbox":{"mode":"read-only"},"activePermissionProfile":null,"reasoningEffort":null}}`+"\n")), &writes)

	res, err := client.ForkThread(context.Background(), "parent", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if res.Thread.ID != "fork-1" {
		t.Fatalf("unexpected fork id %q", res.Thread.ID)
	}
	if res.Thread.ForkedFromID != "parent" {
		t.Fatalf("unexpected forked from id %q", res.Thread.ForkedFromID)
	}
	if res.CWD != "/repo" {
		t.Fatalf("unexpected response cwd %q", res.CWD)
	}
	if res.Model != "gpt-5" {
		t.Fatalf("unexpected model %q", res.Model)
	}
	if len(res.RuntimeWorkspaceRoots) != 1 || res.RuntimeWorkspaceRoots[0] != "/repo" {
		t.Fatalf("unexpected workspace roots %+v", res.RuntimeWorkspaceRoots)
	}

	req := decodeRequest(t, writes.Bytes())
	if req["method"] != "thread/fork" {
		t.Fatalf("expected thread/fork method, got %v", req["method"])
	}
	params := req["params"].(map[string]any)
	assertParam(t, params, "threadId", "parent")
	assertParam(t, params, "cwd", "/repo")
	assertParam(t, params, "ephemeral", true)
	assertParam(t, params, "excludeTurns", true)
	assertParam(t, params, "approvalPolicy", "never")
	assertParam(t, params, "persistExtendedHistory", false)
}

func TestReadThreadRequestShape(t *testing.T) {
	var writes bytes.Buffer
	client := NewClient(bufio.NewReader(strings.NewReader(`{"id":1,"result":{"thread":{"id":"thread-1","ephemeral":false,"cwd":"/repo","status":{"type":"idle"}}}}`+"\n")), &writes)

	res, err := client.ReadThread(context.Background(), "thread-1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Thread.ID != "thread-1" {
		t.Fatalf("unexpected thread id %q", res.Thread.ID)
	}

	req := decodeRequest(t, writes.Bytes())
	if req["method"] != "thread/read" {
		t.Fatalf("expected thread/read method, got %v", req["method"])
	}
	params := req["params"].(map[string]any)
	assertParam(t, params, "threadId", "thread-1")
	assertParam(t, params, "includeTurns", false)
}

func TestStartTurnRequestShape(t *testing.T) {
	var writes bytes.Buffer
	client := NewClient(bufio.NewReader(strings.NewReader(`{"id":1,"result":{"turn":{"id":"turn-1","status":"inProgress"}}}`+"\n")), &writes)

	res, err := client.StartTurn(context.Background(), "thread-1", "Explain this plan")
	if err != nil {
		t.Fatal(err)
	}
	if res.Turn.ID != "turn-1" {
		t.Fatalf("unexpected turn id %q", res.Turn.ID)
	}
	if res.Turn.Status != "inProgress" {
		t.Fatalf("unexpected turn status %q", res.Turn.Status)
	}

	req := decodeRequest(t, writes.Bytes())
	if req["method"] != "turn/start" {
		t.Fatalf("expected turn/start method, got %v", req["method"])
	}
	params := req["params"].(map[string]any)
	assertParam(t, params, "threadId", "thread-1")
	assertParam(t, params, "approvalPolicy", "never")
	input := params["input"].([]any)
	item := input[0].(map[string]any)
	assertParam(t, item, "type", "text")
	assertParam(t, item, "text", "Explain this plan")
	textElements := item["text_elements"].([]any)
	if len(textElements) != 0 {
		t.Fatalf("expected no text elements, got %+v", textElements)
	}
}

func TestBuildSideQuestionPromptIncludesProvenance(t *testing.T) {
	prompt := BuildSideQuestionPrompt(sidequestions.Request{
		Question:     "What should move first?",
		FilePath:     "/repo/plan.md",
		Reference:    "/repo/plan.md:10:4-10:7",
		SelectedText: "CLI",
		PlanExcerpt:  "1. CLI\n2. UI",
	})

	for _, want := range []string{
		"PlanMaxx side question",
		"What should move first?",
		"/repo/plan.md",
		"/repo/plan.md:10:4-10:7",
		"CLI",
		"1. CLI",
		"2. UI",
		"selected plan text",
		"Do not change the plan",
		"produce a final review digest",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected prompt to contain %q\n%s", want, prompt)
		}
	}
}

func TestSideQuestionAskerAskPromptUsesCurrentThreadFork(t *testing.T) {
	stream := strings.Join([]string{
		`{"id":1,"result":{"userAgent":"Codex","codexHome":"/tmp/codex","platformFamily":"unix","platformOs":"macos"}}`,
		`{"id":2,"result":{"thread":{"id":"parent","ephemeral":false,"cwd":"/repo","status":{"type":"idle"}}}}`,
		`{"id":3,"result":{"thread":{"id":"fork-1","forkedFromId":"parent","ephemeral":true,"cwd":"/repo","status":{"type":"idle"}},"cwd":"/repo"}}`,
		`{"id":4,"result":{"turn":{"id":"turn-1","status":"inProgress"}}}`,
		`{"method":"item/completed","params":{"threadId":"fork-1","turnId":"turn-1","item":{"type":"agentMessage","text":"SECTION_OK"}}}`,
		`{"method":"turn/completed","params":{"threadId":"fork-1","turn":{"id":"turn-1","status":"completed"}}}`,
		"",
	}, "\n")
	var writes bytes.Buffer
	client := NewClient(bufio.NewReader(strings.NewReader(stream)), &writes)
	asker := &SideQuestionAsker{Client: client, CWD: "/repo", CurrentThreadID: "parent"}

	answer, err := asker.AskPrompt(context.Background(), "Rewrite this section")
	if err != nil {
		t.Fatal(err)
	}
	if answer != "SECTION_OK" {
		t.Fatalf("unexpected answer %q", answer)
	}
	requests := decodeRequests(t, writes.Bytes())
	if methods := requestMethods(requests); strings.Join(methods, ",") != "initialize,initialized,thread/read,thread/fork,turn/start" {
		t.Fatalf("unexpected app-server methods %v", methods)
	}
	start := requests[4]
	params := start["params"].(map[string]any)
	input := params["input"].([]any)[0].(map[string]any)
	if input["text"] != "Rewrite this section" {
		t.Fatalf("expected raw prompt to be sent, got %v", input["text"])
	}
}

func TestStartTurnAndWaitReturnsAgentMessage(t *testing.T) {
	stream := strings.Join([]string{
		`{"id":1,"result":{"turn":{"id":"turn-1","status":"inProgress"}}}`,
		`{"method":"item/agentMessage/delta","params":{"threadId":"fork-1","turnId":"turn-1","itemId":"item-1","delta":"PLANMAXX_"}}`,
		`{"method":"item/completed","params":{"threadId":"fork-1","turnId":"turn-1","item":{"type":"agentMessage","id":"item-1","text":"PLANMAXX_SIDE_QUESTION_OK"},"completedAtMs":1}}`,
		`{"method":"turn/completed","params":{"threadId":"fork-1","turn":{"id":"turn-1","status":"completed"}}}`,
		"",
	}, "\n")
	var writes bytes.Buffer
	client := NewClient(bufio.NewReader(strings.NewReader(stream)), &writes)

	answer, err := client.StartTurnAndWait(context.Background(), "fork-1", "question")
	if err != nil {
		t.Fatal(err)
	}
	if answer != "PLANMAXX_SIDE_QUESTION_OK" {
		t.Fatalf("unexpected answer %q", answer)
	}
}

func TestStartTurnAndWaitIgnoresUnrelatedNotifications(t *testing.T) {
	stream := strings.Join([]string{
		`{"id":1,"result":{"turn":{"id":"turn-1","status":"inProgress"}}}`,
		`{"method":"item/completed","params":{"threadId":"other-thread","turnId":"turn-1","item":{"type":"agentMessage","text":"wrong thread"}}}`,
		`{"method":"item/completed","params":{"threadId":"fork-1","turnId":"other-turn","item":{"type":"agentMessage","text":"wrong turn"}}}`,
		`{"method":"turn/completed","params":{"threadId":"fork-1","turn":{"id":"other-turn","status":"completed"}}}`,
		`{"method":"item/agentMessage/delta","params":{"threadId":"fork-1","turnId":"turn-1","delta":"right answer"}}`,
		`{"method":"turn/completed","params":{"threadId":"fork-1","turn":{"id":"turn-1","status":"completed"}}}`,
		"",
	}, "\n")
	client := NewClient(bufio.NewReader(strings.NewReader(stream)), &bytes.Buffer{})

	answer, err := client.StartTurnAndWait(context.Background(), "fork-1", "question")
	if err != nil {
		t.Fatal(err)
	}
	if answer != "right answer" {
		t.Fatalf("unexpected answer %q", answer)
	}
}

func TestStartTurnAndWaitErrorsWhenTurnCompletesWithoutAnswer(t *testing.T) {
	stream := strings.Join([]string{
		`{"id":1,"result":{"turn":{"id":"turn-1","status":"inProgress"}}}`,
		`{"method":"turn/completed","params":{"threadId":"fork-1","turn":{"id":"turn-1","status":"completed"}}}`,
		"",
	}, "\n")
	client := NewClient(bufio.NewReader(strings.NewReader(stream)), &bytes.Buffer{})

	_, err := client.StartTurnAndWait(context.Background(), "fork-1", "question")
	if err == nil {
		t.Fatal("expected missing answer error")
	}
	if !strings.Contains(err.Error(), "no agent answer") {
		t.Fatalf("expected missing answer error, got %q", err.Error())
	}
}

func TestStartTurnAndWaitErrorsWhenTurnDoesNotCompleteSuccessfully(t *testing.T) {
	stream := strings.Join([]string{
		`{"id":1,"result":{"turn":{"id":"turn-1","status":"inProgress"}}}`,
		`{"method":"item/completed","params":{"threadId":"fork-1","turnId":"turn-1","item":{"type":"agentMessage","text":"partial answer"}}}`,
		`{"method":"turn/completed","params":{"threadId":"fork-1","turn":{"id":"turn-1","status":"failed"}}}`,
		"",
	}, "\n")
	client := NewClient(bufio.NewReader(strings.NewReader(stream)), &bytes.Buffer{})

	_, err := client.StartTurnAndWait(context.Background(), "fork-1", "question")
	if err == nil {
		t.Fatal("expected failed turn error")
	}
	if !strings.Contains(err.Error(), "status failed") {
		t.Fatalf("expected failed status error, got %q", err.Error())
	}
}

func TestStartTurnAndWaitCancellationReturnsWhenResponseStalls(t *testing.T) {
	reader, responseWriter := io.Pipe()
	t.Cleanup(func() {
		_ = responseWriter.Close()
	})
	writes := &recordingWriter{}
	client := NewClient(bufio.NewReader(reader), writes)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		_, err := client.StartTurnAndWait(ctx, "fork-1", "question")
		errCh <- err
	}()

	writes.waitForWrites(t, 1)

	select {
	case err := <-errCh:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected deadline exceeded, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for stalled StartTurnAndWait cancellation")
	}
}

func TestStartTurnAndWaitCancellationReturnsWhenCompletionStalls(t *testing.T) {
	reader, responseWriter := io.Pipe()
	t.Cleanup(func() {
		_ = responseWriter.Close()
	})
	writes := &recordingWriter{}
	client := NewClient(bufio.NewReader(reader), writes)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		_, err := client.StartTurnAndWait(ctx, "fork-1", "question")
		errCh <- err
	}()

	writes.waitForWrites(t, 1)
	writeLine(t, responseWriter, `{"id":1,"result":{"turn":{"id":"turn-1","status":"inProgress"}}}`)

	select {
	case err := <-errCh:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected deadline exceeded, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for stalled turn completion cancellation")
	}
}

func TestCallCancellationReturnsWhileWaitingForOperationSlot(t *testing.T) {
	reader, responseWriter := io.Pipe()
	t.Cleanup(func() {
		_ = responseWriter.Close()
	})
	writes := &recordingWriter{}
	client := NewClient(bufio.NewReader(reader), writes)

	firstErrCh := make(chan error, 1)
	go func() {
		_, err := client.StartTurnAndWait(context.Background(), "fork-1", "question")
		firstErrCh <- err
	}()
	writes.waitForWrites(t, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		_, err := client.ReadThread(ctx, "thread-1")
		errCh <- err
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected deadline exceeded, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for operation-slot cancellation")
	}
	writes.assertWriteCountStays(t, 1, 50*time.Millisecond)

	writeLine(t, responseWriter, `{"id":1,"result":{"turn":{"id":"turn-1","status":"inProgress"}}}`)
	writeLine(t, responseWriter, `{"method":"item/completed","params":{"threadId":"fork-1","turnId":"turn-1","item":{"type":"agentMessage","text":"answer 1"}}}`)
	writeLine(t, responseWriter, `{"method":"turn/completed","params":{"threadId":"fork-1","turn":{"id":"turn-1","status":"completed"}}}`)
	select {
	case err := <-firstErrCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for first operation cleanup")
	}
}

func TestStartTurnAndWaitCancellationDoesNotWedgeFollowingRequest(t *testing.T) {
	reader, responseWriter := io.Pipe()
	t.Cleanup(func() {
		_ = responseWriter.Close()
	})
	writes := &recordingWriter{}
	client := NewClient(bufio.NewReader(reader), writes)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		_, err := client.StartTurnAndWait(ctx, "fork-1", "question 1")
		errCh <- err
	}()

	writes.waitForWrites(t, 1)
	select {
	case err := <-errCh:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected deadline exceeded, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for first StartTurnAndWait cancellation")
	}

	readErrCh := make(chan error, 1)
	go func() {
		res, err := client.ReadThread(context.Background(), "thread-1")
		if err == nil && res.Thread.ID != "thread-1" {
			err = errString("unexpected thread response " + res.Thread.ID)
		}
		readErrCh <- err
	}()

	writes.waitForWrites(t, 2)
	writeLine(t, responseWriter, `{"id":2,"result":{"thread":{"id":"thread-1","status":{"type":"idle"}}}}`)

	select {
	case err := <-readErrCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for request after canceled StartTurnAndWait")
	}
}

func TestSideQuestionAskerInitializesReadsForksAndStartsTurn(t *testing.T) {
	stream := strings.Join([]string{
		`{"id":1,"result":{"userAgent":"Codex","codexHome":"/tmp/codex"}}`,
		`{"id":2,"result":{"thread":{"id":"current-thread","ephemeral":false,"cwd":"/repo","status":{"type":"idle"}}}}`,
		`{"id":3,"result":{"thread":{"id":"fork-1","forkedFromId":"current-thread","ephemeral":true,"cwd":"/repo","status":{"type":"idle"}},"cwd":"/repo"}}`,
		`{"id":4,"result":{"turn":{"id":"turn-1","status":"inProgress"}}}`,
		`{"method":"item/completed","params":{"threadId":"fork-1","turnId":"turn-1","item":{"type":"agentMessage","text":"Use the CLI first."}}}`,
		`{"method":"turn/completed","params":{"threadId":"fork-1","turn":{"id":"turn-1","status":"completed"}}}`,
		"",
	}, "\n")
	var writes bytes.Buffer
	client := NewClient(bufio.NewReader(strings.NewReader(stream)), &writes)
	asker := &SideQuestionAsker{Client: client, CWD: "/repo"}

	answer, err := asker.Ask(context.Background(), sidequestions.Request{
		ThreadID:     "current-thread",
		Question:     "What first?",
		FilePath:     "/repo/plan.md",
		Reference:    "/repo/plan.md:5:1-5:6",
		SelectedText: "1. CLI",
		PlanExcerpt:  "1. CLI",
	})
	if err != nil {
		t.Fatal(err)
	}
	if answer != "Use the CLI first." {
		t.Fatalf("unexpected answer %q", answer)
	}

	requests := decodeRequests(t, writes.Bytes())
	methods := make([]any, 0, len(requests))
	for _, req := range requests {
		methods = append(methods, req["method"])
	}
	wantMethods := []any{"initialize", "initialized", "thread/read", "thread/fork", "turn/start"}
	if !sameMethods(methods, wantMethods) {
		t.Fatalf("expected methods %v, got %v", wantMethods, methods)
	}
	if _, ok := requests[1]["id"]; ok {
		t.Fatalf("expected initialized notification without id, got %+v", requests[1])
	}
	readParams := requests[2]["params"].(map[string]any)
	assertParam(t, readParams, "threadId", "current-thread")
	forkParams := requests[3]["params"].(map[string]any)
	assertParam(t, forkParams, "threadId", "current-thread")
	assertParam(t, forkParams, "cwd", "/repo")
	assertParam(t, forkParams, "ephemeral", true)
	assertParam(t, forkParams, "excludeTurns", true)
	turnParams := requests[4]["params"].(map[string]any)
	assertParam(t, turnParams, "threadId", "fork-1")
	input := turnParams["input"].([]any)
	prompt := input[0].(map[string]any)["text"].(string)
	if !strings.Contains(prompt, "What first?") ||
		!strings.Contains(prompt, "/repo/plan.md:5:1-5:6") ||
		!strings.Contains(prompt, "Selected text:\n1. CLI") {
		t.Fatalf("expected prompt context in turn/start, got %q", prompt)
	}
}

func TestConcurrentStartTurnAndWaitCallsAreSerializedThroughCompletion(t *testing.T) {
	reader, responseWriter := io.Pipe()
	writes := &recordingWriter{}
	client := NewClient(bufio.NewReader(reader), writes)

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)

	go func() {
		defer wg.Done()
		answer, err := client.StartTurnAndWait(context.Background(), "fork-1", "question 1")
		if err == nil && answer != "answer 1" {
			err = errString("unexpected first answer " + answer)
		}
		errs <- err
	}()

	writes.waitForWrites(t, 1)

	go func() {
		defer wg.Done()
		answer, err := client.StartTurnAndWait(context.Background(), "fork-2", "question 2")
		if err == nil && answer != "answer 2" {
			err = errString("unexpected second answer " + answer)
		}
		errs <- err
	}()

	writes.assertWriteCountStays(t, 1, 50*time.Millisecond)

	writeLine(t, responseWriter, `{"id":1,"result":{"turn":{"id":"turn-1","status":"inProgress"}}}`)
	writeLine(t, responseWriter, `{"method":"item/completed","params":{"threadId":"fork-1","turnId":"turn-1","item":{"type":"agentMessage","text":"answer 1"}}}`)
	writeLine(t, responseWriter, `{"method":"turn/completed","params":{"threadId":"fork-1","turn":{"id":"turn-1","status":"completed"}}}`)

	writes.waitForWrites(t, 2)
	writeLine(t, responseWriter, `{"id":2,"result":{"turn":{"id":"turn-2","status":"inProgress"}}}`)
	writeLine(t, responseWriter, `{"method":"item/completed","params":{"threadId":"fork-2","turnId":"turn-2","item":{"type":"agentMessage","text":"answer 2"}}}`)
	writeLine(t, responseWriter, `{"method":"turn/completed","params":{"threadId":"fork-2","turn":{"id":"turn-2","status":"completed"}}}`)

	wg.Wait()
	close(errs)
	if err := responseWriter.Close(); err != nil {
		t.Fatal(err)
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	requests := decodeRequests(t, writes.bytes())
	if requests[0]["method"] != "turn/start" || requests[1]["method"] != "turn/start" {
		t.Fatalf("expected turn/start requests, got %+v", requests)
	}
}

func TestAppServerErrorIncludesMethodAndMessage(t *testing.T) {
	client := NewClient(bufio.NewReader(strings.NewReader(`{"id":1,"error":{"code":-32602,"message":"excludeTurns requires experimental API"}}`+"\n")), &bytes.Buffer{})

	_, err := client.ForkThread(context.Background(), "parent", "/repo")
	if err == nil {
		t.Fatal("expected app-server error")
	}
	if !strings.Contains(err.Error(), "thread/fork") {
		t.Fatalf("expected method in error, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "excludeTurns requires experimental API") {
		t.Fatalf("expected app-server message in error, got %q", err.Error())
	}
}

func TestCallIgnoresUnrelatedNotificationAndOtherID(t *testing.T) {
	input := strings.Join([]string{
		`{"method":"log/message","params":{"message":"noise"}}`,
		`{"id":99,"result":{"userAgent":"Wrong"}}`,
		`{"id":"unrelated","result":{"userAgent":"Also wrong"}}`,
		`{"id":1,"result":{"userAgent":"Codex","codexHome":"/tmp/codex"}}`,
		"",
	}, "\n")
	client := NewClient(bufio.NewReader(strings.NewReader(input)), &bytes.Buffer{})

	res, err := client.Initialize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.UserAgent != "Codex" {
		t.Fatalf("unexpected user agent %q", res.UserAgent)
	}
}

func TestMalformedMatchingResultReturnsDecodeError(t *testing.T) {
	client := NewClient(bufio.NewReader(strings.NewReader(`{"id":1,"result":{"thread":42}}`+"\n")), &bytes.Buffer{})

	_, err := client.ReadThread(context.Background(), "thread-1")
	if err == nil {
		t.Fatal("expected decode error")
	}
	if !strings.Contains(err.Error(), "decode app-server thread/read result") {
		t.Fatalf("expected method decode error, got %q", err.Error())
	}
}

func TestInitializedWritesNotificationWithoutID(t *testing.T) {
	var writes bytes.Buffer
	client := NewClient(bufio.NewReader(strings.NewReader("")), &writes)

	if err := client.NotifyInitialized(context.Background()); err != nil {
		t.Fatal(err)
	}

	req := decodeRequest(t, writes.Bytes())
	if req["method"] != "initialized" {
		t.Fatalf("expected initialized method, got %v", req["method"])
	}
	if _, ok := req["id"]; ok {
		t.Fatalf("expected notification without id, got %+v", req)
	}
	if _, ok := req["params"]; ok {
		t.Fatalf("expected notification without params, got %+v", req)
	}
}

func TestConcurrentCallsAreSerializedAndDoNotLoseResponses(t *testing.T) {
	reader, responseWriter := io.Pipe()
	writes := &recordingWriter{}
	client := NewClient(bufio.NewReader(reader), writes)

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)

	go func() {
		defer wg.Done()
		res, err := client.Initialize(context.Background())
		if err == nil && res.UserAgent != "Codex" {
			err = errString("unexpected initialize response " + res.UserAgent)
		}
		errs <- err
	}()

	writes.waitForWrites(t, 1)

	go func() {
		defer wg.Done()
		res, err := client.ReadThread(context.Background(), "thread-1")
		if err == nil && res.Thread.ID != "thread-1" {
			err = errString("unexpected thread response " + res.Thread.ID)
		}
		errs <- err
	}()

	writes.assertWriteCountStays(t, 1, 50*time.Millisecond)

	if _, err := responseWriter.Write([]byte(`{"id":1,"result":{"userAgent":"Codex"}}` + "\n")); err != nil {
		t.Fatal(err)
	}
	writes.waitForWrites(t, 2)
	if _, err := responseWriter.Write([]byte(`{"id":2,"result":{"thread":{"id":"thread-1","status":{"type":"idle"}}}}` + "\n")); err != nil {
		t.Fatal(err)
	}

	wg.Wait()
	close(errs)
	if err := responseWriter.Close(); err != nil {
		t.Fatal(err)
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	requests := decodeRequests(t, writes.bytes())
	if requests[0]["method"] != "initialize" {
		t.Fatalf("expected first request initialize, got %v", requests[0]["method"])
	}
	if requests[1]["method"] != "thread/read" {
		t.Fatalf("expected second request thread/read, got %v", requests[1]["method"])
	}
}

func TestSequentialRequestsAdvanceIDs(t *testing.T) {
	var writes bytes.Buffer
	input := strings.Join([]string{
		`{"id":1,"result":{"userAgent":"Codex"}}`,
		`{"id":2,"result":{"thread":{"id":"thread-1","status":{"type":"idle"}}}}`,
		"",
	}, "\n")
	client := NewClient(bufio.NewReader(strings.NewReader(input)), &writes)

	if _, err := client.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ReadThread(context.Background(), "thread-1"); err != nil {
		t.Fatal(err)
	}

	requests := decodeRequests(t, writes.Bytes())
	if requests[0]["id"] != float64(1) {
		t.Fatalf("expected first request id 1, got %v", requests[0]["id"])
	}
	if requests[1]["id"] != float64(2) {
		t.Fatalf("expected second request id 2, got %v", requests[1]["id"])
	}
}

func TestStringJSONRPCIDMatchesNumericRequestID(t *testing.T) {
	client := NewClient(bufio.NewReader(strings.NewReader(`{"id":"1","result":{"userAgent":"Codex"}}`+"\n")), &bytes.Buffer{})

	res, err := client.Initialize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.UserAgent != "Codex" {
		t.Fatalf("unexpected user agent %q", res.UserAgent)
	}
}

func decodeRequest(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var req map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(data), &req); err != nil {
		t.Fatal(err)
	}
	return req
}

func decodeRequests(t *testing.T, data []byte) []map[string]any {
	t.Helper()
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	requests := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var req map[string]any
		if err := json.Unmarshal(line, &req); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, req)
	}
	return requests
}

func assertParam(t *testing.T, params map[string]any, key string, want any) {
	t.Helper()
	if params[key] != want {
		t.Fatalf("expected %s=%v, got %v", key, want, params[key])
	}
}

func sameMethods(got []any, want []any) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func requestMethods(requests []map[string]any) []string {
	methods := make([]string, 0, len(requests))
	for _, req := range requests {
		method, _ := req["method"].(string)
		methods = append(methods, method)
	}
	return methods
}

func writeLine(t *testing.T, w io.Writer, line string) {
	t.Helper()
	if _, err := w.Write([]byte(line + "\n")); err != nil {
		t.Fatal(err)
	}
}

type recordingWriter struct {
	mu     sync.Mutex
	buffer bytes.Buffer
	writes int
}

func (w *recordingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.buffer.Write(p)
	if err == nil {
		w.writes++
	}
	return n, err
}

func (w *recordingWriter) waitForWrites(t *testing.T, count int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		w.mu.Lock()
		writes := w.writes
		w.mu.Unlock()
		if writes >= count {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d writes, got %d", count, writes)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (w *recordingWriter) assertWriteCountStays(t *testing.T, count int, duration time.Duration) {
	t.Helper()
	time.Sleep(duration)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.writes != count {
		t.Fatalf("expected %d writes, got %d", count, w.writes)
	}
}

func (w *recordingWriter) bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.buffer.Bytes()...)
}

type errString string

func (e errString) Error() string {
	return string(e)
}
