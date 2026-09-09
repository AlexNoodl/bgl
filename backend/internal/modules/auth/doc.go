// Package auth implements the auth module: user identity and credentials,
// sessions, OAuth account linking, and email verification / password reset
// tokens. It owns the `auth` PostgreSQL schema and never allows other
// modules to query its tables directly — see docs/architecture.md §2.2
// and docs/decisions/001-modular-monolith-vs-microservices.md.
//
// Data model: docs/database.md §2. Design: docs/decisions/003-authentication.md.
//
// Empty skeleton for now (INFRA-001); populated starting with AUTH-001.
package auth
