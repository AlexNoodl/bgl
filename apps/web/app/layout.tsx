import type { Metadata } from "next";
import { Geist } from "next/font/google";
import type { ReactNode } from "react";

import { SiteShell } from "@/components/layout/site-shell";

import "./globals.css";

const geist = Geist({
  subsets: ["latin", "cyrillic"],
  weight: ["400", "500", "600"],
  variable: "--font-geist",
});

export const metadata: Metadata = {
  title: "BGL",
  description: "Your video games library",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: ReactNode;
}>) {
  return (
    <html lang="en" className={geist.variable}>
      <body>
        <SiteShell>{children}</SiteShell>
      </body>
    </html>
  );
}
