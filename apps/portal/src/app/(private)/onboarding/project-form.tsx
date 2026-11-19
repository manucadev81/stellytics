"use client";

import { useActionState } from "react";
import { buttonClass } from "@/components/ui/button";
import { inputClass } from "@/components/ui/field";
import { createProjectAction, type ProjectActionState } from "./actions";

const initial: ProjectActionState = { error: null };

export function ProjectForm() {
  const [state, action, pending] = useActionState(createProjectAction, initial);
  return (
    <form action={action} className="mt-6 max-w-md space-y-4">
      <label className="block">
        <span className="text-sm font-medium">Project name</span>
        <input
          name="name"
          required
          minLength={2}
          maxLength={100}
          placeholder="Soroban Explorer"
          className={inputClass}
        />
        <span className="mt-1 block text-xs text-zinc-500">
          The name of your Stellar project, as the SCF should see it.
        </span>
      </label>
      {state.error && (
        <p role="alert" className="text-sm text-red-600 dark:text-red-400">
          {state.error}
        </p>
      )}
      <button type="submit" disabled={pending} className={`${buttonClass} sm:w-auto`}>
        {pending ? "Creating…" : "Create project"}
      </button>
    </form>
  );
}
