// Command api is the entry point for the HTTP API server (backend/cmd/api).
//
// This is currently a bare skeleton (INFRA-001): it exists so that the
// repository layout and Go module build end-to-end. Real behaviour
// (HTTP server, routing, middleware, config loading) is added starting
// with INFRA-004/INFRA-005, following docs/architecture.md §3.
package main

import "log"

func main() {
	log.Println("api: skeleton — not yet implemented (see INFRA-004, INFRA-005)")
}
