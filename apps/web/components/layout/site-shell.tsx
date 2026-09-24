import type { ReactNode } from "react";

import { MobileStub } from "@/components/layout/mobile-stub";
import { SiteHeader } from "@/components/layout/site-header";

export function SiteShell({ children }: { children: ReactNode }) {
  return (
    <>
      <div className="tablet:hidden">
        <MobileStub />
      </div>
      <div className="hidden min-h-screen flex-col tablet:flex">
        <SiteHeader />
        <main className="flex-1">{children}</main>
        {/*Заглушка для футера, пока не нужен*/}
        {/*<SiteFooter />*/}
      </div>
    </>
  );
}
