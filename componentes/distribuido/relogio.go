package distribuido

import "sync"

// RelogioLamport implementa um relógio lógico de Lamport.
// Cada servidor incrementa o relógio a cada evento local relevante e, ao receber
// uma mensagem, avança para max(local, recebido)+1. Assim, se o evento A causou o
// evento B, o carimbo de A é menor que o de B, sem depender de relógios físicos
// sincronizados entre as empresas.
type RelogioLamport struct {
	mu    sync.Mutex
	valor int64
}

// Tick registra um evento local (ou envio de mensagem) e devolve o novo carimbo.
func (r *RelogioLamport) Tick() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.valor++
	return r.valor
}

// Atualizar incorpora o carimbo recebido em uma mensagem e devolve o novo valor local.
func (r *RelogioLamport) Atualizar(recebido int64) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if recebido > r.valor {
		r.valor = recebido
	}
	r.valor++
	return r.valor
}

// Valor devolve o carimbo atual sem alterá-lo.
func (r *RelogioLamport) Valor() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.valor
}
