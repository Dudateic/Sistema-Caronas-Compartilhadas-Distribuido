package testes

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/comunicacao/protocolo"
)

var (
	binServidor string
	dirBin      string
)

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}
	var err error
	dirBin, err = os.MkdirTemp("", "vaijunto-e2e-")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	binServidor = filepath.Join(dirBin, "servidor")
	build := exec.Command("go", "build", "-o", binServidor, "./aplicacao/servidor")
	build.Dir = ".."
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Println("falha ao compilar o servidor:", err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dirBin)
	os.Exit(code)
}

type no struct {
	nome  string
	tcp   string
	http  string
	dados string
	pares string
	cmd   *exec.Cmd
}

func portaLivre(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

func (n *no) iniciar(t *testing.T) {
	t.Helper()
	cmd := exec.Command(binServidor,
		"-empresa", n.nome, "-tcp", n.tcp, "-http", n.http, "-pares", n.pares, "-dados", n.dados,
		"-ttl-bloqueio", "2s", "-timeout-par", "800ms")
	if os.Getenv("E2E_LOGS") != "" {
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	n.cmd = cmd
	limite := time.Now().Add(10 * time.Second)
	for time.Now().Before(limite) {
		c1, e1 := net.DialTimeout("tcp", n.tcp, 200*time.Millisecond)
		c2, e2 := net.DialTimeout("tcp", n.http, 200*time.Millisecond)
		if e1 == nil && e2 == nil {
			c1.Close()
			c2.Close()
			return
		}
		if c1 != nil {
			c1.Close()
		}
		if c2 != nil {
			c2.Close()
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("servidor %s nao subiu", n.nome)
}

func (n *no) matar() {
	if n.cmd != nil && n.cmd.Process != nil {
		_ = n.cmd.Process.Kill()
		_, _ = n.cmd.Process.Wait()
		n.cmd = nil
	}
}

type consorcio struct{ A, B, C *no }

func novoConsorcio(t *testing.T) *consorcio {
	t.Helper()
	if testing.Short() {
		t.Skip("teste de integracao (use sem -short)")
	}
	mk := func(nome string) *no {
		return &no{nome: nome, tcp: portaLivre(t), http: portaLivre(t), dados: t.TempDir()}
	}
	c := &consorcio{A: mk("A"), B: mk("B"), C: mk("C")}
	todos := []*no{c.A, c.B, c.C}
	for _, n := range todos {
		for _, outro := range todos {
			if outro != n {
				if n.pares != "" {
					n.pares += ","
				}
				n.pares += fmt.Sprintf("%s=http://%s", outro.nome, outro.http)
			}
		}
	}
	for _, n := range todos {
		n.iniciar(t)
	}
	t.Cleanup(func() {
		for _, n := range todos {
			n.matar()
		}
	})
	return c
}

type cliente struct {
	conn net.Conn
	r    *bufio.Reader
}

func (n *no) conectar(t *testing.T) *cliente {
	t.Helper()
	conn, err := net.DialTimeout("tcp", n.tcp, 2*time.Second)
	if err != nil {
		t.Fatalf("conectando a %s: %v", n.nome, err)
	}
	t.Cleanup(func() { conn.Close() })
	return &cliente{conn: conn, r: bufio.NewReader(conn)}
}

func (c *cliente) trocar(t *testing.T, req any, tipoRes string, destino any) {
	t.Helper()
	b, _ := json.Marshal(req)
	if _, err := c.conn.Write(append(b, '\n')); err != nil {
		t.Fatalf("enviando: %v", err)
	}
	_ = c.conn.SetReadDeadline(time.Now().Add(20 * time.Second))
	for {
		linha, err := c.r.ReadBytes('\n')
		if err != nil {
			t.Fatalf("lendo resposta de %s: %v", tipoRes, err)
		}
		var base protocolo.MensagemBase
		if json.Unmarshal(linha, &base) == nil && base.Tipo == tipoRes {
			if err := json.Unmarshal(linha, destino); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
}

func (n *no) publicar(t *testing.T, rota []string, horario string, assentos int, preco float64) int {
	t.Helper()
	var r protocolo.RespostaPublicarCarona
	n.conectar(t).trocar(t, protocolo.PublicacaoCarona{
		Tipo: protocolo.TipoPublicarCaronaReq, Motorista: "mot-" + n.nome, Rota: rota,
		Data: "2030-01-01", Horario: horario, AssentosTotais: assentos, PrecoPorTrecho: preco,
	}, protocolo.TipoPublicarCaronaRes, &r)
	if !r.Sucesso {
		t.Fatalf("publicar em %s: %s", n.nome, r.Mensagem)
	}
	return r.CaronaID
}

func (n *no) reservar(t *testing.T, passageiro string, trechos ...protocolo.TrechoViagem) protocolo.RespostaReservar {
	t.Helper()
	var r protocolo.RespostaReservar
	n.conectar(t).trocar(t, protocolo.SolicitacaoReserva{
		Tipo: protocolo.TipoReservarItinerarioReq, Passageiro: passageiro, Data: "2030-01-01", Trechos: trechos,
	}, protocolo.TipoReservarItinerarioRes, &r)
	return r
}

func (n *no) ocupados(t *testing.T, caronaID int, origem, destino string) int {
	t.Helper()
	var r protocolo.RespostaConsultarCaronas
	n.conectar(t).trocar(t, protocolo.ConsultaCaronasMotorista{
		Tipo: protocolo.TipoConsultarCaronasReq, Motorista: "mot-" + n.nome,
	}, protocolo.TipoConsultarCaronasRes, &r)
	for _, c := range r.Caronas {
		if c.ID != caronaID {
			continue
		}
		for _, tr := range c.Trechos {
			if tr.Origem == origem && tr.Destino == destino {
				return c.AssentosTotais - tr.AssentosLivres
			}
		}
	}
	t.Fatalf("trecho %s->%s da carona %d nao encontrado em %s", origem, destino, caronaID, n.nome)
	return -1
}

func tv(empresa string, caronaID int, origem, destino string) protocolo.TrechoViagem {
	return protocolo.TrechoViagem{Empresa: empresa, CaronaID: caronaID, Origem: origem, Destino: destino, Horario: "08:00"}
}

func postJSON(t *testing.T, url string, corpo any) int {
	t.Helper()
	var buf io.Reader
	if corpo != nil {
		b, _ := json.Marshal(corpo)
		buf = bytes.NewReader(b)
	}
	resp, err := http.Post(url, "application/json", buf)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func TestConsorcioReservaAtravessaEmpresas(t *testing.T) {
	c := novoConsorcio(t)
	a := c.A.publicar(t, []string{"Salvador", "Feira"}, "08:00", 2, 10)
	b := c.B.publicar(t, []string{"Feira", "Serrinha"}, "10:00", 2, 20)
	cc := c.C.publicar(t, []string{"Serrinha", "Teofilandia"}, "12:00", 2, 30)

	r := c.A.reservar(t, "ana",
		tv("A", a, "Salvador", "Feira"), tv("B", b, "Feira", "Serrinha"), tv("C", cc, "Serrinha", "Teofilandia"))
	if !r.Sucesso {
		t.Fatalf("reserva entre 3 empresas deveria ser confirmada: %s", r.Mensagem)
	}
	if o := c.A.ocupados(t, a, "Salvador", "Feira"); o != 1 {
		t.Errorf("A: ocupados=%d, esperado 1", o)
	}
	if o := c.B.ocupados(t, b, "Feira", "Serrinha"); o != 1 {
		t.Errorf("B: ocupados=%d, esperado 1", o)
	}
	if o := c.C.ocupados(t, cc, "Serrinha", "Teofilandia"); o != 1 {
		t.Errorf("C: ocupados=%d, esperado 1", o)
	}

	var rr protocolo.RespostaConsultarReservas
	c.A.conectar(t).trocar(t, protocolo.ConsultaReservasPassageiro{
		Tipo: protocolo.TipoConsultarReservasReq, Passageiro: "ana",
	}, protocolo.TipoConsultarReservasRes, &rr)
	if len(rr.Reservas) != 1 || rr.Reservas[0].PrecoTotal != 60 || rr.Reservas[0].TxID == "" {
		t.Fatalf("reserva registrada incorretamente: %+v", rr.Reservas)
	}
}

func TestConsorcioNaoConfirmaParcial(t *testing.T) {
	c := novoConsorcio(t)
	a := c.A.publicar(t, []string{"X", "Y"}, "08:00", 2, 10)
	b := c.B.publicar(t, []string{"Y", "Z"}, "09:00", 1, 20)

	if !c.B.reservar(t, "primeiro", tv("B", b, "Y", "Z")).Sucesso {
		t.Fatal("reserva inicial em B falhou")
	}

	if r := c.A.reservar(t, "segundo", tv("A", a, "X", "Y"), tv("B", b, "Y", "Z")); r.Sucesso {
		t.Fatal("itinerario com trecho lotado nao deveria ser confirmado")
	}
	if o := c.A.ocupados(t, a, "X", "Y"); o != 0 {
		t.Fatalf("vaga de A ficou presa/ocupada apos falha: ocupados=%d", o)
	}
	// E a vaga de A continua utilizavel.
	if !c.A.reservar(t, "terceiro", tv("A", a, "X", "Y")).Sucesso {
		t.Fatal("vaga de A deveria estar livre")
	}
}

func TestConsorcioContencaoComCoordenadoresCruzados(t *testing.T) {
	c := novoConsorcio(t)
	a := c.A.publicar(t, []string{"P", "Q"}, "08:00", 1, 10)
	b := c.B.publicar(t, []string{"Q", "R"}, "09:00", 1, 20)

	const clientes = 16
	var wg sync.WaitGroup
	var mu sync.Mutex
	sucessos := 0
	for i := 0; i < clientes; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			coord := c.A
			if i%2 == 1 {
				coord = c.B
			}
			r := coord.reservar(t, fmt.Sprintf("p%d", i), tv("A", a, "P", "Q"), tv("B", b, "Q", "R"))
			if r.Sucesso {
				mu.Lock()
				sucessos++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	oa, ob := c.A.ocupados(t, a, "P", "Q"), c.B.ocupados(t, b, "Q", "R")
	t.Logf("confirmados=%d ocupadosA=%d ocupadosB=%d", sucessos, oa, ob)
	if sucessos > 1 {
		t.Fatalf("a mesma vaga foi confirmada %d vezes", sucessos)
	}
	if oa != sucessos || ob != sucessos {
		t.Fatalf("estado inconsistente entre empresas: A=%d B=%d confirmados=%d", oa, ob, sucessos)
	}
}

func TestConsorcioQuedaERecuperacao(t *testing.T) {
	c := novoConsorcio(t)
	a := c.A.publicar(t, []string{"M", "N"}, "08:00", 1, 10)
	b := c.B.publicar(t, []string{"N", "O"}, "09:00", 5, 10)
	cc := c.C.publicar(t, []string{"O", "S"}, "10:00", 1, 10)

	c.C.matar()

	inicio := time.Now()
	r := c.A.reservar(t, "p1", tv("A", a, "M", "N"), tv("C", cc, "O", "S"))
	if r.Sucesso {
		t.Fatal("reserva dependente da empresa C (fora do ar) nao deveria ser confirmada")
	}
	if d := time.Since(inicio); d > 5*time.Second {
		t.Fatalf("a falha demorou %v para ser detectada", d)
	}
	if o := c.A.ocupados(t, a, "M", "N"); o != 0 {
		t.Fatalf("A ficou com assento ocupado apos falha de C: %d", o)
	}

	if !c.A.reservar(t, "p2", tv("A", a, "M", "N"), tv("B", b, "N", "O")).Sucesso {
		t.Fatal("A e B deveriam continuar operando com C fora do ar")
	}

	c.C.iniciar(t)
	if o := c.C.ocupados(t, cc, "O", "S"); o != 0 {
		t.Fatalf("assento de C deveria estar livre apos recuperacao, ocupados=%d", o)
	}
	b2 := c.B.publicar(t, []string{"N", "O"}, "09:30", 1, 10)
	a2 := c.A.publicar(t, []string{"M", "N"}, "08:30", 1, 10)
	if !c.A.reservar(t, "p3", tv("A", a2, "M", "N"), tv("B", b2, "N", "O"), tv("C", cc, "O", "S")).Sucesso {
		t.Fatal("reserva envolvendo C deveria funcionar apos a recuperacao")
	}
}

func TestConsorcioBloqueioOrfaoExpira(t *testing.T) {
	c := novoConsorcio(t)
	b := c.B.publicar(t, []string{"U", "V"}, "08:00", 1, 10)

	// Simula um coordenador que fez o prepare em B e morreu antes de decidir.
	st := postJSON(t, "http://"+c.B.http+protocolo.RotaPrepare, protocolo.PedidoBloqueio{
		TxID: "coordenador-morto", CaronaID: b, Origem: "U", Destino: "V",
		Passageiro: "fantasma", Data: "2030-01-01", Timestamp: 1,
	})
	if st != 200 {
		t.Fatalf("prepare direto deveria votar SIM, status %d", st)
	}

	if r := c.A.reservar(t, "p1", tv("B", b, "U", "V")); r.Sucesso {
		t.Fatal("a vaga esta bloqueada: a reserva nao deveria ser confirmada ainda")
	}

	time.Sleep(2800 * time.Millisecond)
	if r := c.A.reservar(t, "p2", tv("B", b, "U", "V")); !r.Sucesso {
		t.Fatalf("bloqueio deveria ter expirado e liberado a vaga: %s", r.Mensagem)
	}
	if st := postJSON(t, "http://"+c.B.http+"/api/v1/transacoes/coordenador-morto/commit", nil); st == 200 {
		t.Fatal("commit de transacao expirada nao pode ser aceito")
	}
	if o := c.B.ocupados(t, b, "U", "V"); o != 1 {
		t.Fatalf("B deveria ter exatamente 1 assento ocupado, tem %d", o)
	}
}

func TestConsorcioParticipanteReiniciaMantemVoto(t *testing.T) {
	c := novoConsorcio(t)
	b := c.B.publicar(t, []string{"G", "H"}, "08:00", 1, 10)

	if st := postJSON(t, "http://"+c.B.http+protocolo.RotaPrepare, protocolo.PedidoBloqueio{
		TxID: "tx-persistida", CaronaID: b, Origem: "G", Destino: "H",
		Passageiro: "ana", Data: "2030-01-01", Timestamp: 1,
	}); st != 200 {
		t.Fatalf("prepare: %d", st)
	}

	c.B.matar()
	c.B.iniciar(t)

	if st := postJSON(t, "http://"+c.B.http+"/api/v1/transacoes/tx-persistida/commit", nil); st != 200 {
		t.Fatalf("commit apos reinicio deveria funcionar (bloqueio persistido), status %d", st)
	}
	if o := c.B.ocupados(t, b, "G", "H"); o != 1 {
		t.Fatalf("ocupados=%d, esperado 1", o)
	}
}
