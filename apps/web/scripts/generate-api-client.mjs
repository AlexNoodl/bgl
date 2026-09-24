#!/usr/bin/env node
// Regenerates lib/api/openapi.json and lib/api/schema.d.ts from the backend's
// OpenAPI spec (backend/cmd/openapi-spec — no database or listener needed).
// Run via `pnpm run generate:api`; `predev` runs it automatically before
// `next dev`, and CI runs it explicitly before lint/build
import { execFileSync } from "node:child_process";
import { mkdirSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

const webRoot = path.resolve(fileURLToPath(import.meta.url), "../..");
const backendRoot = path.resolve(webRoot, "../../backend");
const outDir = path.resolve(webRoot, "lib/api");
const specPath = path.join(outDir, "openapi.json");

mkdirSync(outDir, { recursive: true });

const spec = execFileSync("go", ["run", "./cmd/openapi-spec"], { cwd: backendRoot });
writeFileSync(specPath, spec);

execFileSync("pnpm", ["exec", "openapi-typescript", specPath, "-o", path.join(outDir, "schema.d.ts")], {
  cwd: webRoot,
  stdio: "inherit",
});
