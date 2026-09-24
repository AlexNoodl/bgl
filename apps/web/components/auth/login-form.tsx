"use client";

import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";

import { apiUrl } from "@/lib/api";

// Временная форма авторизации
export function LoginForm() {
  const router = useRouter();
  const [identifier, setIdentifier] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);
    setIsSubmitting(true);

    try {
      const response = await fetch(apiUrl("/v1/auth/login"), {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "include",
        body: JSON.stringify({ identifier, password }),
      });

      if (!response.ok) {
        setError("Неверный email/username или пароль.");
        return;
      }

      router.push("/");
      router.refresh();
    } catch {
      setError("Не удалось связаться с сервером. Попробуйте позже.");
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="flex flex-col gap-4">
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
