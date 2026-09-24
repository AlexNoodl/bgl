import Link from "next/link";

import { AuthStatus } from "@/components/layout/auth-status";
import { PageContainer } from "@/components/ui/page-container";

const NAV_LINKS = [{ href: "/", label: "Home" }];

export function SiteHeader() {
  return (
    <header className="border-b border-border bg-background">
      <PageContainer className="flex h-14 items-center justify-between py-0">
        <Link href="/" className="font-semibold">
          BGL
        </Link>
        <div className="flex items-center gap-6">
          <nav aria-label="Main" className="flex gap-4 text-14">
            {NAV_LINKS.map((link) => (
              <Link
                key={link.href}
                href={link.href}
                className="text-muted-foreground transition-colors hover:text-foreground"
              >
                {link.label}
              </Link>
            ))}
          </nav>
          <AuthStatus user={null} />
        </div>
      </PageContainer>
    </header>
  );
}
