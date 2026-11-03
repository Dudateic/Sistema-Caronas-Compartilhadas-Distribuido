package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/distribuido"
	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/modelos"
)

func TestReservaAtravessaEmpresas(t *testing.T) {
	c := novoConsorcio(t, "A", "B", "C")
	a := c.nos["A"].publicar(t, []string{"SSA", "FSA"}, 2, 10)
	b := c.nos["B"].publicar(t, []string{"FSA", "VCA"}, 2, 10)
	cc := c.nos["C"].publicar(t, []string{"VCA", "MOC"}, 2, 10)

	ok, motivo := c.nos["A"].reservar("ana",
		trecho("A", a, "SSA", "FSA"), trecho("B", b, "FSA", "VCA"), trecho("C", cc, "VCA", "MOC"))
	if !ok {
		t.Fatalf("reserva deveria confirmar: %s", motivo)
	}
	esperar(t, 2*time.Second, "passageira registrada nas 3 empresas", func() bool {
		return len(c.nos["A"].passageiros(a, "SSA", "FSA")) == 1 &&
			len(c.nos["B"].passageiros(b, "FSA", "VCA")) == 1 &&
			len(c.nos["C"].passageiros(cc, "VCA", "MOC")) == 1
	})
	c.invariantes(t)
}

func TestNaoConfirmaParcial(t *testing.T) {
	c := novoConsorcio(t, "A", "B", "C")
	a := c.nos["A"].publicar(t, []string{"SSA", "FSA"}, 1, 10)
	b := c.nos["B"].publicar(t, []string{"FSA", "VCA"}, 1, 10)
	cc := c.nos["C"].publicar(t, []string{"VCA", "MOC"}, 1, 10)

	if ok, m := c.nos["C"].reservar("zé", trecho("C", cc, "VCA", "MOC")); !ok {
		t.Fatalf("setup: %s", m)
	}
	ok, _ := c.nos["A"].reservar("ana",
		trecho("A", a, "SSA", "FSA"), trecho("B", b, "FSA", "VCA"), trecho("C", cc, "VCA", "MOC"))
	if ok {
		t.Fatal("reserva nao deveria confirmar: trecho de C lotado")
	}
	esperar(t, 2*time.Second, "assentos de A e B liberados pelo ABORT", func() bool {
		return c.nos["A"].livres(a, "SSA", "FSA") == 1 && c.nos["B"].livres(b, "FSA", "VCA") == 1
	})
	c.invariantes(t)
}

func TestUltimoAssentoComCoordenadoresDiferentes(t *testing.T) {
	c := novoConsorcio(t, "A", "B", "C")
	b := c.nos["B"].publicar(t, []string{"FSA", "VCA"}, 1, 10)

	var confirmados int32
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		coord := []string{"A", "B", "C"}[i%3]
		wg.Add(1)
		go func(i int, coord string) {
			defer wg.Done()
			if ok, _ := c.nos[coord].reservar(fmt.Sprintf("p%d", i), trecho("B", b, "FSA", "VCA")); ok {
				atomic.AddInt32(&confirmados, 1)
			}
		}(i, coord)
	}
	wg.Wait()

	if confirmados != 1 {
		t.Fatalf("esperado exatamente 1 confirmacao para o ultimo assento, obtido %d", confirmados)
	}
	esperar(t, 2*time.Second, "1 passageiro no trecho", func() bool { return len(c.nos["B"].passageiros(b, "FSA", "VCA")) == 1 })
	c.invariantes(t)
}

func TestSemDeadlockEmOrdensOpostas(t *testing.T) {
	c := novoConsorcio(t, "A", "B")
	a := c.nos["A"].publicar(t, []string{"X", "Y"}, 1, 10)
	b := c.nos["B"].publicar(t, []string{"Y", "Z"}, 1, 10)

	var wg sync.WaitGroup
	var ok1, ok2 bool
	wg.Add(2)
	go func() {
		defer wg.Done()
		ok1, _ = c.nos["A"].reservar("p1", trecho("A", a, "X", "Y"), trecho("B", b, "Y", "Z"))
	}()
	go func() {
		defer wg.Done()
		ok2, _ = c.nos["B"].reservar("p2", trecho("B", b, "Y", "Z"), trecho("A", a, "X", "Y"))
	}()

	feito := make(chan struct{})
	go func() { wg.Wait(); close(feito) }()
	select {
	case <-feito:
	case <-time.After(10 * time.Second):
		t.Fatal("DEADLOCK: reservas cruzadas nao terminaram")
	}
	if ok1 && ok2 {
		t.Fatal("as duas reservas confirmaram para 1 assento")
	}
	esperar(t, 2*time.Second, "sem pendencias", func() bool {
		return c.nos["A"].part.PendentesAtivas() == 0 && c.nos["B"].part.PendentesAtivas() == 0
	})
	c.invariantes(t)
}

func TestCargaConcorrenteMantemInvariantes(t *testing.T) {
	c := novoConsorcio(t, "A", "B", "C")
	ids := []string{"A", "B", "C"}
	rotas := map[string][]int{}
	for _, id := range ids {
		rotas[id] = []int{
			c.nos[id].publicar(t, []string{"P", "Q"}, 5, 10),
			c.nos[id].publicar(t, []string{"Q", "R"}, 5, 10),
		}
	}

	const reservas = 300
	var confirmadas int32
	var wg sync.WaitGroup
	sem := make(chan struct{}, 40)
	for i := 0; i < reservas; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			r := rand.New(rand.NewSource(int64(i)))
			x, y := ids[r.Intn(3)], ids[r.Intn(3)]
			coord := ids[r.Intn(3)]
			ts := []modelos.TrechoViagem{trecho(x, rotas[x][0], "P", "Q"), trecho(y, rotas[y][1], "Q", "R")}
			if ok, _ := c.nos[coord].reservar(fmt.Sprintf("p%d", i), ts...); ok {
				atomic.AddInt32(&confirmadas, 1)
			}
		}(i)
	}
	wg.Wait()

	esperar(t, 5*time.Second, "sem pendencias apos a carga", func() bool {
		for _, n := range c.nos {
			if n.part.PendentesAtivas() != 0 {
				return false
			}
		}
		return true
	})
	c.invariantes(t)

	nos := map[string]int{}
	for _, id := range ids {
		for _, car := range c.nos[id].caronas.Snapshot() {
			for _, tr := range car.Trechos {
				for _, p := range tr.Passageiros {
					nos[p]++
				}
			}
		}
	}
	for p, n := range nos {
		if n != 2 {
			t.Errorf("passageiro %s aparece em %d trechos (esperado 2): reserva pela metade", p, n)
		}
	}
	if int(confirmadas)*2 != func() int {
		s := 0
		for _, n := range nos {
			s += n
		}
		return s
	}() {
		t.Errorf("confirmadas=%d nao bate com o total de trechos ocupados", confirmadas)
	}
	t.Logf("%d reservas concorrentes, %d confirmadas, invariantes preservadas", reservas, confirmadas)
}

func TestEmpresaForaDoArNaoBloqueiaAsDemais(t *testing.T) {
	c := novoConsorcio(t, "A", "B", "C")
	a := c.nos["A"].publicar(t, []string{"SSA", "FSA"}, 2, 10)
	b := c.nos["B"].publicar(t, []string{"FSA", "VCA"}, 2, 10)
	cc := c.nos["C"].publicar(t, []string{"VCA", "MOC"}, 2, 10)

	c.nos["B"].derrubar()

	ok, _ := c.nos["A"].reservar("ana", trecho("A", a, "SSA", "FSA"), trecho("B", b, "FSA", "VCA"), trecho("C", cc, "VCA", "MOC"))
	if ok {
		t.Fatal("nao deveria confirmar com B fora do ar")
	}
	esperar(t, 3*time.Second, "A e C liberam os assentos", func() bool {
		return c.nos["A"].livres(a, "SSA", "FSA") == 2 && c.nos["C"].livres(cc, "VCA", "MOC") == 2
	})

	if ok, m := c.nos["A"].reservar("bia", trecho("A", a, "SSA", "FSA")); !ok {
		t.Fatalf("A deveria operar normalmente sem B: %s", m)
	}
	if ok, m := c.nos["C"].reservar("caio", trecho("A", a, "SSA", "FSA"), trecho("C", cc, "VCA", "MOC")); !ok {
		t.Fatalf("A e C deveriam operar sem B: %s", m)
	}
	c.nos["A"].coord.DefinirTimeoutPrepare(800 * time.Millisecond)
	c.invariantes(t)
}

func TestParticipanteCaiAposPrepareECommitChegaDepois(t *testing.T) {
	c := novoConsorcio(t, "A", "B")
	b := c.nos["B"].publicar(t, []string{"FSA", "VCA"}, 1, 10)

	resp, err := c.nos["B"].part.ProcessarPrepare(modelos.PrepareRequest{
		TxID: "tx_manual_1", CaronaID: b, Origem: "FSA", Destino: "VCA", Passageiro: "ana", Timestamp: 5, Coordenador: "A",
	})
	if err != nil || !resp.VotoSim {
		t.Fatalf("prepare: %+v %v", resp, err)
	}

	c.nos["B"].derrubar()
	c.nos["B"].iniciar(t)

	if got := c.nos["B"].part.PendentesAtivas(); got != 1 {
		t.Fatalf("apos reiniciar, B deveria ter 1 transacao pendente recuperada do log, tem %d", got)
	}
	if got := c.nos["B"].livres(b, "FSA", "VCA"); got != 0 {
		t.Fatalf("o assento deveria continuar reservado apos o reinicio, livres=%d", got)
	}

	if err := c.nos["B"].part.ProcessarCommit("tx_manual_1"); err != nil {
		t.Fatalf("commit apos recuperacao: %v", err)
	}
	if p := c.nos["B"].passageiros(b, "FSA", "VCA"); len(p) != 1 || p[0] != "ana" {
		t.Fatalf("passageiros=%v", p)
	}
	c.invariantes(t)
}

func TestCoordenadorCaiDepoisDeDecidirCommit(t *testing.T) {
	c := novoConsorcio(t, "A", "B")
	b := c.nos["B"].publicar(t, []string{"FSA", "VCA"}, 1, 10)

	if resp, _ := c.nos["B"].part.ProcessarPrepare(modelos.PrepareRequest{
		TxID: "tx_coord_1", CaronaID: b, Origem: "FSA", Destino: "VCA", Passageiro: "ana", Coordenador: "A",
	}); !resp.VotoSim {
		t.Fatal("prepare recusado")
	}

	c.nos["A"].derrubar()
	wal, err := distribuido.NovoLogTransacoes(c.nos["A"].dir + "/coordenador.log")
	if err != nil {
		t.Fatal(err)
	}
	det, _ := json.Marshal(map[string]any{"participantes": map[string]string{"B": c.url("B")}, "passageiro": "ana"})
	_ = wal.Gravar("tx_coord_1", distribuido.LogInicio, "", string(det))
	_ = wal.Gravar("tx_coord_1", distribuido.LogCommit, "", "global_commit_aprovado")
	_ = wal.Fechar()

	c.nos["A"].iniciar(t)
	esperar(t, 3*time.Second, "B confirma o assento apos a recuperacao de A", func() bool {
		return len(c.nos["B"].passageiros(b, "FSA", "VCA")) == 1
	})
	if c.nos["B"].part.PendentesAtivas() != 0 {
		t.Fatal("B ainda tem transacao pendente")
	}
	c.invariantes(t)
}

func TestCoordenadorCaiAntesDeDecidirAbortaPresumido(t *testing.T) {
	c := novoConsorcio(t, "A", "B")
	b := c.nos["B"].publicar(t, []string{"FSA", "VCA"}, 1, 10)

	if resp, _ := c.nos["B"].part.ProcessarPrepare(modelos.PrepareRequest{
		TxID: "tx_coord_2", CaronaID: b, Origem: "FSA", Destino: "VCA", Passageiro: "ana", Coordenador: "A",
	}); !resp.VotoSim {
		t.Fatal("prepare recusado")
	}

	c.nos["A"].derrubar()
	wal, _ := distribuido.NovoLogTransacoes(c.nos["A"].dir + "/coordenador.log")
	det, _ := json.Marshal(map[string]any{"participantes": map[string]string{"B": c.url("B")}, "passageiro": "ana"})
	_ = wal.Gravar("tx_coord_2", distribuido.LogInicio, "", string(det))
	_ = wal.Fechar()

	c.nos["A"].iniciar(t)

	esperar(t, 3*time.Second, "B libera o assento (abort presumido)", func() bool {
		return c.nos["B"].livres(b, "FSA", "VCA") == 1 && c.nos["B"].part.PendentesAtivas() == 0
	})
	c.invariantes(t)
}

func TestBloqueioOrfaoExpiraConsultandoCoordenador(t *testing.T) {
	c := novoConsorcio(t, "A", "B")
	b := c.nos["B"].publicar(t, []string{"FSA", "VCA"}, 1, 10)

	if resp, _ := c.nos["B"].part.ProcessarPrepare(modelos.PrepareRequest{
		TxID: "tx_orfa", CaronaID: b, Origem: "FSA", Destino: "VCA", Passageiro: "ana", Coordenador: "A",
	}); !resp.VotoSim {
		t.Fatal("prepare recusado")
	}
	time.Sleep(60 * time.Millisecond)
	c.nos["B"].part.LimparTransacoesExpiradas(30 * time.Millisecond)

	if got := c.nos["B"].livres(b, "FSA", "VCA"); got != 1 {
		t.Fatalf("assento orfao nao foi liberado, livres=%d", got)
	}
	c.invariantes(t)
}

func TestParticipanteNaoAbortaSozinhoSeCoordenadorForaDoAr(t *testing.T) {
	c := novoConsorcio(t, "A", "B")
	b := c.nos["B"].publicar(t, []string{"FSA", "VCA"}, 1, 10)
	_, _ = c.nos["B"].part.ProcessarPrepare(modelos.PrepareRequest{
		TxID: "tx_espera", CaronaID: b, Origem: "FSA", Destino: "VCA", Passageiro: "ana", Coordenador: "A",
	})
	c.nos["A"].derrubar()
	time.Sleep(60 * time.Millisecond)
	c.nos["B"].part.LimparTransacoesExpiradas(30 * time.Millisecond)
	if c.nos["B"].part.PendentesAtivas() != 1 {
		t.Fatal("B abortou sozinho sem saber a decisao do coordenador")
	}

	c.nos["A"].iniciar(t) // coordenador volta: sem registro => abort presumido
	esperar(t, 3*time.Second, "B resolve apos A voltar", func() bool {
		c.nos["B"].part.LimparTransacoesExpiradas(30 * time.Millisecond)
		return c.nos["B"].part.PendentesAtivas() == 0
	})
	if c.nos["B"].livres(b, "FSA", "VCA") != 1 {
		t.Fatal("assento deveria ter sido liberado")
	}
}

func TestReinicioPreservaReservasConfirmadas(t *testing.T) {
	c := novoConsorcio(t, "A", "B")
	b := c.nos["B"].publicar(t, []string{"FSA", "VCA"}, 2, 10)
	if ok, m := c.nos["A"].reservar("ana", trecho("B", b, "FSA", "VCA")); !ok {
		t.Fatal(m)
	}
	esperar(t, 2*time.Second, "confirmado", func() bool { return len(c.nos["B"].passageiros(b, "FSA", "VCA")) == 1 })

	c.nos["B"].derrubar()
	c.nos["B"].iniciar(t)

	if p := c.nos["B"].passageiros(b, "FSA", "VCA"); len(p) != 1 {
		t.Fatalf("passageiros apos reinicio: %v", p)
	}
	if l := c.nos["B"].livres(b, "FSA", "VCA"); l != 1 {
		t.Fatalf("livres apos reinicio: %d", l)
	}
	c.invariantes(t)
}

func TestReconciliacaoLiberaAssentoSemVotoNoLog(t *testing.T) {
	c := novoConsorcio(t, "B")
	b := c.nos["B"].publicar(t, []string{"FSA", "VCA"}, 2, 10)
	if err := c.nos["B"].caronas.BloquearAssentoProvisorio(b, "FSA", "VCA", "fantasma"); err != nil {
		t.Fatal(err)
	}
	if c.nos["B"].livres(b, "FSA", "VCA") != 1 {
		t.Fatal("setup")
	}
	c.nos["B"].derrubar()
	c.nos["B"].iniciar(t)
	if l := c.nos["B"].livres(b, "FSA", "VCA"); l != 2 {
		t.Fatalf("assento preso nao foi reconciliado: livres=%d", l)
	}
}

func postJSON(t *testing.T, url string, corpo any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(corpo)
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&m)
	return resp.StatusCode, m
}

func TestAPIRestCodigosEIdempotencia(t *testing.T) {
	c := novoConsorcio(t, "B")
	b := c.nos["B"].publicar(t, []string{"FSA", "VCA"}, 1, 10)
	base := c.url("B") + "/api/v1/transacoes/"
	prep := modelos.PrepareRequest{TxID: "tx_rest", CaronaID: b, Origem: "FSA", Destino: "VCA", Passageiro: "ana", Coordenador: "A"}

	if st, _ := postJSON(t, base+"prepare", prep); st != 200 {
		t.Fatalf("prepare esperado 200, veio %d", st)
	}
	// PREPARE repetido (mesma requisição) não pode bloquear um 2º assento.
	if st, _ := postJSON(t, base+"prepare", prep); st != 200 {
		t.Fatalf("prepare repetido esperado 200, veio %d", st)
	}
	if l := c.nos["B"].livres(b, "FSA", "VCA"); l != 0 {
		t.Fatalf("prepare repetido bloqueou assento extra: livres=%d", l)
	}
	outro := prep
	outro.TxID, outro.Passageiro = "tx_rest_2", "bia"
	if st, _ := postJSON(t, base+"prepare", outro); st != 409 {
		t.Fatalf("voto NAO esperado 409, veio %d", st)
	}
	if st, _ := postJSON(t, base+"prepare", map[string]any{"lixo": true}); st != 400 {
		t.Fatalf("corpo invalido esperado 400, veio %d", st)
	}

	if st, _ := postJSON(t, base+"commit", map[string]any{"tx_id": "tx_rest"}); st != 200 {
		t.Fatalf("commit esperado 200, veio %d", st)
	}
	// COMMIT repetido: 200 e sem passageiro duplicado.
	if st, _ := postJSON(t, base+"commit", map[string]any{"tx_id": "tx_rest"}); st != 200 {
		t.Fatalf("commit repetido esperado 200, veio %d", st)
	}
	if p := c.nos["B"].passageiros(b, "FSA", "VCA"); len(p) != 1 {
		t.Fatalf("commit repetido duplicou passageiro: %v", p)
	}
	if st, _ := postJSON(t, base+"abort", map[string]any{"tx_id": "tx_rest"}); st != 409 {
		t.Fatalf("abort de transacao confirmada esperado 409, veio %d", st)
	}
	if st, _ := postJSON(t, base+"commit", map[string]any{"tx_id": "inexistente"}); st != 404 {
		t.Fatalf("commit de transacao desconhecida esperado 404, veio %d", st)
	}
	if st, _ := postJSON(t, base+"abort", map[string]any{"tx_id": "inexistente"}); st != 200 {
		t.Fatalf("abort de transacao desconhecida esperado 200, veio %d", st)
	}
	late := modelos.PrepareRequest{TxID: "inexistente", CaronaID: b, Origem: "FSA", Destino: "VCA", Passageiro: "caio"}
	if st, _ := postJSON(t, base+"prepare", late); st != 409 {
		t.Fatalf("prepare atrasado apos abort esperado 409, veio %d", st)
	}
	c.invariantes(t)
}

func TestRelogioLamport(t *testing.T) {
	var r distribuido.RelogioLamport
	a := r.Tick()
	b := r.Atualizar(100) // mensagem com carimbo maior: relógio salta
	if !(b > 100 && b > a) {
		t.Fatalf("Atualizar(100) = %d", b)
	}
	if d := r.Atualizar(1); d <= b {
		t.Fatalf("relogio nao pode regredir: %d <= %d", d, b)
	}
}

func prepararPara(t *testing.T, n *no, tx string, caronaID int, origem, destino, passageiro string, participantes []string) {
	t.Helper()
	resp, err := n.part.ProcessarPrepare(modelos.PrepareRequest{
		TxID: tx, CaronaID: caronaID, Origem: origem, Destino: destino, Passageiro: passageiro,
		Coordenador: "A", Participantes: participantes,
	})
	if err != nil || !resp.VotoSim {
		t.Fatalf("prepare em %s: %+v %v", n.id, resp, err)
	}
}

func TestCoordenadorCaidoParticipanteQueNuncaVotouLiberaOutro(t *testing.T) {
	c := novoConsorcio(t, "A", "B", "C")
	b := c.nos["B"].publicar(t, []string{"FSA", "VCA"}, 1, 10)
	cc := c.nos["C"].publicar(t, []string{"VCA", "MOC"}, 1, 10)
	prepararPara(t, c.nos["B"], "tx_coop_1", b, "FSA", "VCA", "ana", []string{"A", "B", "C"})

	c.nos["A"].derrubar()
	time.Sleep(60 * time.Millisecond)
	c.nos["B"].part.LimparTransacoesExpiradas(30 * time.Millisecond)

	if c.nos["B"].part.PendentesAtivas() != 0 || c.nos["B"].livres(b, "FSA", "VCA") != 1 {
		t.Fatal("B deveria ter liberado o assento depois de saber que C nunca votou")
	}
	resp, _ := c.nos["C"].part.ProcessarPrepare(modelos.PrepareRequest{
		TxID: "tx_coop_1", CaronaID: cc, Origem: "VCA", Destino: "MOC", Passageiro: "ana", Coordenador: "A",
	})
	if resp.VotoSim {
		t.Fatal("C votou SIM depois de prometer abortar")
	}
	c.invariantes(t)
}

func TestCoordenadorCaidoParticipanteQueJaConfirmouDecideOutro(t *testing.T) {
	c := novoConsorcio(t, "A", "B", "C")
	b := c.nos["B"].publicar(t, []string{"FSA", "VCA"}, 1, 10)
	cc := c.nos["C"].publicar(t, []string{"VCA", "MOC"}, 1, 10)
	todos := []string{"A", "B", "C"}
	prepararPara(t, c.nos["B"], "tx_coop_2", b, "FSA", "VCA", "ana", todos)
	prepararPara(t, c.nos["C"], "tx_coop_2", cc, "VCA", "MOC", "ana", todos)
	if err := c.nos["C"].part.ProcessarCommit("tx_coop_2"); err != nil { // o COMMIT chegou só a C
		t.Fatal(err)
	}

	c.nos["A"].derrubar()
	time.Sleep(60 * time.Millisecond)
	c.nos["B"].part.LimparTransacoesExpiradas(30 * time.Millisecond)

	if p := c.nos["B"].passageiros(b, "FSA", "VCA"); len(p) != 1 || p[0] != "ana" {
		t.Fatalf("B deveria ter confirmado a reserva: %v", p)
	}
	c.invariantes(t)
}

func TestCoordenadorCaidoTodosPreparadosEsperamARecuperacao(t *testing.T) {
	c := novoConsorcio(t, "A", "B", "C")
	b := c.nos["B"].publicar(t, []string{"FSA", "VCA"}, 1, 10)
	cc := c.nos["C"].publicar(t, []string{"VCA", "MOC"}, 1, 10)
	todos := []string{"A", "B", "C"}
	prepararPara(t, c.nos["B"], "tx_coop_3", b, "FSA", "VCA", "ana", todos)
	prepararPara(t, c.nos["C"], "tx_coop_3", cc, "VCA", "MOC", "ana", todos)

	c.nos["A"].derrubar()
	time.Sleep(60 * time.Millisecond)
	c.nos["B"].part.LimparTransacoesExpiradas(30 * time.Millisecond)
	c.nos["C"].part.LimparTransacoesExpiradas(30 * time.Millisecond)
	if c.nos["B"].part.PendentesAtivas() != 1 || c.nos["C"].part.PendentesAtivas() != 1 {
		t.Fatal("ninguem poderia decidir sozinho: todos votaram SIM")
	}

	wal, _ := distribuido.NovoLogTransacoes(c.nos["A"].dir + "/coordenador.log")
	det, _ := json.Marshal(map[string]any{"participantes": map[string]string{"B": c.url("B"), "C": c.url("C")}})
	_ = wal.Gravar("tx_coop_3", distribuido.LogInicio, "", string(det))
	_ = wal.Gravar("tx_coop_3", distribuido.LogCommit, "", "global_commit_aprovado")
	_ = wal.Fechar()
	c.nos["A"].iniciar(t)

	esperar(t, 3*time.Second, "B e C confirmam apos A voltar", func() bool {
		return len(c.nos["B"].passageiros(b, "FSA", "VCA")) == 1 && len(c.nos["C"].passageiros(cc, "VCA", "MOC")) == 1
	})
	c.invariantes(t)
}

func TestAPIEstadoDaTransacao(t *testing.T) {
	c := novoConsorcio(t, "B")
	b := c.nos["B"].publicar(t, []string{"FSA", "VCA"}, 1, 10)
	prepararPara(t, c.nos["B"], "tx_estado", b, "FSA", "VCA", "ana", []string{"A", "B"})

	estado := func(tx string) string {
		resp, err := http.Get(c.url("B") + "/api/v1/transacoes/estado?tx_id=" + tx)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var m map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&m)
		return fmt.Sprint(m["estado"])
	}
	if e := estado("tx_estado"); e != "PREPARADA" {
		t.Fatalf("esperado PREPARADA, veio %s", e)
	}
	_ = c.nos["B"].part.ProcessarCommit("tx_estado")
	if e := estado("tx_estado"); e != "CONFIRMADA" {
		t.Fatalf("esperado CONFIRMADA, veio %s", e)
	}
	if e := estado("nunca_vista"); e != "ABORTADA" {
		t.Fatalf("transacao desconhecida deve virar ABORTADA (promessa), veio %s", e)
	}
}
