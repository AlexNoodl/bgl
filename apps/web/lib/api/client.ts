import createClient from "openapi-fetch";

import { API_BASE_URL } from "@/lib/api";

import type { paths } from "./schema";

export const apiClient = createClient<paths>({ baseUrl: API_BASE_URL, credentials: "include" });
