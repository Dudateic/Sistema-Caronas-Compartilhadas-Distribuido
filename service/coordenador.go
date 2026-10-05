package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/comunicacao/protocolo"
)

var (
	maxTentativasPrepare = 8
	esperaPrepare        = 50 * time.Millisecond
	tentativasDecisao    = 6
	intervaloDecisao     = 200 * time.Millisecond

	retriesDecisao sync.WaitGroup
)

type votoTrecho struct {
	voto   bool
	preco  float64
	motivo string
}

func clienteHTTP() *http.Client {
	return &http.Client{Timeout: Config.TimeoutPar}
}

func enviarPrepare(empresa string, pedido protocolo.PedidoBloqueio) (protocolo.RespostaPrepare, error) {
	if ehLocal(empresa) {
		_, resp := Preparar(pedido)
		return resp, nil
	}
	base, ok := Config.Pares[empresa]
	if !ok {
		return protocolo.RespostaPrepare{}, fmt.Errorf("empresa %s desconhecida", empresa)
	}
	corpo, _ := json.Marshal(pedido)
	resp, err := clienteHTTP().Post(base+protocolo.RotaPrepare, "application/json", bytes.NewReader(corpo))
	if err != nil {
		return protocolo.RespostaPrepare{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict {
		return protocolo.RespostaPrepare{}, fmt.Errorf("resposta HTTP %d", resp.StatusCode)
	}
	var voto protocolo.RespostaPrepare
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&voto); err != nil {
		return protocolo.RespostaPrepare{}, fmt.Errorf("resposta invalida: %w", err)
	}
	voto.Voto = voto.Voto && resp.StatusCode == http.StatusOK // 409 nunca e SIM
	return voto, nil
}

func solicitarVoto(txID string, ts int64, req protocolo.SolicitacaoReserva, t protocolo.TrechoViagem) votoTrecho {
	pedido := protocolo.PedidoBloqueio{
		TxID: txID, CaronaID: t.CaronaID, Origem: t.Origem, Destino: t.Destino,
		Passageiro: req.Passageiro, Data: req.Data, Timestamp: ts,
	}
	for tentativa := 0; ; tentativa++ {
		resp, err := enviarPrepare(t.Empresa, pedido)
		if err != nil {
			return votoTrecho{motivo: fmt.Sprintf("empresa %s indisponivel (%v)", t.Empresa, err)}
		}
		Relogio.Atualizar(resp.Relogio)
		if resp.Voto {
			return votoTrecho{voto: true, preco: resp.Preco}
		}
		maisAntiga := precede(ts, txID, resp.HolderTimestamp, resp.HolderTxID)
		if resp.Bloqueada && maisAntiga && tentativa < maxTentativasPrepare {
			time.Sleep(esperaPrepare * time.Duration(tentativa+1))
			continue
		}
		motivo := resp.Motivo
		if motivo == "" {
			motivo = "voto NAO"
		}
		return votoTrecho{motivo: fmt.Sprintf("%s (empresa %s, carona %d)", motivo, t.Empresa, t.CaronaID)}
	}
}

func enviarDecisao(empresa, txID, decisao, motivo string) (error, bool) {
	if ehLocal(empresa) {
		var status int
		var resp protocolo.RespostaTransacao
		if decisao == "commit" {
			status, resp = Commitar(txID)
		} else {
			status, resp = Abortar(txID, motivo)
		}
		if status != http.StatusOK {
			return fmt.Errorf("%s local recusado (%d): %s", decisao, status, resp.Mensagem), true
		}
		return nil, false
	}

	base, ok := Config.Pares[empresa]
	if !ok {
		return fmt.Errorf("empresa %s desconhecida", empresa), true
	}
	var corpo []byte
	var rota string
	if decisao == "commit" {
		corpo, _ = json.Marshal(protocolo.PedidoCommit{TxID: txID})
		rota = fmt.Sprintf(protocolo.RotaCommit, url.PathEscape(txID))
	} else {
		corpo, _ = json.Marshal(protocolo.PedidoRollback{TxID: txID, Motivo: motivo})
		rota = fmt.Sprintf(protocolo.RotaRollback, url.PathEscape(txID))
	}
	resp, err := clienteHTTP().Post(base+rota, "application/json", bytes.NewReader(corpo))
	if err != nil {
		return err, false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusOK:
		return nil, false
	case resp.StatusCode >= 500:
		return fmt.Errorf("HTTP %d", resp.StatusCode), false // transitorio
	default:
		return fmt.Errorf("%s recusado pela empresa %s (HTTP %d)", decisao, empresa, resp.StatusCode), true
	}
}

func distribuirDecisao(txID string, empresas []string, decisao, motivo string) {
	var mu sync.Mutex
	var falhas []string
	var wg sync.WaitGroup
	for _, emp := range empresas {
		wg.Add(1)
		go func(emp string) {
			defer wg.Done()
			err, permanente := enviarDecisao(emp, txID, decisao, motivo)
			if err == nil {
				return
			}
			fmt.Printf("[2PC] %s da transacao %s para %s falhou: %v\n", decisao, txID, emp, err)
			if !permanente {
				mu.Lock()
				falhas = append(falhas, emp)
				mu.Unlock()
			}
		}(emp)
	}
	wg.Wait()

	if len(falhas) > 0 {
		retriesDecisao.Add(1)
		go reenviarDecisao(txID, falhas, decisao, motivo)
	}
}

func reenviarDecisao(txID string, empresas []string, decisao, motivo string) {
	defer retriesDecisao.Done()
	restantes := empresas
	for tentativa := 1; tentativa <= tentativasDecisao && len(restantes) > 0; tentativa++ {
		time.Sleep(intervaloDecisao * time.Duration(tentativa))
		var ainda []string
		for _, emp := range restantes {
			if err, permanente := enviarDecisao(emp, txID, decisao, motivo); err != nil && !permanente {
				ainda = append(ainda, emp)
			}
		}
		restantes = ainda
	}
	if len(restantes) > 0 {
		fmt.Printf("[2PC][ALERTA] %s da transacao %s nao entregue a %v; os bloqueios expirarao por timeout\n", decisao, txID, restantes)
	}
}

func CoordenarReserva(req protocolo.SolicitacaoReserva) (int, string, bool) {
	inicio := time.Now()

	trechos := make([]protocolo.TrechoViagem, len(req.Trechos))
	copy(trechos, req.Trechos)

	var empresas []string
	vistasEmp := map[string]bool{}
	vistosTrecho := map[string]bool{}
	for i := range trechos {
		t := &trechos[i]
		if t.Empresa == "" {
			t.Empresa = Config.Empresa
		}
		if !ehLocal(t.Empresa) {
			if _, ok := Config.Pares[t.Empresa]; !ok {
				return 0, fmt.Sprintf("empresa desconhecida no consorcio: %s", t.Empresa), false
			}
		}
		chave := fmt.Sprintf("%s|%d|%s|%s", t.Empresa, t.CaronaID, t.Origem, t.Destino)
		if vistosTrecho[chave] {
			return 0, "o itinerario repete o mesmo trecho", false
		}
		vistosTrecho[chave] = true
		if !vistasEmp[t.Empresa] {
			vistasEmp[t.Empresa] = true
			empresas = append(empresas, t.Empresa)
		}
	}

	txID := fmt.Sprintf("%s-%s", Config.Empresa, gerarID())
	ts := Relogio.Tick()
	fmt.Printf("[2PC] Iniciando transacao %s (ts=%d, %d trecho(s), %d empresa(s))\n", txID, ts, len(trechos), len(empresas))

	votos := make([]votoTrecho, len(trechos))
	var wg sync.WaitGroup
	for i := range trechos {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			votos[i] = solicitarVoto(txID, ts, req, trechos[i])
		}(i)
	}
	wg.Wait()

	motivo := ""
	for _, v := range votos {
		if !v.voto {
			motivo = v.motivo
			break
		}
	}
	if motivo == "" && time.Since(inicio) > Config.TTLBloqueio/2 {
		motivo = "tempo excedido na fase de preparacao"
	}

	if motivo != "" {
		distribuirDecisao(txID, empresas, "rollback", motivo)
		fmt.Printf("[2PC] Transacao %s abortada: %s\n", txID, motivo)
		return 0, motivo, false
	}

	var total float64
	for i := range trechos {
		trechos[i].Preco = votos[i].preco
		total += votos[i].preco
	}

	MutexReservas.Lock()
	reservaID := ProximoReservaID
	ProximoReservaID++
	ReservasRegistradas = append(ReservasRegistradas, protocolo.Reserva{
		ID:         reservaID,
		TxID:       txID,
		Passageiro: req.Passageiro,
		Data:       req.Data,
		PrecoTotal: total,
		Trechos:    trechos,
	})
	if err := SalvarJSON(ArquivoReservas, ReservasRegistradas); err != nil {
		fmt.Printf("[ERRO] Falha ao persistir reservas: %v\n", err)
	}
	MutexReservas.Unlock()

	distribuirDecisao(txID, empresas, "commit", "")
	fmt.Printf("[RESERVA] Reserva #%d confirmada para '%s' (R$ %.2f) tx=%s\n", reservaID, req.Passageiro, total, txID)
	return reservaID, "Reserva confirmada com sucesso!", true
}
