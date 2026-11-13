import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";

const config = [
  ...nextVitals,
  ...nextTs,
  { ignores: [".next/**", "next-env.d.ts", "drizzle/**"] },
  // Só src/lib/umami/ fala com o Umami (#58, ADR 0003). A #18 estende esta
  // regra ao cliente do banco.
  {
    files: ["src/**/*.{ts,tsx}"],
    ignores: ["src/lib/umami/**"],
    rules: {
      "no-restricted-imports": [
        "error",
        {
          patterns: [
            {
              group: ["**/umami/http", "@/lib/umami/http"],
              message: "Só src/lib/umami/ chama o Umami. Use as funções públicas de lá.",
            },
          ],
        },
      ],
    },
  },
];

export default config;
