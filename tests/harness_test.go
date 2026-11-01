package tests

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/distribuido"
	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/modelos"
	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/rest"
	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/servicos"
)

func TestMain(m *testing.M) {
	// Escrito uma única vez, antes de qualquer teste: reenvios da fase 2 em segundo plano leem este valor.
	distribuido.IntervaloReenvioFase2 = 100 * time.Millisecond
	os.Exit(m.Run())
}

// no é uma empresa do consórcio rodando em processo: tem diretório de dados próprio,
// logs de transação próprios e uma API REST em porta HTTP real. derrubar() simula a queda
// (perde toda a memória) e iniciar() simula o reinício (relê o disco).
type no struct {
	id    string
	dir   string
	addr  string
	pares map[string]string

	caronas *servicos.GerenciadorCaronas
	part    *distribuido.Participante2PC
	coord   *distribuido.Coordenador2PC
	srv     *http.Server
	wals    []*distribuido.LogTransacoes
	cliente *rest.ClienteRESTInterEmpresas
}

type consorcio struct {
	nos map[string]*no
}

func (c *consorcio) url(id string) string { return "http://" + c.nos[id].addr }

// novoConsorcio sobe as empresas informadas, todas conhecendo umas às outras.
func novoConsorcio(t *testing.T, ids ...string) *consorcio {
	t.Helper()
	c := &consorcio{nos: map[string]*no{}}
	for _, id := range ids {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		c.nos[id] = &no{id: id, dir: filepath.Join(t.TempDir(), "empresa_"+id), addr: l.Addr().String()}
		_ = l.Close() // reabrimos o mesmo endereço em iniciar()
	}
	for _, id := range ids {
		n := c.nos[id]
		n.pares = map[string]string{}
		for _, outro := range ids {
			if outro != id {
				n.pares[outro] = c.url(outro)
			}
		}
	}
	for _, id := range ids {
		c.nos[id].iniciar(t)
	}
	t.Cleanup(func() {
		for _, n := range c.nos {
			n.derrubar()
		}
	})
	return c
}

func (n *no) iniciar(t *testing.T) {
	t.Helper()
	var err error
	n.caronas, err = servicos.NovoGerenciadorCaronas(n.id, n.dir)
	if err != nil {
		t.Fatalf("caronas %s: %v", n.id, err)
	}
	walP, err := distribuido.NovoLogTransacoes(filepath.Join(n.dir, "participante.log"))
	if err != nil {
		t.Fatal(err)
	}
	walC, err := distribuido.NovoLogTransacoes(filepath.Join(n.dir, "coordenador.log"))
	if err != nil {
		t.Fatal(err)
	}
	n.wals = []*distribuido.LogTransacoes{walP, walC}

	n.part, err = distribuido.NovoParticipante2PC(n.id, n.caronas, walP)
	if err != nil {
		t.Fatalf("participante %s: %v", n.id, err)
	}
	n.cliente = rest.NovoClienteREST(400 * time.Millisecond)
	n.coord = distribuido.NovoCoordenador2PC(n.id, "", n.cliente, walC, n.part, n.pares)
	n.coord.DefinirTimeoutPrepare(800 * time.Millisecond)
	n.part.DefinirConsultaDecisao(func(coordenador, txID string) (string, error) {
		if coordenador == n.id {
			return n.coord.Decisao(txID), nil
		}
		u, ok := n.pares[coordenador]
		if !ok {
			return "", fmt.Errorf("coordenador desconhecido")
		}
		return n.cliente.ConsultarDecisao(u, txID)
	})
	n.part.DefinirConsultaPares(func(empresa, txID string) (string, error) {
		u, ok := n.pares[empresa]
		if !ok {
			return "", fmt.Errorf("empresa desconhecida")
		}
		return n.cliente.ConsultarEstado(u, txID)
	})
	if err := n.coord.Recuperar(); err != nil {
		t.Fatalf("recuperar coordenador %s: %v", n.id, err)
	}

	mux := http.NewServeMux()
	rest.NovoHandlersREST(n.id, n.caronas, n.part, n.coord, nil, nil).RegistrarRotas(mux)
	var l net.Listener
	for i := 0; i < 50; i++ { // a porta pode demorar um instante para ser liberada
		l, err = net.Listen("tcp", n.addr)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("listen %s: %v", n.addr, err)
	}
	srv := &http.Server{Handler: mux}
	n.srv = srv
	go func() { _ = srv.Serve(l) }()
}

// derrubar simula a queda: fecha a API e descarta tudo o que estava só em memória.
func (n *no) derrubar() {
	if n.srv != nil {
		_ = n.srv.Close()
		n.srv = nil
	}
	for _, w := range n.wals {
		_ = w.Fechar()
	}
	n.wals = nil
}

func (n *no) publicar(t *testing.T, rota []string, assentos int, preco float64) int {
	t.Helper()
	id, err := n.caronas.PublicarCarona("motorista_"+n.id, "2030-01-01", "08:00", rota, assentos, preco)
	if err != nil {
		t.Fatalf("publicar: %v", err)
	}
	return id
}

func trecho(empresa string, caronaID int, origem, destino string) modelos.TrechoViagem {
	return modelos.TrechoViagem{Empresa: empresa, CaronaID: caronaID, Origem: origem, Destino: destino, Data: "2030-01-01", Preco: 10}
}

var contadorTx int64
var muTx sync.Mutex

func novoTx(prefixo string) string {
	muTx.Lock()
	defer muTx.Unlock()
	contadorTx++
	return fmt.Sprintf("tx_%s_%d", prefixo, contadorTx)
}

func (n *no) reservar(passageiro string, trechos ...modelos.TrechoViagem) (bool, string) {
	ok, motivo, _ := n.coord.ExecutarReservaAtomica(novoTx(n.id), passageiro, modelos.Itinerario{Trechos: trechos}, 0)
	return ok, motivo
}

func (n *no) livres(caronaID int, origem, destino string) int {
	for _, c := range n.caronas.Snapshot() {
		if c.ID != caronaID {
			continue
		}
		for _, t := range c.Trechos {
			if t.Origem == origem && t.Destino == destino {
				return t.AssentosLivres
			}
		}
	}
	return -1
}

func (n *no) passageiros(caronaID int, origem, destino string) []string {
	for _, c := range n.caronas.Snapshot() {
		if c.ID != caronaID {
			continue
		}
		for _, t := range c.Trechos {
			if t.Origem == origem && t.Destino == destino {
				return t.Passageiros
			}
		}
	}
	return nil
}

// invariantes verifica, em todas as empresas, que nenhum trecho está superlotado e que
// livres + passageiros + pendentes == totais (nenhum assento vazado ou preso).
func (c *consorcio) invariantes(t *testing.T) {
	t.Helper()
	for id, n := range c.nos {
		pend := n.part.PendentesAtivas()
		for _, car := range n.caronas.Snapshot() {
			for _, tr := range car.Trechos {
				if tr.AssentosLivres < 0 {
					t.Errorf("[%s] carona %d %s->%s: assentos negativos (%d)", id, car.ID, tr.Origem, tr.Destino, tr.AssentosLivres)
				}
				if len(tr.Passageiros) > car.AssentosTotais {
					t.Errorf("[%s] carona %d %s->%s: OVERBOOKING %d passageiros para %d assentos", id, car.ID, tr.Origem, tr.Destino, len(tr.Passageiros), car.AssentosTotais)
				}
				if pend == 0 && tr.AssentosLivres+len(tr.Passageiros) != car.AssentosTotais {
					t.Errorf("[%s] carona %d %s->%s: livres(%d)+passageiros(%d) != totais(%d) sem transacao pendente",
						id, car.ID, tr.Origem, tr.Destino, tr.AssentosLivres, len(tr.Passageiros), car.AssentosTotais)
				}
			}
		}
	}
}

// esperar repete a condição até ela ficar verdadeira ou o prazo estourar.
func esperar(t *testing.T, prazo time.Duration, descricao string, cond func() bool) {
	t.Helper()
	limite := time.Now().Add(prazo)
	for time.Now().Before(limite) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("condicao nao atingida no prazo: %s", descricao)
}
