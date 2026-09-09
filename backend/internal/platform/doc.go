// Package platform holds cross-cutting backend concerns shared by cmd/api
// and cmd/worker: HTTP server/middleware, configuration loading,
// structured logging, telemetry, the background job queue client, and the
// database connection pool. See docs/architecture.md §3.
//
// Empty skeleton for now (INFRA-001); populated starting with INFRA-004
// (logging/request-id middleware) and INFRA-005 (health checks).
package platform
