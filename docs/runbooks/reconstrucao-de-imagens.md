# Reconstrução das imagens Go

Este procedimento compara o **conteúdo de cada camada**, não o digest integral da imagem. Aplica-se a `ingest-web`, `collector`, `rollup` e `replication-monitor`, em `linux/amd64`. Ele exige uma imagem de release por digest, a revisão que a produziu, Docker com Buildx, Python 3, Git, acesso ao registro e espaço para duas exportações. Rode-o em uma máquina separada da que gerou a release.

1. Confirme no registro o digest da imagem da release e a tag Git correspondente. Verifique a assinatura Cosign pelo procedimento de [`deploy.md`](../deploy.md) antes de usar a imagem. Uma tag mutável não substitui o digest.
2. Em um clone que contenha a tag e este procedimento, execute para cada imagem:

   ```bash
   scripts/reproducibility/verify.sh vX.Y.Z ingest-web ghcr.io/romulospl/stellytics/ingest-web@sha256:<digest>
   scripts/reproducibility/verify.sh vX.Y.Z collector ghcr.io/romulospl/stellytics/collector@sha256:<digest>
   scripts/reproducibility/verify.sh vX.Y.Z rollup ghcr.io/romulospl/stellytics/rollup@sha256:<digest>
   scripts/reproducibility/verify.sh vX.Y.Z replication-monitor ghcr.io/romulospl/stellytics/replication-monitor@sha256:<digest>
   ```

   Use os digests individuais reais. Uma release anterior à existência de um serviço não terá sua imagem. O script exporta a revisão com `git archive`, faz pull por digest, cria um construtor BuildKit temporário sem cache compartilhado e reconstrói a imagem sem atestação. Remove o construtor e os arquivos temporários ao sair. Ele não modifica a imagem da release.
3. Guarde a revisão, plataforma, digest da referência e resultado em um registro de release. Se houver diferença, preserve os logs, não conclua que a imagem corresponde ao código e investigue antes de aprová-la para deploy. Erros de rede ou de build também fazem o comando falhar.

O comparador confere a ordem e a quantidade de camadas e, em cada uma, os caminhos, tipos, tamanhos, permissões, IDs de usuário e grupo, links, metadados PAX não temporais e SHA-256 do conteúdo. Ele rejeita caminhos duplicados numa camada. **Não compara timestamps, configuração OCI, labels, manifesto, atestações ou digest integral**; portanto o sucesso não prova que a imagem completa seja idêntica bit a bit. Assinatura e proveniência continuam sendo verificações separadas. Com o mesmo número e conteúdo de camadas, diferenças em configuração de execução ainda podem alterar o comportamento.

## Ensaio e limites

Em 17/11/2026, a revisão `c84d4d9a970ad754168bb2bb1f806e6bed89801b` foi reconstruída em `linux/amd64` com dois construtores BuildKit distintos no mesmo host, sem cache de camadas, usando Docker 29.9.2. O conteúdo comparado foi igual: `ingest-web` (2 camadas), `collector` (3), `rollup` (1) e `replication-monitor` (1). Os digests do gateway diferiram; os timestamps e a configuração OCI não foram normalizados. O comando `verify.sh` também foi exercitado integralmente contra o ID imutável da imagem local do gateway e por digest em um registry temporário no localhost, reconstruindo-a em construtores adicionais. Os testes do comparador verificam que mudança de conteúdo ou permissão falha e que timestamp isolado não causa falha.

**Ainda não houve comparação com uma imagem de release:** na data do ensaio o repositório não tinha releases publicadas. O ensaio local usa construtores isolados no mesmo host, não demonstra independência de hardware ou sistema operacional. A primeira release disponível precisa ser verificada em outra máquina pelo procedimento acima.

O portal Node/Next.js não foi avaliado: dependências nativas, metadados e horários de build podem mudar o resultado. O Postgres de produção instala pacotes de repositórios externos sem versões fixas; as camadas de bases de terceiros também dependem dos respectivos digests e plataformas. O frontend `docker/dockerfile:1`, a versão do BuildKit, a configuração OCI, timestamps e atestações de SBOM/proveniência podem alterar digests mesmo quando os arquivos Go são iguais.

**Decisão de automação:** por enquanto a reconstrução é manual após cada release, antes de aprovar a imagem para produção. Não foi acrescentado um job automático de release: ainda não existe uma imagem publicada para validar o fluxo com o registry, seus índices e atestações. Depois da primeira execução real bem-sucedida, pode-se automatizar em outro runner, preservando o digest exato emitido pelo job de publicação e tratando falhas como falhas, sem confundir a comparação de camadas com igualdade do manifesto.
