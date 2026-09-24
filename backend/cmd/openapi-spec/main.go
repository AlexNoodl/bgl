package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"bgl/internal/apiserver"
)

func main() {
	mux := http.NewServeMux()
	api := apiserver.BuildAPI(mux, apiserver.Deps{})

	b, err := json.MarshalIndent(api.OpenAPI(), "", "  ")
	if err != nil {
		log.Fatalf("openapi-spec: marshaling spec: %v", err)
	}
	if _, err := os.Stdout.Write(b); err != nil {
		log.Fatalf("openapi-spec: writing spec: %v", err)
	}
}
