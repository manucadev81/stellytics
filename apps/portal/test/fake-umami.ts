// Umami falso para os testes: implementa só as rotas que o portal usa, com
// as mesmas respostas do Umami 3.4.1, e registra cada pedido recebido.
import { randomUUID } from "node:crypto";
import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";

export type FakeUmami = {
  url: string;
  apiKey: string;
  websites: Map<string, { id: string; name: string; domain: string }>;
  requests: { method: string; path: string }[];
  close: () => Promise<void>;
};

export async function startFakeUmami(apiKey: string): Promise<FakeUmami> {
  const websites: FakeUmami["websites"] = new Map();
  const requests: FakeUmami["requests"] = [];

  const server: Server = createServer(async (req, res) => {
    const url = new URL(req.url ?? "/", "http://fake");
    const method = req.method ?? "GET";
    requests.push({ method, path: url.pathname });

    const send = (status: number, body: unknown) => {
      res.writeHead(status, { "content-type": "application/json" });
      res.end(JSON.stringify(body));
    };

    if (req.headers.authorization !== `Bearer ${apiKey}`) return send(401, { error: "Unauthorized" });

    let raw = "";
    for await (const chunk of req) raw += chunk;
    const body = raw ? JSON.parse(raw) : {};

    if (method === "POST" && url.pathname === "/api/websites") {
      const website = { id: randomUUID(), name: body.name, domain: body.domain };
      websites.set(website.id, website);
      return send(200, website);
    }
    if (method === "GET" && url.pathname === "/api/websites") {
      const page = Number(url.searchParams.get("page") ?? 1);
      const pageSize = Number(url.searchParams.get("pageSize") ?? 10);
      const all = [...websites.values()];
      const data = all.slice((page - 1) * pageSize, page * pageSize);
      return send(200, { data, count: all.length, page, pageSize });
    }
    const match = url.pathname.match(/^\/api\/websites\/([^/]+)(\/stats)?$/);
    if (match && websites.has(match[1]!)) {
      if (method === "DELETE" && !match[2]) {
        websites.delete(match[1]!);
        return send(200, { ok: true });
      }
      if (method === "GET" && match[2]) {
        const totals = { pageviews: 7, visitors: 3, visits: 4, bounces: 1, totaltime: 60 };
        return send(200, { ...totals, comparison: { ...totals, pageviews: 5 } });
      }
    }
    return send(404, { error: "Not found" });
  });

  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const { port } = server.address() as AddressInfo;
  return {
    url: `http://127.0.0.1:${port}`,
    apiKey,
    websites,
    requests,
    close: () => new Promise((resolve) => server.close(() => resolve())),
  };
}
