package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/modelos"
)

const (
	TimeoutPadraoHTTP = 3 * time.Second
)

// ErrNaoEncontrado indica HTTP 404: o recurso já não existe no destino (por exemplo, assento já liberado).
var ErrNaoEncontrado = errors.New("recurso nao encontrado no destino")

// ClienteRESTInterEmpresas realiza chamadas HTTP para os servidores das outras empresas do consórcio
type ClienteRESTInterEmpresas struct {
	httpClient *http.Client
}

// NovoClienteREST cria um cliente HTTP com timeouts bem calibrados para evitar nós congelados
func NovoClienteREST(timeout ...time.Duration) *ClienteRESTInterEmpresas {
	limite := TimeoutPadraoHTTP
	if len(timeout) > 0 && timeout[0] > 0 {
		limite = timeout[0]
	}

	return &ClienteRESTInterEmpresas{
		httpClient: &http.Client{
			Timeout: limite,
		},
	}
}

// ConsultarTrechos busca os trechos disponíveis em uma empresa remota específica
func (c *ClienteRESTInterEmpresas) ConsultarTrechos(enderecoBase, origem, destino string) ([]modelos.TrechoViagem, error) {
	urlAlvo := fmt.Sprintf("%s/api/v1/trechos?origem=%s&destino=%s",
		enderecoBase,
		url.QueryEscape(origem),
		url.QueryEscape(destino),
	)

	ctx, cancel := context.WithTimeout(context.Background(), TimeoutPadraoHTTP)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlAlvo, nil)
	if err != nil {
		return nil, fmt.Errorf("falha ao criar requisicao de busca: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("falha na comunicacao com %s: %w", enderecoBase, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("servidor respondeu com status HTTP %d", resp.StatusCode)
	}

	var trechos []modelos.TrechoViagem
	if err := json.NewDecoder(resp.Body).Decode(&trechos); err != nil {
		return nil, fmt.Errorf("falha ao decodificar JSON dos trechos: %w", err)
	}

	return trechos, nil
}

// EnviarPrepare envia o pedido de reserva provisória na Fase 1 do 2PC
func (c *ClienteRESTInterEmpresas) EnviarPrepare(enderecoBase string, prepareReq modelos.PrepareRequest) (modelos.PrepareResponse, error) {
	urlAlvo := fmt.Sprintf("%s/api/v1/transacoes/prepare", enderecoBase)

	payloadBytes, err := json.Marshal(prepareReq)
	if err != nil {
		return modelos.PrepareResponse{TxID: prepareReq.TxID, VotoSim: false}, fmt.Errorf("erro ao serializar prepare: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), TimeoutPadraoHTTP)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlAlvo, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return modelos.PrepareResponse{TxID: prepareReq.TxID, VotoSim: false}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return modelos.PrepareResponse{
			TxID:    prepareReq.TxID,
			VotoSim: false,
			Motivo:  fmt.Sprintf("queda ou timeout ao contactar %s", enderecoBase),
		}, err
	}
	defer resp.Body.Close()

	// 200 = voto SIM, 409 = voto NAO (ambos trazem o corpo); qualquer outro codigo e erro
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict {
		return modelos.PrepareResponse{TxID: prepareReq.TxID, VotoSim: false},
			fmt.Errorf("%s respondeu HTTP %d ao prepare", enderecoBase, resp.StatusCode)
	}

	var prepareResp modelos.PrepareResponse
	if err := json.NewDecoder(resp.Body).Decode(&prepareResp); err != nil {
		return modelos.PrepareResponse{TxID: prepareReq.TxID, VotoSim: false}, fmt.Errorf("resposta invalida: %w", err)
	}

	return prepareResp, nil
}

// EnviarCommit consolida em definitivo a reserva na Fase 2 do 2PC
func (c *ClienteRESTInterEmpresas) EnviarCommit(enderecoBase, txID string) error {
	return c.enviarDecisaoFase2(enderecoBase, txID, "/api/v1/transacoes/commit")
}

// EnviarAbort desfaz a reserva e libera o assento na Fase 2 do 2PC
func (c *ClienteRESTInterEmpresas) EnviarAbort(enderecoBase, txID string) error {
	return c.enviarDecisaoFase2(enderecoBase, txID, "/api/v1/transacoes/abort")
}

// enviarDecisaoFase2 centraliza o disparo de Commit ou Abort
func (c *ClienteRESTInterEmpresas) enviarDecisaoFase2(enderecoBase, txID, endpoint string) error {
	urlAlvo := fmt.Sprintf("%s%s", enderecoBase, endpoint)

	decisao := modelos.DecisaoRequest{
		TxID: txID,
	}

	payloadBytes, err := json.Marshal(decisao)
	if err != nil {
		return fmt.Errorf("erro ao serializar decisao: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), TimeoutPadraoHTTP)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlAlvo, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("erro ao notificar %s em %s: %w", endpoint, enderecoBase, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("empresa respondeu com HTTP %d para a decisao", resp.StatusCode)
	}

	return nil
}

// EnviarCancelamentoTrecho avisa o servidor parceiro para liberar o assento de uma carona confirmada
func (c *ClienteRESTInterEmpresas) EnviarCancelamentoTrecho(enderecoBase string, caronaID int, origem, destino, passageiro string) error {
	urlAlvo := fmt.Sprintf("%s/api/v1/trechos/cancelar", strings.TrimRight(enderecoBase, "/"))

	payload := map[string]any{
		"carona_id":  caronaID,
		"origem":     origem,
		"destino":    destino,
		"passageiro": passageiro,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("erro ao serializar cancelamento: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), TimeoutPadraoHTTP)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlAlvo, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("falha ao conectar em %s: %w", enderecoBase, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return ErrNaoEncontrado
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("servidor respondeu com HTTP %d ao cancelar trecho", resp.StatusCode)
	}

	return nil
}

// ConsultarDecisao pergunta ao coordenador (endereco base) a decisao global de uma transacao.
func (c *ClienteRESTInterEmpresas) ConsultarDecisao(enderecoBase, txID string) (string, error) {
	urlAlvo := fmt.Sprintf("%s/api/v1/transacoes/decisao?tx_id=%s", strings.TrimRight(enderecoBase, "/"), url.QueryEscape(txID))

	ctx, cancel := context.WithTimeout(context.Background(), TimeoutPadraoHTTP)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlAlvo, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("falha ao consultar decisao em %s: %w", enderecoBase, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("coordenador respondeu HTTP %d", resp.StatusCode)
	}
	var d modelos.DecisaoResponse
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return "", fmt.Errorf("resposta invalida do coordenador: %w", err)
	}
	return d.Decisao, nil
}

// ConsultarEstado pergunta a outro participante o estado de uma transação.
func (c *ClienteRESTInterEmpresas) ConsultarEstado(enderecoBase, txID string) (string, error) {
	urlAlvo := fmt.Sprintf("%s/api/v1/transacoes/estado?tx_id=%s", strings.TrimRight(enderecoBase, "/"), url.QueryEscape(txID))

	ctx, cancel := context.WithTimeout(context.Background(), TimeoutPadraoHTTP)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlAlvo, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("falha ao consultar estado em %s: %w", enderecoBase, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("participante respondeu HTTP %d", resp.StatusCode)
	}
	var e modelos.EstadoResponse
	if err := json.NewDecoder(resp.Body).Decode(&e); err != nil {
		return "", fmt.Errorf("resposta invalida do participante: %w", err)
	}
	return e.Estado, nil
}

// NotificarCaronaCancelada avisa uma empresa parceira que uma carona foi cancelada pelo motorista.
func (c *ClienteRESTInterEmpresas) NotificarCaronaCancelada(enderecoBase, empresaOrigem string, caronaID int) error {
	payload, _ := json.Marshal(map[string]any{"empresa": empresaOrigem, "carona_id": caronaID})

	ctx, cancel := context.WithTimeout(context.Background(), TimeoutPadraoHTTP)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/api/v1/caronas/cancelada", strings.TrimRight(enderecoBase, "/")), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("falha ao notificar %s: %w", enderecoBase, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s respondeu HTTP %d", enderecoBase, resp.StatusCode)
	}
	return nil
}
