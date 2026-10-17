package servicos

import (
	"fmt"
	"strings"
	"time"
)

// ConfigConsorcio agrega as definições do nó local no consórcio
type ConfigConsorcio struct {
	Empresa     string
	Pares       map[string]string // Empresa -> URL REST (ex: "B" -> "http://empresa_b:8080")
	TTLBloqueio time.Duration
	TimeoutPar  time.Duration
}

// Variáveis globais compartilhadas no pacote servicos
var (
	Config         ConfigConsorcio
	DiretorioDados = "dados"
)

// ParsePares converte a string de parceiras "B=http://host:8080,C=http://host:8080" em map
func ParsePares(texto string) (map[string]string, error) {
	pares := make(map[string]string)
	texto = strings.TrimSpace(texto)
	if texto == "" {
		return pares, nil
	}

	entradas := strings.Split(texto, ",")
	for _, entrada := range entradas {
		entrada = strings.TrimSpace(entrada)
		if entrada == "" {
			continue
		}
		partes := strings.SplitN(entrada, "=", 2)
		if len(partes) != 2 {
			return nil, fmt.Errorf("formato invalido de parceira (esperado NOME=URL): %q", entrada)
		}
		nome := strings.TrimSpace(partes[0])
		url := strings.TrimRight(strings.TrimSpace(partes[1]), "/")
		if nome == "" || url == "" {
			return nil, fmt.Errorf("nome ou URL vazios na definicao de par: %q", entrada)
		}
		pares[nome] = url
	}

	return pares, nil
}
