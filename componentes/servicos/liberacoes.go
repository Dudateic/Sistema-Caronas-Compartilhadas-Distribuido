package servicos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/modelos"
	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/rest"
	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/visual"
)

// Tipos de tarefa que precisam chegar a OUTRA empresa e não podem se perder se ela estiver fora do ar.
const (
	tarefaLiberarTrecho   = "LIBERAR_TRECHO"   // devolve o assento de um passageiro que cancelou/perdeu a reserva
	tarefaCaronaCancelada = "CARONA_CANCELADA" // avisa que uma carona foi cancelada pelo motorista
)

type tarefaRemota struct {
	Tipo       string `json:"tipo"`
	Empresa    string `json:"empresa"` // empresa de destino
	CaronaID   int    `json:"carona_id"`
	Origem     string `json:"origem,omitempty"`
	Destino    string `json:"destino,omitempty"`
	Passageiro string `json:"passageiro,omitempty"`
}

// OutboxRemoto guarda em disco as notificações destinadas a outras empresas e as reenvia
// periodicamente até serem entregues. Todas as operações são idempotentes no destino:
// liberar um trecho já liberado (404) e avisar duas vezes o cancelamento de uma carona não têm efeito extra.
type OutboxRemoto struct {
	mu      sync.Mutex
	caminho string
	tarefas []tarefaRemota
}

var InstanciaOutbox *OutboxRemoto

func NovoOutboxRemoto(diretorio string) *OutboxRemoto {
	o := &OutboxRemoto{caminho: filepath.Join(diretorio, "notificacoes_pendentes.json")}
	if b, err := os.ReadFile(o.caminho); err == nil && len(b) > 0 {
		_ = json.Unmarshal(b, &o.tarefas)
	}
	return o
}

func (o *OutboxRemoto) salvar() {
	b, err := json.MarshalIndent(o.tarefas, "", "  ")
	if err == nil {
		_ = os.WriteFile(o.caminho, b, 0644)
	}
}

// Pendentes devolve quantas notificações ainda não foram entregues.
func (o *OutboxRemoto) Pendentes() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.tarefas)
}

func (o *OutboxRemoto) enfileirar(t tarefaRemota) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, existente := range o.tarefas {
		if existente == t {
			return
		}
	}
	o.tarefas = append(o.tarefas, t)
	o.salvar()
}

func urlDoPar(nome string) (string, bool) {
	for n, u := range Config.Pares {
		if strings.EqualFold(n, nome) {
			return u, true
		}
	}
	return "", false
}

// entregar tenta uma vez. Devolve true quando não há mais nada a fazer (entregue ou destino desconhecido).
func entregar(t tarefaRemota) bool {
	url, ok := urlDoPar(t.Empresa)
	if !ok || InstanciaClienteREST == nil {
		return true // empresa fora da configuração: não adianta tentar de novo
	}
	switch t.Tipo {
	case tarefaLiberarTrecho:
		err := InstanciaClienteREST.EnviarCancelamentoTrecho(url, t.CaronaID, t.Origem, t.Destino, t.Passageiro)
		return err == nil || errors.Is(err, rest.ErrNaoEncontrado) // 404: já estava liberado
	case tarefaCaronaCancelada:
		err := InstanciaClienteREST.NotificarCaronaCancelada(url, Config.Empresa, t.CaronaID)
		if err != nil {
			visual.MensagemAviso(fmt.Sprintf("[Cancelamento] Aviso da carona #%d nao entregue a %s (%v); sera reenviado", t.CaronaID, t.Empresa, err))
			return false
		}
		visual.MensagemSucesso(fmt.Sprintf("[Cancelamento] Empresa %s avisada do cancelamento da carona #%d", t.Empresa, t.CaronaID))
		return true
	}
	return true
}

// ExecutarOuEnfileirar tenta entregar agora; se o destino não responder, guarda para reenviar depois.
// Devolve true se foi entregue na hora.
func executarOuEnfileirar(t tarefaRemota) bool {
	if entregar(t) {
		return true
	}
	if InstanciaOutbox != nil {
		InstanciaOutbox.enfileirar(t)
	}
	return false
}

func (o *OutboxRemoto) reenviar() {
	o.mu.Lock()
	copia := append([]tarefaRemota(nil), o.tarefas...)
	o.mu.Unlock()

	for _, t := range copia {
		if !entregar(t) {
			continue
		}
		o.mu.Lock()
		for i, existente := range o.tarefas {
			if existente == t {
				o.tarefas = append(o.tarefas[:i], o.tarefas[i+1:]...)
				break
			}
		}
		o.salvar()
		o.mu.Unlock()
	}
}

// IniciarReenvioNotificacoes reenvia periodicamente as notificações pendentes às outras empresas.
func IniciarReenvioNotificacoes(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if InstanciaOutbox != nil && InstanciaOutbox.Pendentes() > 0 {
					InstanciaOutbox.reenviar()
				}
			}
		}
	}()
}

// liberarOutrosTrechos devolve os assentos dos demais trechos de uma reserva desfeita:
// os desta empresa na hora, os de outras empresas por notificação confiável.
// `ignorar` identifica os trechos que não devem ser tocados (os da carona cancelada).
func liberarOutrosTrechos(passageiro string, trechos []modelos.TrechoViagem, ignorar func(modelos.TrechoViagem) bool) {
	for _, t := range trechos {
		if ignorar != nil && ignorar(t) {
			continue
		}
		if t.Empresa == "" || strings.EqualFold(t.Empresa, Config.Empresa) {
			if InstanciaCaronas != nil {
				_ = InstanciaCaronas.LiberarAssentoConfirmado(t.CaronaID, t.Origem, t.Destino, passageiro)
			}
			continue
		}
		tarefa := tarefaRemota{Tipo: tarefaLiberarTrecho, Empresa: t.Empresa, CaronaID: t.CaronaID, Origem: t.Origem, Destino: t.Destino, Passageiro: passageiro}
		go executarOuEnfileirar(tarefa)
	}
}
