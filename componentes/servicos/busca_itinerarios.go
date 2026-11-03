package servicos

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/modelos"
)

// chaveTrecho identifica um trecho de forma única no consórcio:
// empresa + carona + origem + destino.
func chaveTrecho(t modelos.TrechoViagem) string {
	return fmt.Sprintf("%s|%d|%s|%s",
		strings.ToUpper(strings.TrimSpace(t.Empresa)),
		t.CaronaID,
		strings.ToUpper(strings.TrimSpace(t.Origem)),
		strings.ToUpper(strings.TrimSpace(t.Destino)))
}

// deduplicarTrechos remove trechos repetidos (ex.: o mesmo trecho vindo do catálogo local
// e de uma consulta a parceiro, ou devolvido duas vezes), preservando a ordem.
func deduplicarTrechos(trechos []modelos.TrechoViagem) []modelos.TrechoViagem {
	vistos := make(map[string]bool, len(trechos))
	saida := make([]modelos.TrechoViagem, 0, len(trechos))
	for _, t := range trechos {
		k := chaveTrecho(t)
		if vistos[k] {
			continue
		}
		vistos[k] = true
		saida = append(saida, t)
	}
	return saida
}

// chaveItinerario é a assinatura de um itinerário: a sequência de trechos que o compõe.
func chaveItinerario(trechos []modelos.TrechoViagem) string {
	partes := make([]string, len(trechos))
	for i, t := range trechos {
		partes[i] = chaveTrecho(t)
	}
	return strings.Join(partes, ">")
}

// MontarItinerarios faz a busca em profundidade (DFS) sobre os trechos do consórcio e
// devolve itinerários distintos de origem até destino. Não repete cidade dentro do
// itinerário, não repete trecho e não devolve o mesmo itinerário duas vezes.
func MontarItinerarios(trechos []modelos.TrechoViagem, origem, destino, data string) []modelos.Itinerario {
	origem = strings.ToUpper(strings.TrimSpace(origem))
	destino = strings.ToUpper(strings.TrimSpace(destino))
	data = strings.TrimSpace(data)

	// 1. Trechos únicos e já filtrados pela data pedida.
	unicos := deduplicarTrechos(trechos)
	porOrigem := make(map[string][]modelos.TrechoViagem)
	for _, t := range unicos {
		if data != "" && t.Data != "" && !strings.EqualFold(t.Data, data) {
			continue
		}
		o := strings.ToUpper(strings.TrimSpace(t.Origem))
		porOrigem[o] = append(porOrigem[o], t)
	}

	itinerarios := []modelos.Itinerario{}
	assinaturas := make(map[string]bool)
	visitadas := make(map[string]bool)
	caminho := make([]modelos.TrechoViagem, 0, 8)

	var dfs func(atual string, preco float64)
	dfs = func(atual string, preco float64) {
		if atual == destino && len(caminho) > 0 {
			sig := chaveItinerario(caminho)
			if assinaturas[sig] {
				return
			}
			assinaturas[sig] = true

			copia := make([]modelos.TrechoViagem, len(caminho))
			copy(copia, caminho)

			dataViagem := data
			if dataViagem == "" {
				dataViagem = copia[0].Data
				if dataViagem == "" {
					dataViagem = time.Now().Format("2006-01-02")
				}
			}
			itinerarios = append(itinerarios, modelos.Itinerario{
				Data:       dataViagem,
				PrecoTotal: preco,
				Trechos:    copia,
			})
			return
		}

		visitadas[atual] = true
		defer func() { visitadas[atual] = false }()

		for _, t := range porOrigem[atual] {
			prox := strings.ToUpper(strings.TrimSpace(t.Destino))
			if visitadas[prox] {
				continue
			}
			caminho = append(caminho, t)
			dfs(prox, preco+t.Preco)
			caminho = caminho[:len(caminho)-1]
		}
	}

	dfs(origem, 0)

	sort.SliceStable(itinerarios, func(i, j int) bool {
		if itinerarios[i].PrecoTotal != itinerarios[j].PrecoTotal {
			return itinerarios[i].PrecoTotal < itinerarios[j].PrecoTotal
		}
		return len(itinerarios[i].Trechos) < len(itinerarios[j].Trechos)
	})
	return itinerarios
}
