"use client";

import { useRouter } from "next/navigation";
import { type SubmitEvent, useState } from "react";

import { apiClient } from "@/lib/api/client";

// Временная форма авторизации
export function LoginForm() {
  const router = useRouter();
  const [identifier, setIdentifier] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [welcomeMessage, setWelcomeMessage] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);

  async function handleSubmit(event: SubmitEvent) {
    event.preventDefault();
    setError(null);
    setWelcomeMessage(null);
    setIsSubmitting(true);

    try {
      const { data, error: apiError } = await apiClient.POST("/v1/auth/login", {
        body: { identifier, password },
      });

      if (apiError) {
        setError(apiError.error.message);
        return;
      }

      setWelcomeMessage(`Добро пожаловать, ${data.username}!`);
      router.push("/");
      router.refresh();
    } catch {
      setError("Не удалось связаться с сервером. Попробуйте позже.");
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <form onSubmit={(event) => handleSubmit(event)} className="flex flex-col gap-4">
      <div className="flex flex-col gap-1.5">
        <label htmlFor="identifier" className="text-14 font-medium text-foreground">
          Email или username
        </label>
        <input
          id="identifier"
          name="identifier"
          type="text"
          autoComplete="username"
          required
          value={identifier}
          onChange={(event) => setIdentifier(event.target.value)}
          className="rounded-md border border-input bg-background px-3 py-2 text-14 text-foreground outline-none focus:ring-2 focus:ring-ring"
        />
      </div>
      <div className="flex flex-col gap-1.5">
        <label htmlFor="password" className="text-14 font-medium text-foreground">
          Пароль
        </label>
        <input
          id="password"
          name="password"
          type="password"
          autoComplete="current-password"
          required
          value={password}
          onChange={(event) => setPassword(event.target.value)}
          className="rounded-md border border-input bg-background px-3 py-2 text-14 text-foreground outline-none focus:ring-2 focus:ring-ring"
        />
      </div>
      {error ? <p className="text-14 text-destructive">{error}</p> : null}
      {welcomeMessage ? <p className="text-14 text-foreground">{welcomeMessage}</p> : null}
      <button
        type="submit"
        disabled={isSubmitting}
        className="rounded-md bg-primary px-3 py-2 text-14 font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:opacity-50"
      >
        {isSubmitting ? "Вход…" : "Войти"}
      </button>
    </form>
  );
}
