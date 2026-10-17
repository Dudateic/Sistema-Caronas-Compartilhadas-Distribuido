package servicos

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

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
	caminhoCompleto := filepath.Join(diretorioDados, "dados.json")

	gc := &GerenciadorCaronas{
		idEmpresa:           idEmpresa,
		caminhoPersistencia: caminhoCompleto,
		caronas:             make([]modelos.Carona, 0),
		proximoID:           1,
		bloqueiosAtivos:     make(map[string]bloqueioItem),
	}

	_ = gc.carregarDoDisco()
	_ = gc.salvarEmDisco() // Força a criação do ficheiro no disco logo no arranque!
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

			// Se origem ou destino forem vazios, devolve todos os trechos livres para montagem via grafo (DFS)
			origemOk := origemFiltro == "" || tOrigem == origemFiltro
			destinoOk := destinoFiltro == "" || tDestino == destinoFiltro

			if origemOk && destinoOk && trecho.AssentosLivres > 0 {
				encontrados = append(encontrados, modelos.TrechoViagem{
					Empresa:   gc.idEmpresa,
					CaronaID:  carona.ID,
					Motorista: carona.Motorista,
					Origem:    trecho.Origem,
					Destino:   trecho.Destino,
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

	chave := fmt.Sprintf("%d:%s:%s:%s", caronaID, origem, destino, passageiro)
	if _, jaBloqueou := gc.bloqueiosAtivos[chave]; jaBloqueou {
		return ErrPassageiroJaInscrito
	}

	for i := range gc.caronas {
		if gc.caronas[i].ID == caronaID {
			for j := range gc.caronas[i].Trechos {
				t := &gc.caronas[i].Trechos[j]
				if strings.EqualFold(t.Origem, origem) && strings.EqualFold(t.Destino, destino) {
					// Verifica se o passageiro já tem reserva confirmada
					for _, p := range t.Passageiros {
						if p == passageiro {
							return ErrPassageiroJaInscrito
						}
					}

					if t.AssentosLivres <= 0 {
						return ErrSemAssentosLivres
					}

					// Bloqueia temporariamente 1 assento
					t.AssentosLivres--
					gc.bloqueiosAtivos[chave] = bloqueioItem{
						caronaID:   caronaID,
						origem:     origem,
						destino:    destino,
						passageiro: passageiro,
					}
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

	chave := fmt.Sprintf("%d:%s:%s:%s", caronaID, origem, destino, passageiro)
	if _, existe := gc.bloqueiosAtivos[chave]; !existe {
		return nil // Já liberado ou nunca bloqueado (idempotência)
	}
	delete(gc.bloqueiosAtivos, chave)

	for i := range gc.caronas {
		if gc.caronas[i].ID == caronaID {
			for j := range gc.caronas[i].Trechos {
				t := &gc.caronas[i].Trechos[j]
				if strings.EqualFold(t.Origem, origem) && strings.EqualFold(t.Destino, destino) {
					t.AssentosLivres++
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

	chave := fmt.Sprintf("%d:%s:%s:%s", caronaID, origem, destino, passageiro)
	delete(gc.bloqueiosAtivos, chave)

	for i := range gc.caronas {
		if gc.caronas[i].ID == caronaID {
			for j := range gc.caronas[i].Trechos {
				t := &gc.caronas[i].Trechos[j]
				if strings.EqualFold(t.Origem, origem) && strings.EqualFold(t.Destino, destino) {
					t.Passageiros = append(t.Passageiros, passageiro)
					_ = gc.salvarEmDisco()
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

	// Normaliza as cidades da rota
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
		if c.Motorista == motorista {
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
			if c.Motorista != motorista {
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
