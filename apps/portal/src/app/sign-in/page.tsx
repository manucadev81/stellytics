import type { Metadata } from "next";
import { headers } from "next/headers";
import Link from "next/link";
import { redirect } from "next/navigation";
import { AuthCard, linkClass } from "@/components/auth-card";
import { auth } from "@/lib/auth";
import { safeNext } from "@/lib/safe-next";
import { SignInForm } from "./sign-in-form";

export const metadata: Metadata = { title: "Sign in" };

export default async function SignInPage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const params = await searchParams;
  const next = safeNext(params.next);
  const session = await auth.api.getSession({ headers: await headers() });
  if (session) redirect(next);

  return (
    <AuthCard title="Sign in to stellytics" subtitle="Web and API analytics for Stellar projects.">
      {params.reset === "1" && (
        <p role="status" className="mt-4 text-sm text-emerald-700 dark:text-emerald-400">
          Your password was changed. Sign in with the new one.
        </p>
      )}
      <SignInForm next={next} />
      <div className="mt-6 flex justify-between text-sm text-zinc-500">
        <Link href="/sign-up" className={linkClass}>
          Create an account
        </Link>
        <Link href="/forgot-password" className={linkClass}>
          Forgot password?
        </Link>
      </div>
    </AuthCard>
  );
}
