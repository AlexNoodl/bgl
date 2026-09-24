import type { Metadata } from "next";

import { LoginForm } from "@/components/auth/login-form";
import { PageContainer } from "@/components/ui/page-container";

export const metadata: Metadata = {
  title: "Log in — BGL",
  description: "Log in to your BGL account.",
};
// Заглушка страницы авторизации на будущее
export default function LoginPage() {
  return (
    <PageContainer className="max-w-sm">
      <h1 className="text-32 font-semibold leading-none">Вход</h1>
      <div className="mt-6">
        <LoginForm />
      </div>
    </PageContainer>
  );
}
