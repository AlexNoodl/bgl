package auth

import (
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

func newTestAPI(t *testing.T, register func(api huma.API)) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	cfg := huma.DefaultConfig("test", "0.0.0")
	cfg.CreateHooks = nil
	api := humago.New(mux, cfg)
	register(api)
	return mux
}
