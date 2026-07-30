package grokbuild

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRelocateSessionWithACPUsesExactCrossCWDContract(t *testing.T) {
	binDir := t.TempDir()
	wrapper := fmt.Sprintf(
		"#!/bin/sh\nPLANMAXX_GROKBUILD_ACP_HELPER=1 exec %q -test.run '^TestGrokACPHelperProcess$'\n",
		os.Args[0],
	)
	if err := os.WriteFile(filepath.Join(binDir, "grok"), []byte(wrapper), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	root := t.TempDir()
	isolation := &Isolation{
		HomeDir:                filepath.Join(root, "home"),
		GrokHome:               filepath.Join(root, "home", ".grok"),
		WorkspaceRoot:          filepath.Join(root, "workspace"),
		WorkingDirectory:       filepath.Join(root, "workspace", "services", "api"),
		SourceRoot:             "/repo",
		SourceWorkingDirectory: "/repo/services/api",
	}
	for _, directory := range []string{
		isolation.GrokHome,
		isolation.WorkspaceRoot,
		isolation.WorkingDirectory,
		filepath.Join(root, "tmp"),
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	created, err := relocateSessionWithACP(context.Background(), isolation, sourceSessionID, forkSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("ACP relocation did not report the created fork")
	}
}

func TestRelocateSessionWithACPRequiresCleanupWhenForkResponseIsLost(t *testing.T) {
	binDir := t.TempDir()
	wrapper := fmt.Sprintf(
		"#!/bin/sh\nPLANMAXX_GROKBUILD_ACP_HELPER=1 exec %q -test.run '^TestGrokACPHelperProcess$'\n",
		os.Args[0],
	)
	if err := os.WriteFile(filepath.Join(binDir, "grok"), []byte(wrapper), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PLANMAXX_GROKBUILD_DROP_FORK_RESPONSE", "1")
	root := t.TempDir()
	isolation := &Isolation{
		HomeDir:                filepath.Join(root, "home"),
		GrokHome:               filepath.Join(root, "home", ".grok"),
		WorkspaceRoot:          filepath.Join(root, "workspace"),
		WorkingDirectory:       filepath.Join(root, "workspace"),
		SourceRoot:             "/repo",
		SourceWorkingDirectory: "/repo",
	}
	for _, directory := range []string{
		isolation.GrokHome,
		isolation.WorkspaceRoot,
		filepath.Join(root, "tmp"),
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	created, err := relocateSessionWithACP(context.Background(), isolation, sourceSessionID, forkSessionID)
	if err == nil || !created {
		t.Fatalf("lost fork response must require cleanup: created=%v err=%v", created, err)
	}
}

func TestGrokACPHelperProcess(t *testing.T) {
	if os.Getenv("PLANMAXX_GROKBUILD_ACP_HELPER") != "1" {
		return
	}
	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for {
		var request struct {
			ID     int            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := decoder.Decode(&request); err != nil {
			return
		}
		switch request.Method {
		case "initialize":
			if request.Params["protocolVersion"] != float64(1) {
				t.Fatalf("unexpected initialize request: %+v", request)
			}
			_ = encoder.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      request.ID,
				"result": map[string]any{
					"protocolVersion":   1,
					"agentCapabilities": map[string]any{"loadSession": true},
					"_meta":             map[string]any{"grokShell": true},
				},
			})
		case "_x.ai/session/fork":
			if request.Params["sourceSessionId"] != sourceSessionID ||
				request.Params["sourceCwd"] != "/repo/services/api" ||
				request.Params["sourceWorkspaceDir"] != "/repo" ||
				request.Params["newSessionId"] != forkSessionID ||
				!strings.HasSuffix(request.Params["newCwd"].(string), filepath.Join("services", "api")) {
				t.Fatalf("unexpected fork request: %+v", request.Params)
			}
			if os.Getenv("PLANMAXX_GROKBUILD_DROP_FORK_RESPONSE") == "1" {
				return
			}
			_ = encoder.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      request.ID,
				"result": map[string]any{
					"newSessionId":       forkSessionID,
					"chatMessagesCopied": 1,
					"updatesCopied":      0,
					"planStateCopied":    false,
					"newCwd":             request.Params["newCwd"],
					"parentSessionId":    sourceSessionID,
				},
			})
		default:
			t.Fatalf("unexpected ACP method %q", request.Method)
		}
	}
}
