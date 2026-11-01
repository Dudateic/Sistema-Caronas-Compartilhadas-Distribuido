package servicos

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/modelos"
)

var (
	ErrCaronaNaoEncontrada  = errors.New("carona nao encontrada")
	ErrTrechoNaoEncontrado  = errors.New("trecho nao encontrado na carona indicada")
	ErrSemAssentosLivres    = errors.New("nao ha assentos disponiveis no trecho")
	ErrPassageiroJaInscrito = errors.New("passageiro ja possui assento neste trecho")
	ErrPermissaoNegada      = errors.New("permissao negada para esta carona")
)

// bloqueioItem rastreia um assento temporariamente retido durante a Fase 1 do 2PC
type bloqueioItem struct {
	caronaID   int
	origem     string
	destino    string
	passageiro string
}

type GerenciadorCaronas struct {
	idEmpresa           string
	caminhoPersistencia string
	mu                  sync.RWMutex

	caronas         []modelos.Carona
	proximoID       int
	bloqueiosAtivos map[string]bloqueioItem // chave única: "caronaID:origem:destino:passageiro"
}

func NovoGerenciadorCaronas(idEmpresa, diretorioDados string) (*GerenciadorCaronas, error) {
	if err := os.MkdirAll(diretorioDados, 0755); err != nil {
		return nil, fmt.Errorf("falha ao criar pasta de dados: %w", err)
	}

	caminhoCompleto := filepath.Join(diretorioDados, "dados.json")

	gc := &GerenciadorCaronas{
		idEmpresa:           idEmpresa,
		caminhoPersistencia: caminhoCompleto,
		caronas:             make([]modelos.Carona, 0),
		proximoID:           1,
		bloqueiosAtivos:     make(map[string]bloqueioItem),
	}

	_ = gc.carregarDoDisco()
	_ = gc.salvarEmDisco()
	return gc, nil
}

// BuscarTrechos retorna todos os segmentos compatíveis com assentos livres
func (gc *GerenciadorCaronas) BuscarTrechos(origem, destino string) []modelos.TrechoViagem {
	gc.mu.RLock()
	defer gc.mu.RUnlock()

	var encontrados []modelos.TrechoViagem

	origemFiltro := strings.ToUpper(strings.TrimSpace(origem))
	destinoFiltro := strings.ToUpper(strings.TrimSpace(destino))

	for _, carona := range gc.caronas {
		for _, trecho := range carona.Trechos {
			tOrigem := strings.ToUpper(trecho.Origem)
			tDestino := strings.ToUpper(trecho.Destino)

			origemOk := origemFiltro == "" || tOrigem == origemFiltro
			destinoOk := destinoFiltro == "" || tDestino == destinoFiltro

			if origemOk && destinoOk && trecho.AssentosLivres > 0 {
				encontrados = append(encontrados, modelos.TrechoViagem{
					Empresa:   gc.idEmpresa,
					CaronaID:  carona.ID,
					Motorista: carona.Motorista,
					Origem:    trecho.Origem,
					Destino:   trecho.Destino,
					Data:      carona.Data,
					Horario:   carona.Horario,
					Preco:     trecho.Preco,
				})
			}
		}
	}

	return encontrados
}

// BloquearAssentoProvisorio é acionado na Fase 1 do 2PC (PREPARE)
func (gc *GerenciadorCaronas) BloquearAssentoProvisorio(caronaID int, origem, destino, passageiro string) error {
	gc.mu.Lock()
	defer gc.mu.Unlock()

	for i := range gc.caronas {
		if gc.caronas[i].ID == caronaID {
			for j := range gc.caronas[i].Trechos {
				t := &gc.caronas[i].Trechos[j]
				if strings.EqualFold(t.Origem, origem) && strings.EqualFold(t.Destino, destino) {
					// Verifica se há assentos disponíveis
					if t.AssentosLivres <= 0 {
						return ErrSemAssentosLivres
					}

					// Bloqueia temporariamente 1 assento
					t.AssentosLivres--

					chave := fmt.Sprintf("%d:%s:%s:%s:%d", caronaID, strings.ToUpper(origem), strings.ToUpper(destino), passageiro, time.Now().UnixNano())
					gc.bloqueiosAtivos[chave] = bloqueioItem{
						caronaID:   caronaID,
						origem:     strings.ToUpper(origem),
						destino:    strings.ToUpper(destino),
						passageiro: passageiro,
					}

					_ = gc.salvarEmDisco()
					return nil
				}
			}
			return ErrTrechoNaoEncontrado
		}
	}

	return ErrCaronaNaoEncontrada
}

// LiberarAssentoProvisorio é acionado na Fase 2 se houver ABORT
func (gc *GerenciadorCaronas) LiberarAssentoProvisorio(caronaID int, origem, destino, passageiro string) error {
	gc.mu.Lock()
	defer gc.mu.Unlock()

	// Remove o bloqueio ativo correspondente
	for chave, item := range gc.bloqueiosAtivos {
		if item.caronaID == caronaID && strings.EqualFold(item.passageiro, passageiro) &&
			strings.EqualFold(item.origem, origem) && strings.EqualFold(item.destino, destino) {
			delete(gc.bloqueiosAtivos, chave)
			break
		}
	}

	for i := range gc.caronas {
		if gc.caronas[i].ID == caronaID {
			for j := range gc.caronas[i].Trechos {
				t := &gc.caronas[i].Trechos[j]
				if strings.EqualFold(t.Origem, origem) && strings.EqualFold(t.Destino, destino) {
					t.AssentosLivres++
					_ = gc.salvarEmDisco()
					return nil
				}
			}
			return ErrTrechoNaoEncontrado
		}
	}

	return ErrCaronaNaoEncontrada
}

// ConfirmarAssentoDefinitivo é acionado na Fase 2 quando ocorre GLOBAL COMMIT
func (gc *GerenciadorCaronas) ConfirmarAssentoDefinitivo(caronaID int, origem, destino, passageiro string) error {
	gc.mu.Lock()
	defer gc.mu.Unlock()

	// 1. Limpa o registo do bloqueio provisório
	for chave, item := range gc.bloqueiosAtivos {
		if item.caronaID == caronaID && strings.EqualFold(item.passageiro, passageiro) &&
			strings.EqualFold(item.origem, origem) && strings.EqualFold(item.destino, destino) {
			delete(gc.bloqueiosAtivos, chave)
			break
		}
	}

	// 2. Regista o passageiro no trecho
	for i := range gc.caronas {
		if gc.caronas[i].ID == caronaID {
			for j := range gc.caronas[i].Trechos {
				t := &gc.caronas[i].Trechos[j]
				if strings.EqualFold(t.Origem, origem) && strings.EqualFold(t.Destino, destino) {
					t.Passageiros = append(t.Passageiros, passageiro)

					if err := gc.salvarEmDisco(); err != nil {
						return fmt.Errorf("falha ao persistir confirmacao em disco: %w", err)
					}
					return nil
				}
			}
			return ErrTrechoNaoEncontrado
		}
	}

	return ErrCaronaNaoEncontrada
}

// LiberarAssentoConfirmado é acionado no cancelamento definitivo de uma reserva confirmada
func (gc *GerenciadorCaronas) LiberarAssentoConfirmado(caronaID int, origem, destino, passageiro string) error {
	gc.mu.Lock()
	defer gc.mu.Unlock()

	for i := range gc.caronas {
		if gc.caronas[i].ID == caronaID {
			for j := range gc.caronas[i].Trechos {
				t := &gc.caronas[i].Trechos[j]
				if strings.EqualFold(t.Origem, origem) && strings.EqualFold(t.Destino, destino) {
					novo := make([]string, 0, len(t.Passageiros))
					encontrou := false

					for _, p := range t.Passageiros {
						if strings.EqualFold(p, passageiro) && !encontrou {
							encontrou = true
							continue // Remove o passageiro
						}
						novo = append(novo, p)
					}

					if !encontrou {
						return errors.New("passageiro nao encontrado no trecho indicado")
					}

					t.Passageiros = novo
					t.AssentosLivres++

					if err := gc.salvarEmDisco(); err != nil {
						return fmt.Errorf("falha ao persistir desocupacao em disco: %w", err)
					}
					return nil
				}
			}
			return ErrTrechoNaoEncontrado
		}
	}

	return ErrCaronaNaoEncontrada
}

// PublicarCarona divide a sequência de cidades em trechos contíguos
func (gc *GerenciadorCaronas) PublicarCarona(motorista, data, horario string, rota []string, assentos int, precoTrecho float64) (int, error) {
	if len(rota) < 2 {
		return 0, errors.New("rota necessita de no minimo duas cidades")
	}
	if assentos <= 0 {
		return 0, errors.New("total de assentos deve ser maior que zero")
	}
	if precoTrecho <= 0 {
		return 0, errors.New("preco por trecho deve ser maior que zero")
	}

	gc.mu.Lock()
	defer gc.mu.Unlock()

	var trechos []modelos.Trecho
	for i := 0; i < len(rota)-1; i++ {
		trechos = append(trechos, modelos.Trecho{
			Origem:         strings.ToUpper(strings.TrimSpace(rota[i])),
			Destino:        strings.ToUpper(strings.TrimSpace(rota[i+1])),
			AssentosLivres: assentos,
			Preco:          precoTrecho,
			Passageiros:    make([]string, 0),
		})
	}

	rotaNormalizada := make([]string, len(rota))
	for i, c := range rota {
		rotaNormalizada[i] = strings.ToUpper(strings.TrimSpace(c))
	}

	novaCarona := modelos.Carona{
		ID:             gc.proximoID,
		Motorista:      motorista,
		Data:           data,
		Horario:        horario,
		AssentosTotais: assentos,
		PrecoPorTrecho: precoTrecho,
		Rota:           rotaNormalizada,
		Trechos:        trechos,
	}

	gc.caronas = append(gc.caronas, novaCarona)
	idGerado := gc.proximoID
	gc.proximoID++

	_ = gc.salvarEmDisco()
	return idGerado, nil
}

// ObterCaronasMotorista filtra as caronas pertencentes ao motorista
func (gc *GerenciadorCaronas) ObterCaronasMotorista(motorista string) []modelos.Carona {
	gc.mu.RLock()
	defer gc.mu.RUnlock()

	var resultado []modelos.Carona
	for _, c := range gc.caronas {
		if strings.EqualFold(c.Motorista, motorista) {
			resultado = append(resultado, c)
		}
	}
	return resultado
}

// CancelarCarona remove a viagem caso ela pertença ao motorista
func (gc *GerenciadorCaronas) CancelarCarona(caronaID int, motorista string) error {
	gc.mu.Lock()
	defer gc.mu.Unlock()

	indice := -1
	for i, c := range gc.caronas {
		if c.ID == caronaID {
			if !strings.EqualFold(c.Motorista, motorista) {
				return ErrPermissaoNegada
			}
			indice = i
			break
		}
	}

	if indice == -1 {
		return ErrCaronaNaoEncontrada
	}

	gc.caronas = append(gc.caronas[:indice], gc.caronas[indice+1:]...)
	_ = gc.salvarEmDisco()
	return nil
}

// ObterTrechosOcupados retorna todos os passageiros alocados em trechos desta empresa
func (gc *GerenciadorCaronas) ObterTrechosOcupados() []modelos.TrechoOcupadoInfo {
	gc.mu.RLock()
	defer gc.mu.RUnlock()

	var ocupados []modelos.TrechoOcupadoInfo
	for _, c := range gc.caronas {
		for _, t := range c.Trechos {
			for _, pass := range t.Passageiros {
				ocupados = append(ocupados, modelos.TrechoOcupadoInfo{
					CaronaID:   c.ID,
					Origem:     t.Origem,
					Destino:    t.Destino,
					Passageiro: pass,
				})
			}
		}
	}
	return ocupados
}

// LiberarTrechosEmLote remove passageiros cancelados e devolve os assentos livres
func (gc *GerenciadorCaronas) LiberarTrechosEmLote(itens []modelos.TrechoOcupadoInfo) int {
	if len(itens) == 0 {
		return 0
	}

	gc.mu.Lock()
	defer gc.mu.Unlock()

	alterados := 0
	for _, item := range itens {
		for i := range gc.caronas {
			if gc.caronas[i].ID == item.CaronaID {
				for j := range gc.caronas[i].Trechos {
					t := &gc.caronas[i].Trechos[j]
					if strings.EqualFold(t.Origem, item.Origem) && strings.EqualFold(t.Destino, item.Destino) {
						novoSlice := make([]string, 0, len(t.Passageiros))
						encontrou := false
						for _, p := range t.Passageiros {
							if strings.EqualFold(p, item.Passageiro) && !encontrou {
								encontrou = true
								continue
							}
							novoSlice = append(novoSlice, p)
						}

						if encontrou {
							t.Passageiros = novoSlice
							t.AssentosLivres++
							alterados++
						}
					}
				}
			}
		}
	}

	if alterados > 0 {
		_ = gc.salvarEmDisco()
	}
	return alterados
}

// ConfirmarAssentoSeAusente confirma o assento somente se o passageiro ainda não está no trecho.
// Usado ao reaplicar um COMMIT na recuperação, quando a confirmação pode já ter sido gravada.
func (gc *GerenciadorCaronas) ConfirmarAssentoSeAusente(caronaID int, origem, destino, passageiro string) error {
	gc.mu.Lock()
	for i := range gc.caronas {
		if gc.caronas[i].ID != caronaID {
			continue
		}
		for j := range gc.caronas[i].Trechos {
			t := &gc.caronas[i].Trechos[j]
			if strings.EqualFold(t.Origem, origem) && strings.EqualFold(t.Destino, destino) {
				for _, p := range t.Passageiros {
					if strings.EqualFold(p, passageiro) {
						gc.mu.Unlock()
						return nil
					}
				}
			}
		}
	}
	gc.mu.Unlock()
	return gc.ConfirmarAssentoDefinitivo(caronaID, origem, destino, passageiro)
}

// ReconciliarAssentos restaura a invariante AssentosLivres = totais - passageiros - pendentes.
// Chamado após a recuperação, quando só o log sabe quais prepares ainda aguardam decisão.
// Repara tanto o assento "preso" (decrementado sem voto gravado) quanto o contrário.
// A chave de pendentes segue distribuido.ChaveTrecho.
func (gc *GerenciadorCaronas) ReconciliarAssentos(pendentes map[string]int) {
	gc.mu.Lock()
	defer gc.mu.Unlock()

	alterou := false
	for i := range gc.caronas {
		c := &gc.caronas[i]
		for j := range c.Trechos {
			t := &c.Trechos[j]
			chave := fmt.Sprintf("%d|%s|%s", c.ID, strings.ToUpper(t.Origem), strings.ToUpper(t.Destino))
			esperado := c.AssentosTotais - len(t.Passageiros) - pendentes[chave]
			if esperado < 0 {
				esperado = 0
			}
			if t.AssentosLivres != esperado {
				t.AssentosLivres = esperado
				alterou = true
			}
		}
	}
	gc.bloqueiosAtivos = make(map[string]bloqueioItem)
	if alterou {
		_ = gc.salvarEmDisco()
	}
}

// Snapshot devolve uma cópia profunda das caronas (usada por auditoria e testes).
func (gc *GerenciadorCaronas) Snapshot() []modelos.Carona {
	gc.mu.RLock()
	defer gc.mu.RUnlock()
	copia := make([]modelos.Carona, len(gc.caronas))
	for i, c := range gc.caronas {
		copia[i] = c
		copia[i].Trechos = make([]modelos.Trecho, len(c.Trechos))
		for j, t := range c.Trechos {
			copia[i].Trechos[j] = t
			copia[i].Trechos[j].Passageiros = append([]string(nil), t.Passageiros...)
		}
	}
	return copia
}
