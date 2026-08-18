package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println("codex-cli planmaxx-windows-test")
		return
	}
	if len(os.Args) != 4 || os.Args[1] != "app-server" || os.Args[2] != "--listen" || os.Args[3] != "stdio://" {
		fmt.Fprintln(os.Stderr, "unexpected fake Codex arguments")
		os.Exit(2)
	}

	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request struct {
			ID     any            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			panic(err)
		}
		switch request.Method {
		case "initialize":
			respond(encoder, request.ID, map[string]any{"userAgent": "Codex Windows test", "platformOs": "windows"})
		case "initialized":
			continue
		case "thread/read":
			threadID, _ := request.Params["threadId"].(string)
			respond(encoder, request.ID, map[string]any{"thread": map[string]any{"id": threadID, "status": map[string]any{"type": "idle"}}})
		case "thread/fork":
			sourceID, _ := request.Params["threadId"].(string)
			respond(encoder, request.ID, map[string]any{
				"thread":         map[string]any{"id": "fork-1", "forkedFromId": sourceID, "ephemeral": true, "status": map[string]any{"type": "idle"}},
				"approvalPolicy": "never",
				"sandbox":        map[string]any{"type": "readOnly", "networkAccess": false},
			})
		case "turn/start":
			respond(encoder, request.ID, map[string]any{"turn": map[string]any{"id": "turn-1", "status": "inProgress"}})
			_ = encoder.Encode(map[string]any{"method": "item/completed", "params": map[string]any{"threadId": "fork-1", "turnId": "turn-1", "item": map[string]any{"type": "agentMessage", "text": "Windows app-server answer."}}})
			_ = encoder.Encode(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "fork-1", "turn": map[string]any{"id": "turn-1", "status": "completed"}}})
		default:
			fmt.Fprintf(os.Stderr, "unexpected method %s\n", request.Method)
			os.Exit(2)
		}
	}
	if err := scanner.Err(); err != nil {
		panic(err)
	}
}

func respond(encoder *json.Encoder, id any, result any) {
	if err := encoder.Encode(map[string]any{"id": id, "result": result}); err != nil {
		panic(err)
	}
}
