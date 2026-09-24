import Link from "next/link";

export interface AuthStatusUser {
  displayName: string;
  avatarUrl?: string | null;
}

interface AuthStatusProps {
  user: AuthStatusUser | null;
}

export function AuthStatus({ user }: AuthStatusProps) {
  if (!user) {
    return (
      <Link
        href="/login"
        className="rounded-md bg-primary px-3 py-1.5 text-14 font-medium text-primary-foreground transition-colors hover:bg-primary/90"
      >
        Войти
      </Link>
    );
  }

  return (
    <span
      title={user.displayName}
      className="flex size-8 shrink-0 items-center justify-center overflow-hidden rounded-full bg-muted text-14 font-medium text-muted-foreground"
    >
      {user.avatarUrl ? (
        // eslint-disable-next-line @next/next/no-img-element -- arbitrary user-hosted avatar URLs, not part of the local image domain allowlist
        <img src={user.avatarUrl} alt={user.displayName} className="size-full object-cover" />
      ) : (
        user.displayName.slice(0, 1).toUpperCase()
      )}
    </span>
  );
}
