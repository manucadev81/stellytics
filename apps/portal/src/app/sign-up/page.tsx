import type { Metadata } from "next";
import { headers } from "next/headers";
import Link from "next/link";
import { redirect } from "next/navigation";
import { AuthCard, linkClass } from "@/components/auth-card";
import { auth } from "@/lib/auth";
import { SignUpForm } from "./sign-up-form";

export const metadata: Metadata = { title: "Create account" };

export default async function SignUpPage() {
  const session = await auth.api.getSession({ headers: await headers() });
  if (session) redirect("/dashboard");

  return (
    <AuthCard title="Create your notify account" subtitle="Web and API analytics for Stellar projects.">
      <SignUpForm />
      <p className="mt-6 text-sm text-zinc-500">
        Already have an account?{" "}
        <Link href="/sign-in" className={linkClass}>
          Sign in
        </Link>
      </p>
    </AuthCard>
  );
}
