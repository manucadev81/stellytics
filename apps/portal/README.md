# apps/portal

Portal em Next.js (App Router), com Drizzle ORM, Better Auth, Tailwind e ECharts. É a única porta de entrada da plataforma: cadastro self-service, login, painéis do tenant e painéis globais do SCF.

Lê as estatísticas web pela API do Umami (com uma conta de serviço) e as métricas de API direto do banco `notify`. O isolamento entre tenants na leitura é responsabilidade do portal: o tenant vem sempre da sessão.

Criado na [#57](https://github.com/romulospl/notify/issues/57).
