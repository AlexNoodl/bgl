MIGRATIONS_DIR := migrations

.PHONY: migrate-up migrate-down migrate-status migrate-create

migrate-up:
	@test -n "$$DATABASE_URL" || (echo "DATABASE_URL is not set" >&2; exit 1)
	goose -dir $(MIGRATIONS_DIR) postgres "$$DATABASE_URL" up

migrate-down:
	@test -n "$$DATABASE_URL" || (echo "DATABASE_URL is not set" >&2; exit 1)
	goose -dir $(MIGRATIONS_DIR) postgres "$$DATABASE_URL" down

migrate-status:
	@test -n "$$DATABASE_URL" || (echo "DATABASE_URL is not set" >&2; exit 1)
	goose -dir $(MIGRATIONS_DIR) postgres "$$DATABASE_URL" status

# Usage: make migrate-create name=add_something
migrate-create:
	@test -n "$(name)" || (echo "usage: make migrate-create name=add_something" >&2; exit 1)
	goose -dir $(MIGRATIONS_DIR) -s create $(name) sql
