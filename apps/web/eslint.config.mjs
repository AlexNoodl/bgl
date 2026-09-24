import { defineConfig, globalIgnores } from "eslint/config";
import nextPlugin from "eslint-config-next";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";

export default defineConfig([
  ...nextPlugin,
  ...nextVitals,
  ...nextTs,
  globalIgnores([".next/**", "out/**", "build/**", "node_modules/**", "next-env.d.ts", "lib/api/schema.d.ts"]),
]);
