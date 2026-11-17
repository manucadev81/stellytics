// Comando loadtest: gerador de carga do pipeline web e do pipeline de API (#51).
//
// Subcomandos:
//
//	setup     cria no Umami e no banco stellytics os sites, tenants, serviços e chaves do teste
//	web       manda page views ao gateway (GET /c, prova de JS, POST /e)
//	api       manda OTLP/HTTP (métricas ou spans) ao coletor com chave de ingestão
//	teardown  apaga tudo o que o setup criou
//
// A carga é de chegada aberta (Poisson): o ritmo não espera as respostas, então
// um servidor lento mostra latência e erro em vez de frear o gerador. O perfil
// de taxa é uma lista de degraus "taxa x duração", por exemplo
// "11.6x2m,116x3m,11.6x1m". Ver docs/relatorios/carga-1m-dia.md.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "setup":
		err = runSetup(os.Args[2:])
	case "teardown":
		err = runTeardown(os.Args[2:])
	case "web":
		err = runWeb(os.Args[2:])
	case "api":
		err = runAPI(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "loadtest:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "uso: loadtest setup|web|api|teardown [flags]  (-h em cada subcomando)")
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
