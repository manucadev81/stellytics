import type { Metadata } from "next";
import Link from "next/link";
import { AuthCard, linkClass } from "@/components/auth-card";
import { ForgotPasswordForm } from "./forgot-password-form";

export const metadata: Metadata = { title: "Forgot password" };

export default function ForgotPasswordPage() {
  return (
    <AuthCard title="Reset your password" subtitle="We'll email you a link to choose a new password.">
      <ForgotPasswordForm />
      <p className="mt-6 text-sm text-zinc-500">
        <Link href="/sign-in" className={linkClass}>
          Back to sign in
        </Link>
      </p>
    </AuthCard>
  );
}
