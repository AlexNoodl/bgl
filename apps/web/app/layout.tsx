import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "BGL",
  description: "Личная библиотека и платформа обзора видеоигр.",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
