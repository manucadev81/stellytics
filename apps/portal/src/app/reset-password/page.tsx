import type { Metadata } from "next";
import Link from "next/link";
import { AuthCard, linkClass } from "@/components/auth-card";
import { ResetPasswordForm } from "./reset-password-form";

// O token está na URL: nenhuma página seguinte recebe o endereço como Referer.
export const metadata: Metadata = { title: "Choose a new password", referrer: "no-referrer" };

// Destino do link de redefinição (#60). O Better Auth confere o token antes
// de redirecionar: chega aqui com ?token= ou com ?error=INVALID_TOKEN.
export default async function ResetPasswordPage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const { token, error } = await searchParams;

  if (error || typeof token !== "string" || token.length === 0) {
    return (
      <AuthCard title="This link doesn't work">
        <p className="mt-4 text-sm text-zinc-600 dark:text-zinc-400">
          The reset link is invalid, was already used or expired. Ask for a new one.
        </p>
        <p className="mt-6 text-sm">
          <Link href="/forgot-password" className={linkClass}>
            Request a new link
          </Link>
        </p>
      </AuthCard>
    );
  }

  return (
    <AuthCard title="Choose a new password">
      <ResetPasswordForm token={token} />
    </AuthCard>
  );
}
