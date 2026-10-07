package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/comunicacao/protocolo"
)

func pedido(tx string, caronaID int, ts int64) protocolo.PedidoBloqueio {
	return protocolo.PedidoBloqueio{
		TxID: tx, CaronaID: caronaID, Origem: "A", Destino: "B",
		Passageiro: "p-" + tx, Data: "2030-01-01", Timestamp: ts,
	}
}

func qtdPendentes() int {
	mutexTx.Lock()
	defer mutexTx.Unlock()
	return len(pendentes)
}

func TestPrepareBloqueiaSemConsumirAssento(t *testing.T) {
	resetar(t)
	id := publicar(t, "m1", []string{"A", "B"}, "08:00", 1, 10)

	st, r := Preparar(pedido("tx1", id, 1))
	if st != http.StatusOK || !r.Voto || r.Preco != 10 {
		t.Fatalf("prepare deveria votar SIM com preco 10: %d %+v", st, r)
	}
	if l := livres(id, "A", "B"); l != 1 {
		t.Fatalf("prepare nao deve consumir o assento definitivo, livres=%d", l)
	}

	st, r = Preparar(pedido("tx2", id, 2))
	if st != http.StatusConflict || r.Voto || !r.Bloqueada || r.HolderTxID != "tx1" {
		t.Fatalf("segundo prepare deveria ser recusado por bloqueio de tx1: %d %+v", st, r)
	}
	if n := Bloqueados(id, "A", "B"); n != 1 {
		t.Fatalf("esperava 1 bloqueio, veio %d", n)
	}
}

func TestPrepareIdempotente(t *testing.T) {
	resetar(t)
	id := publicar(t, "m1", []string{"A", "B"}, "08:00", 2, 10)

	for i := 0; i < 3; i++ {
		if st, r := Preparar(pedido("tx1", id, 1)); st != http.StatusOK || !r.Voto {
			t.Fatalf("repeticao %d do prepare deveria continuar SIM: %+v", i, r)
		}
	}
	if n := Bloqueados(id, "A", "B"); n != 1 {
		t.Fatalf("prepare repetido nao pode bloquear varias vagas, bloqueios=%d", n)
	}
}

func TestCommitConsomeAssentoEIdempotente(t *testing.T) {
	resetar(t)
	id := publicar(t, "m1", []string{"A", "B"}, "08:00", 1, 10)
	Preparar(pedido("tx1", id, 1))

	for i := 0; i < 2; i++ {
		st, r := Commitar("tx1")
		if st != http.StatusOK {
			t.Fatalf("commit %d falhou: %d %+v", i, st, r)
		}
	}
	if l := livres(id, "A", "B"); l != 0 {
		t.Fatalf("commit repetido consumiu assentos demais: livres=%d (esperado 0)", l)
	}
	if n := qtdPendentes(); n != 0 {
		t.Fatalf("nao deveria restar bloqueio, restam %d", n)
	}
	if st, _ := Abortar("tx1", "tardio"); st != http.StatusConflict {
		t.Fatalf("rollback de transacao ja confirmada deveria ser recusado, status %d", st)
	}
	verificarInvariantes(t)
}

func TestRollbackLiberaBloqueio(t *testing.T) {
	resetar(t)
	id := publicar(t, "m1", []string{"A", "B"}, "08:00", 1, 10)
	Preparar(pedido("tx1", id, 1))
	if st, _ := Abortar("tx1", "teste"); st != http.StatusOK {
		t.Fatalf("rollback falhou: %d", st)
	}
	if st, r := Preparar(pedido("tx2", id, 2)); st != http.StatusOK || !r.Voto {
		t.Fatalf("vaga deveria estar livre apos rollback: %+v", r)
	}
	if st, _ := Abortar("tx1", "repetido"); st != http.StatusOK {
		t.Fatal("rollback deve ser idempotente")
	}
}

// Um prepare atrasado que chega depois do rollback nao pode recriar o bloqueio.
func TestPrepareAposRollbackRecusado(t *testing.T) {
	resetar(t)
	id := publicar(t, "m1", []string{"A", "B"}, "08:00", 1, 10)
	Abortar("txX", "rollback antes do prepare")
	if st, r := Preparar(pedido("txX", id, 1)); st != http.StatusConflict || r.Voto {
		t.Fatalf("prepare tardio deveria ser recusado: %+v", r)
	}
	if n := qtdPendentes(); n != 0 {
		t.Fatalf("vazamento de bloqueio: %d pendentes", n)
	}
}

func TestCommitDeTransacaoDesconhecida(t *testing.T) {
	resetar(t)
	if st, _ := Commitar("nao-existe"); st != http.StatusNotFound {
		t.Fatalf("esperava 404, veio %d", st)
	}
}

// Nenhum assento permanece bloqueado se o coordenador some depois do prepare.
func TestBloqueioExpiraSemCommit(t *testing.T) {
	resetar(t)
	Config.TTLBloqueio = 60 * time.Millisecond
	id := publicar(t, "m1", []string{"A", "B"}, "08:00", 1, 10)
	Preparar(pedido("orfa", id, 1))

	time.Sleep(150 * time.Millisecond)
	if n := LimparBloqueiosExpirados(); n != 1 {
		t.Fatalf("esperava 1 bloqueio expirado, veio %d", n)
	}
	if st, r := Preparar(pedido("tx2", id, 2)); st != http.StatusOK || !r.Voto {
		t.Fatalf("vaga deveria ter sido liberada pelo timeout: %+v", r)
	}
	if st, _ := Commitar("orfa"); st != http.StatusConflict {
		t.Fatalf("commit de transacao expirada deveria ser recusado, status %d", st)
	}
	verificarInvariantes(t)
}

func TestPrepareConcorrenteUltimaVaga(t *testing.T) {
	resetar(t)
	id := publicar(t, "m1", []string{"A", "B"}, "08:00", 1, 10)

	var wg sync.WaitGroup
	var mu sync.Mutex
	sims := 0
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if st, r := Preparar(pedido(fmt.Sprintf("tx%d", i), id, int64(i+1))); st == http.StatusOK && r.Voto {
				mu.Lock()
				sims++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if sims != 1 {
		t.Fatalf("esperava exatamente 1 voto SIM para a ultima vaga, vieram %d", sims)
	}
}

func TestBloqueioReduzDisponibilidadeNaBusca(t *testing.T) {
	resetar(t)
	id := publicar(t, "m1", []string{"A", "B"}, "08:00", 1, 10)
	Preparar(pedido("tx1", id, 1))

	var r protocolo.RespostaBuscarItinerarios
	chamar(t, ProcessarBuscarItinerarios, protocolo.ConsultaItinerario{
		Tipo: protocolo.TipoBuscarItinerariosReq, Origem: "A", Destino: "B", Data: "2030-01-01",
	}, &r)
	if len(r.Itinerarios) != 0 {
		t.Fatalf("vaga bloqueada nao deveria aparecer na busca: %+v", r.Itinerarios)
	}
}

func TestPrecedeLamport(t *testing.T) {
	if !precede(1, "z", 2, "a") {
		t.Error("menor timestamp deve preceder")
	}
	if precede(2, "a", 1, "z") {
		t.Error("maior timestamp nao deve preceder")
	}
	if !precede(5, "a", 5, "b") || precede(5, "b", 5, "a") {
		t.Error("empate deve ser decidido de forma deterministica pelo tx_id")
	}
	if precede(5, "a", 5, "a") {
		t.Error("uma transacao nao precede a si mesma")
	}
}

func TestRelogioLamport(t *testing.T) {
	var r relogioLamport
	a := r.Tick()
	b := r.Tick()
	if b <= a {
		t.Fatal("Tick deve ser crescente")
	}
	if c := r.Atualizar(100); c <= 100 {
		t.Fatalf("Atualizar(100) deve passar de 100, veio %d", c)
	}
	if c := r.Atualizar(1); c <= 101 {
		t.Fatalf("relogio nunca retrocede, veio %d", c)
	}
}

func TestParsePares(t *testing.T) {
	p, err := ParsePares("B=http://b:8080/, C = http://c:8080")
	if err != nil || p["B"] != "http://b:8080" || p["C"] != "http://c:8080" {
		t.Fatalf("parse invalido: %v %v", p, err)
	}
	for _, ruim := range []string{"B", "=http://x", "B=", "B=ftp://x"} {
		if _, err := ParsePares(ruim); err == nil {
			t.Errorf("%q deveria ser invalido", ruim)
		}
	}
	if p, err := ParsePares(""); err != nil || len(p) != 0 {
		t.Errorf("string vazia deve resultar em mapa vazio")
	}
}

// ---- API REST ----

func post(t *testing.T, url string, corpo any) (int, []byte) {
	t.Helper()
	var buf io.Reader
	switch c := corpo.(type) {
	case nil:
	case string:
		buf = bytes.NewBufferString(c)
	default:
		b, _ := json.Marshal(c)
		buf = bytes.NewReader(b)
	}
	resp, err := http.Post(url, "application/json", buf)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	dados, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, dados
}

func TestAPIRestFluxoCompleto(t *testing.T) {
	resetar(t)
	srv := httptest.NewServer(NovoHandlerAPI())
	defer srv.Close()
	id := publicar(t, "m1", []string{"A", "B"}, "08:00", 1, 10)

	// prepare -> 200 SIM
	st, corpo := post(t, srv.URL+protocolo.RotaPrepare, pedido("tx1", id, 1))
	var voto protocolo.RespostaPrepare
	_ = json.Unmarshal(corpo, &voto)
	if st != 200 || !voto.Voto || voto.Preco != 10 {
		t.Fatalf("prepare: %d %s", st, corpo)
	}

	// prepare concorrente na mesma vaga -> 409 NAO
	st, corpo = post(t, srv.URL+protocolo.RotaPrepare, pedido("tx2", id, 2))
	_ = json.Unmarshal(corpo, &voto)
	if st != 409 || voto.Voto || !voto.Bloqueada {
		t.Fatalf("segundo prepare deveria ser 409 bloqueado: %d %s", st, corpo)
	}

	// commit (tx_id na URL, corpo opcional) -> 200
	if st, corpo = post(t, srv.URL+"/api/v1/transacoes/tx1/commit", nil); st != 200 {
		t.Fatalf("commit: %d %s", st, corpo)
	}
	if l := livres(id, "A", "B"); l != 0 {
		t.Fatalf("commit via REST nao consumiu o assento: livres=%d", l)
	}

	// rollback de tx2 -> 200
	if st, corpo = post(t, srv.URL+"/api/v1/transacoes/tx2/rollback", protocolo.PedidoRollback{TxID: "tx2", Motivo: "teste"}); st != 200 {
		t.Fatalf("rollback: %d %s", st, corpo)
	}
	// rollback de transacao confirmada -> 409
	if st, _ = post(t, srv.URL+"/api/v1/transacoes/tx1/rollback", nil); st != 409 {
		t.Fatalf("rollback apos commit deveria ser 409, veio %d", st)
	}
	// commit desconhecido -> 404
	if st, _ = post(t, srv.URL+"/api/v1/transacoes/fantasma/commit", nil); st != 404 {
		t.Fatalf("commit desconhecido deveria ser 404, veio %d", st)
	}
}

func TestAPIRestValidacoes(t *testing.T) {
	resetar(t)
	srv := httptest.NewServer(NovoHandlerAPI())
	defer srv.Close()

	if st, _ := post(t, srv.URL+protocolo.RotaPrepare, "{nao e json"); st != 400 {
		t.Errorf("JSON invalido deveria ser 400, veio %d", st)
	}
	if st, _ := post(t, srv.URL+protocolo.RotaPrepare, protocolo.PedidoBloqueio{TxID: "x"}); st != 400 {
		t.Errorf("campos faltando deveria ser 400, veio %d", st)
	}
	if st, _ := post(t, srv.URL+"/api/v1/transacoes/a/commit", protocolo.PedidoCommit{TxID: "b"}); st != 400 {
		t.Errorf("tx_id divergente deveria ser 400, veio %d", st)
	}
	resp, err := http.Get(srv.URL + protocolo.RotaPrepare)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET deveria ser 405, veio %d", resp.StatusCode)
	}
}
