// Command stubplugin is a test producer: `stubplugin uir` answers the uir-producer/1 protocol, with
// the publication in $STUB_PUBLICATION and a failure injected by $STUB_MODE.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

type request struct {
	ID     int    `json:"id"`
	Method string `json:"method"`
}

func reply(id int, result any, rpcError any) {
	out, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result, "error": rpcError})
	if err != nil {
		panic(err)
	}
	fmt.Println(string(out))
}

func main() {
	if len(os.Args) != 2 || os.Args[1] != "uir" {
		fmt.Fprintln(os.Stderr, "usage: stubplugin uir")
		os.Exit(2)
	}
	mode := os.Getenv("STUB_MODE")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1<<20), 64<<20)
	for scanner.Scan() {
		var call request
		if err := json.Unmarshal(scanner.Bytes(), &call); err != nil {
			panic(err)
		}
		switch call.Method {
		case "initialize":
			protocol := "uir-producer/1"
			if mode == "wrong-protocol" {
				protocol = "uir-producer/0"
			}
			if mode == "garbage" {
				fmt.Println("this is not json")
				continue
			}
			reply(call.ID, map[string]any{"name": "stub", "version": "9.9.9", "protocol": protocol, "methods": []string{"initialize", "publication", "shutdown"}}, nil)
		case "publication":
			switch mode {
			case "crash":
				fmt.Fprintln(os.Stderr, "stub: boom while scanning")
				os.Exit(3)
			case "rpc-error":
				fmt.Fprintln(os.Stderr, "stub: GITHUB_TOKEN is not set")
				reply(call.ID, nil, map[string]any{"code": -32000, "message": "security scan failed", "data": "set GITHUB_TOKEN"})
				continue
			}
			encoded, err := os.ReadFile(os.Getenv("STUB_PUBLICATION"))
			if err != nil {
				panic(err)
			}
			reply(call.ID, json.RawMessage(encoded), nil)
		case "shutdown":
			reply(call.ID, nil, nil)
			return
		}
	}
}
