// Package library implements the library module: a user's tracked games
// (status/rating/notes/playtime/trophies), their game ownership
// ("collection"), lists (including the wishlist), and statistics. It owns
// the `library` PostgreSQL schema — see docs/architecture.md §2.2 and
// docs/decisions/001-modular-monolith-vs-microservices.md.
//
// Data model: docs/database.md §3. Domain decisions on library vs.
// collection vs. wishlist vs. goals: docs/decisions/011-library-collection-wishlist-and-goals-model.md.
//
// Empty skeleton for now (INFRA-001); populated starting with PROFILE-001/LIB-001.
package library
