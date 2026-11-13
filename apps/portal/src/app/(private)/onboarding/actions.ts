"use server";

import { redirect } from "next/navigation";
import { requireOnboardingContext } from "@/lib/access";
import { createProject, ProjectError } from "@/lib/data/tenants";

export type ProjectActionState = { error: string | null };

// Cria o projeto de quem está na sessão (#17). O usuário vem da sessão; do
// formulário só chega o nome.
export async function createProjectAction(_prev: ProjectActionState, form: FormData): Promise<ProjectActionState> {
  const ctx = await requireOnboardingContext();
  try {
    await createProject(ctx, { name: String(form.get("name") ?? "") });
  } catch (error) {
    if (error instanceof ProjectError) {
      if (error.code === "already_member") redirect("/dashboard");
      return { error: error.message };
    }
    console.error("Falha ao criar projeto", (error as Error).name);
    return { error: "Something went wrong. Try again." };
  }
  redirect("/dashboard");
}
