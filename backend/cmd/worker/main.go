// Command worker is the entry point for the background job worker
// (backend/cmd/worker). It is built from the same module code as cmd/api
// (docs/architecture.md §3) but deployed as a separate process.
//
// This is currently a bare skeleton (INFRA-001). Real behaviour (job queue
// consumption via river, IGDB sync, search index maintenance) is added
// starting with INFRA-007.
package main

import "log"

func main() {
	log.Println("worker: skeleton — not yet implemented (see INFRA-007)")
}
