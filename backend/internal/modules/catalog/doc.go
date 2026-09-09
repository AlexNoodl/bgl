// Package catalog implements the catalog module: game metadata, search,
// and IGDB integration. It owns the `catalog` PostgreSQL schema — see
// docs/architecture.md §2.2 and docs/decisions/001-modular-monolith-vs-microservices.md.
//
// Data model: docs/database.md §4. Search design: docs/decisions/005-search-postgres-first.md.
// IGDB integration: docs/decisions/006-igdb-integration.md.
//
// Empty skeleton for now (INFRA-001); populated starting with GAME-001.
package catalog
