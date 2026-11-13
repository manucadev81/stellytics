"use client";

import { useActionState, useEffect, useRef } from "react";
import { inputClass } from "@/components/auth-card";
import { createSiteAction, type SiteActionState } from "./actions";

const initial: SiteActionState = { status: "idle" };

export function SiteForm() {
  const [state, action, pending] = useActionState(createSiteAction, initial);
  const form = useRef<HTMLFormElement>(null);

  // Limpa o formulário depois de cadastrar.
  useEffect(() => {
    if (state.status === "created") form.current?.reset();
  }, [state]);

  return (
    <form ref={form} action={action} className="flex flex-wrap items-end gap-3">
      <label className="block min-w-48 flex-1">
        <span className="text-sm font-medium">Name</span>
        <input name="name" required maxLength={100} placeholder="Docs" className={inputClass} />
      </label>
      <label className="block min-w-64 flex-1">
        <span className="text-sm font-medium">Domain</span>
        <input name="domain" required maxLength={500} placeholder="docs.example.org" className={inputClass} />
      </label>
      <button
        type="submit"
        disabled={pending}
        className="rounded-md bg-zinc-900 px-3 py-2 text-sm font-medium text-white hover:bg-zinc-700 disabled:opacity-60 dark:bg-zinc-100 dark:text-zinc-900 dark:hover:bg-zinc-300"
      >
        {pending ? "Adding…" : "Add website"}
      </button>
      {state.status === "error" && (
        <p role="alert" className="w-full text-sm text-red-600 dark:text-red-400">
          {state.message}
        </p>
      )}
    </form>
  );
}
