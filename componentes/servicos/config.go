package service

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// ConfigConsorcio descreve a identidade desta empresa e como falar com as parceiras.
type ConfigConsorcio struct {
	Empresa     string            // nome desta empresa (ex.: "A")
	Pares       map[string]string // nome da empresa parceira -> URL base da API REST
	TTLBloqueio time.Duration     // tempo maximo que um assento fica bloqueado sem commit
	TimeoutPar  time.Duration     // timeout de cada chamada HTTP a uma parceira
}

// Config deve ser definida uma unica vez no inicio do processo, antes de atender requisicoes.
var Config = ConfigConsorcio{
	Empresa:     "VAIJUNTO",
	Pares:       map[string]string{},
	TTLBloqueio: 30 * time.Second,
	TimeoutPar:  3 * time.Second,
}

// ehLocal informa se o trecho pertence a esta empresa (campo vazio = local).
func ehLocal(empresa string) bool {
	return empresa == "" || empresa == Config.Empresa
}

// ParsePares le "B=http://host:8082,C=http://host:8083".
func ParsePares(texto string) (map[string]string, error) {
	pares := map[string]string{}
	for _, item := range strings.Split(texto, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		nome, url, ok := strings.Cut(item, "=")
		nome, url = strings.TrimSpace(nome), strings.TrimRight(strings.TrimSpace(url), "/")
		if !ok || nome == "" || url == "" {
			return nil, fmt.Errorf("par invalido %q (use Nome=http://host:porta)", item)
		}
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			return nil, fmt.Errorf("URL do par %q deve comecar com http:// ou https://", nome)
		}
		pares[nome] = url
	}
	return pares, nil
}

// relogioLamport e o relogio logico desta empresa. A precedencia entre transacoes
// concorrentes usa (timestamp, tx_id), nunca o relogio fisico.
type relogioLamport struct {
	mu    sync.Mutex
	valor int64
}

// Tick avanca o relogio para um evento local (ex.: iniciar uma transacao).
func (r *relogioLamport) Tick() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.valor++
	return r.valor
}

// Atualizar aplica a regra de recebimento: max(local, recebido) + 1.
func (r *relogioLamport) Atualizar(recebido int64) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if recebido > r.valor {
		r.valor = recebido
	}
	r.valor++
	return r.valor
}

func (r *relogioLamport) Valor() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.valor
}

var Relogio relogioLamport

// precede devolve true se a transacao 1 tem precedencia (e "mais antiga") sobre a 2.
// Ordem total: menor timestamp de Lamport; empate decidido pelo menor tx_id.
func precede(ts1 int64, tx1 string, ts2 int64, tx2 string) bool {
	if ts1 != ts2 {
		return ts1 < ts2
	}
	return tx1 < tx2
}
