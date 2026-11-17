import path from "node:path";
import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Imagem Docker enxuta: só o servidor e as dependências rastreadas.
  output: "standalone",
  // Monorepo: rastreia dependências a partir da raiz do repositório.
  outputFileTracingRoot: path.join(import.meta.dirname, "../.."),
  poweredByHeader: false,
  // O portal não usa o otimizador de imagens; evita depender de libvips em execução.
  images: { unoptimized: true },
};

export default nextConfig;
