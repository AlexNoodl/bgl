import type { Metadata } from "next";

import "./globals.css";
import type { ReactNode } from "react";

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
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
