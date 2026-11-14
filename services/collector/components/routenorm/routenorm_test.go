package routenorm

import (
	"strings"
	"testing"
	"unicode/utf8"
)

const (
	account  = "GAAZI4TCR3TY5OJHCTJC2A4QSY6CJWJH5IAJTGKIN2ER7LBNVKOCCWN7"
	contract = "CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC"
	muxed    = "MAAAAAAAAAAAAAB7BQ2L7E5NBWMXDUCMZSIPOBKRDSBYVLMXGSSKF6YNPIB7Y77ITLVL6"
	txHash   = "3389e9f0f1a65f19736cacf544c2e825313e8447f569233bb8db39aa607c8889"
)

func TestPathPlaceholders(t *testing.T) {
	cases := []struct{ in, want string }{
		// O exemplo da issue.
		{"/accounts/" + account + "/payments/", "/accounts/{address}/payments"},
		// UUID, em minúsculas e maiúsculas.
		{"/orders/3f2c1b9e-4a3d-4c8e-9f00-1234567890ab", "/orders/{uuid}"},
		{"/orders/3F2C1B9E-4A3D-4C8E-9F00-1234567890AB/items", "/orders/{uuid}/items"},
		// Endereços Stellar: conta, contrato e muxed.
		{"/contracts/" + contract + "/events", "/contracts/{address}/events"},
		{"/accounts/" + muxed, "/accounts/{address}"},
		// Hash de transação, com e sem 0x, e hash curto que ainda vale.
		{"/transactions/" + txHash, "/transactions/{hash}"},
		{"/tx/0x" + txHash, "/tx/{hash}"},
		{"/ledgers/abcdef0123456789", "/ledgers/{hash}"},
		// IDs numéricos.
		{"/ledgers/52345678/operations/1", "/ledgers/{id}/operations/{id}"},
		// Vários marcadores no mesmo caminho.
		{"/accounts/" + account + "/offers/123/tx/" + txHash, "/accounts/{address}/offers/{id}/tx/{hash}"},
	}
	for _, c := range cases {
		if got := Path(c.in); got != c.want {
			t.Errorf("Path(%q) = %q, quero %q", c.in, got, c.want)
		}
	}
}

func TestPathKeepsNonMatchingSegments(t *testing.T) {
	cases := []struct{ in, want string }{
		// Palavras que parecem hexadecimais, mas são curtas demais para {hash}.
		{"/cafe/add/beef", "/cafe/add/beef"},
		{"/v1/abcdef012345678", "/v1/abcdef012345678"}, // 15 dígitos hex
		// Quase UUID: tamanho certo, hífen no lugar errado ou letra fora do hex.
		{"/x/3f2c1b9e4-a3d-4c8e-9f00-1234567890ab", "/x/3f2c1b9e4-a3d-4c8e-9f00-1234567890ab"},
		{"/x/3f2c1b9e-4a3d-4c8e-9f00-1234567890az", "/x/3f2c1b9e-4a3d-4c8e-9f00-1234567890az"},
		// Quase endereço: minúsculas, letra fora do base32 (0, 1, 8, 9), tamanho
		// errado e prefixo errado.
		{"/accounts/" + strings.ToLower(account), "/accounts/" + strings.ToLower(account)},
		{"/accounts/G" + strings.Repeat("A", 54) + "1", "/accounts/G" + strings.Repeat("A", 54) + "1"},
		{"/accounts/" + account[:55], "/accounts/" + account[:55]},
		{"/accounts/X" + account[1:], "/accounts/X" + account[1:]},
		{"/accounts/M" + account[1:], "/accounts/M" + account[1:]}, // M com 56 caracteres
		// Número com sinal ou decimal não é {id}.
		{"/prices/-1/1.5", "/prices/-1/1.5"},
		// Versão de API e nomes comuns ficam.
		{"/v2/assets", "/v2/assets"},
	}
	for _, c := range cases {
		if got := Path(c.in); got != c.want {
			t.Errorf("Path(%q) = %q, quero %q", c.in, got, c.want)
		}
	}
}

func TestPathShape(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "/"},
		{"/", "/"},
		{"//", "/"},
		{"/payments/", "/payments"},
		{"/a//b///c/", "/a/b/c"},
		{"payments", "/payments"},
		{"/search?q=GABC&limit=10", "/search"},
		{"/docs#section", "/docs"},
		{"/ledgers/42?cursor=now", "/ledgers/{id}"},
		{"https://api.example.org/ledgers/42/?x=1", "/ledgers/{id}"},
	}
	for _, c := range cases {
		if got := Path(c.in); got != c.want {
			t.Errorf("Path(%q) = %q, quero %q", c.in, got, c.want)
		}
	}
}

func TestRouteClientRouteWins(t *testing.T) {
	cases := []struct{ route, path, want string }{
		// O template do framework vale como veio, sem passar pelas regras.
		{"/users/:id", "/users/42", "/users/:id"},
		{"/accounts/{account_id}/payments/", "/accounts/" + account + "/payments/", "/accounts/{account_id}/payments/"},
		{"/items/42", "/items/43", "/items/42"},
		// Sem http.route, valem as regras.
		{"", "/users/42", "/users/{id}"},
	}
	for _, c := range cases {
		if got := Route(c.route, c.path); got != c.want {
			t.Errorf("Route(%q, %q) = %q, quero %q", c.route, c.path, got, c.want)
		}
	}
}

func TestMaxLen(t *testing.T) {
	long := "/" + strings.Repeat("z", 400)
	if got := Path(long); len(got) != MaxLen {
		t.Errorf("len(Path) = %d, quero %d", len(got), MaxLen)
	}
	// Com vários segmentos, o corte fica no fim do último que cabe inteiro.
	segs := strings.Repeat("/segmento", 40)
	if got := Path(segs); len(got) > MaxLen || strings.HasSuffix(got, "/seg") || !strings.HasSuffix(got, "/segmento") {
		t.Errorf("Path cortou no meio de um segmento: ...%q", got[len(got)-12:])
	}
	if got := Route(long, ""); len(got) != MaxLen {
		t.Errorf("len(Route) = %d, quero %d", len(got), MaxLen)
	}
	// O corte não parte um caractere de vários bytes.
	accented := "/" + strings.Repeat("é", 200)
	if got := Path(accented); len(got) > MaxLen || !utf8.ValidString(got) {
		t.Errorf("Path cortou mal: %d bytes, UTF-8 válido = %v", len(got), utf8.ValidString(got))
	}
}

func FuzzPath(f *testing.F) {
	for _, s := range []string{"/", "/accounts/" + account + "/payments/", "/a//b?c#d", "https://x/y/1"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		got := Path(in)
		if !strings.HasPrefix(got, "/") || len(got) > MaxLen {
			t.Fatalf("Path(%q) = %q", in, got)
		}
		if got != "/" && strings.HasSuffix(got, "/") {
			t.Fatalf("Path(%q) = %q termina em barra", in, got)
		}
		if strings.Contains(got, "//") {
			t.Fatalf("Path(%q) = %q tem barra repetida", in, got)
		}
		// Normalizar de novo não muda nada, quando a entrada não precisou de corte
		// (um segmento cortado no meio pode passar a casar com um marcador).
		if again := Path(got); again != got && utf8.ValidString(in) && len(in) < MaxLen {
			t.Fatalf("Path não é idempotente: %q -> %q -> %q", in, got, again)
		}
	})
}
