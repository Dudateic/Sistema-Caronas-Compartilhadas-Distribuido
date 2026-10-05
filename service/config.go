package service

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type ConfigConsorcio struct {
	Empresa     string
	Pares       map[string]string
	TTLBloqueio time.Duration
	TimeoutPar  time.Duration
}

var Config = ConfigConsorcio{
	Empresa:     "VAIJUNTO",
	Pares:       map[string]string{},
	TTLBloqueio: 30 * time.Second,
	TimeoutPar:  3 * time.Second,
}

func ehLocal(empresa string) bool {
	return empresa == "" || empresa == Config.Empresa
}

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

type relogioLamport struct {
	mu    sync.Mutex
	valor int64
}

func (r *relogioLamport) Tick() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.valor++
	return r.valor
}

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

func precede(ts1 int64, tx1 string, ts2 int64, tx2 string) bool {
	if ts1 != ts2 {
		return ts1 < ts2
	}
	return tx1 < tx2
}
