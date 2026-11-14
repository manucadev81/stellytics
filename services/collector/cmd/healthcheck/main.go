// Command healthcheck é o healthcheck do contêiner do coletor, que não tem
// shell nem curl (imagem scratch). Faz um GET no receiver OTLP HTTP: qualquer
// resposta HTTP (o receiver responde 401 sem chave ou 405 a um GET) quer dizer
// que o coletor está de pé. Sai com 0 se respondeu e 1 se não.
package main

import (
	"net/http"
	"os"
	"time"
)

func main() {
	addr := os.Getenv("HEALTHCHECK_URL")
	if addr == "" {
		addr = "http://127.0.0.1:4318/v1/metrics"
	}
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get(addr)
	if err != nil {
		os.Exit(1)
	}
	resp.Body.Close()
}
