package distribuido

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type TipoRegistroLog string

const (
	LogPrepare TipoRegistroLog = "PREPARE"
	LogCommit  TipoRegistroLog = "COMMIT"
	LogAbort   TipoRegistroLog = "ABORT"
)

type EntradaLog struct {
	Timestamp time.Time       `json:"timestamp"`
	TxID      string          `json:"tx_id"`
	Tipo      TipoRegistroLog `json:"tipo"`
	RecursoID string          `json:"recurso_id"` // ex: "carona_1_trecho_0"
	Detalhes  string          `json:"detalhes,omitempty"`
}

type LogTransacoes struct {
	caminho  string
	ficheiro *os.File
	mu       sync.Mutex
}

func NovoLogTransacoes(caminho string) (*LogTransacoes, error) {
	if err := os.MkdirAll(filepath.Dir(caminho), 0755); err != nil {
		return nil, fmt.Errorf("falha ao criar pasta para log de transacoes: %w", err)
	}

	f, err := os.OpenFile(caminho, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("falha ao abrir log de transacoes em %s: %w", caminho, err)
	}

	return &LogTransacoes{
		caminho:  caminho,
		ficheiro: f,
	}, nil
}

func (l *LogTransacoes) Gravar(txID string, tipo TipoRegistroLog, recursoID string, detalhes string) error {
	entrada := EntradaLog{
		Timestamp: time.Now(),
		TxID:      txID,
		Tipo:      tipo,
		RecursoID: recursoID,
		Detalhes:  detalhes,
	}

	dados, err := json.Marshal(entrada)
	if err != nil {
		return fmt.Errorf("erro ao serializar entrada do log: %w", err)
	}
	dados = append(dados, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()

	if _, err := l.ficheiro.Write(dados); err != nil {
		return fmt.Errorf("erro de I/O ao gravar no log: %w", err)
	}

	if err := l.ficheiro.Sync(); err != nil {
		return fmt.Errorf("falha ao sincronizar log com o disco (fsync): %w", err)
	}

	return nil
}

func (l *LogTransacoes) RecuperarEstado() (map[string]TipoRegistroLog, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	fLeitura, err := os.Open(l.caminho)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]TipoRegistroLog), nil
		}
		return nil, fmt.Errorf("falha ao abrir log para leitura de recuperacao: %w", err)
	}
	defer fLeitura.Close()

	estadoTransacoes := make(map[string]TipoRegistroLog)
	scanner := bufio.NewScanner(fLeitura)

	for scanner.Scan() {
		linha := scanner.Bytes()
		if len(linha) == 0 {
			continue
		}

		var entrada EntradaLog
		if err := json.Unmarshal(linha, &entrada); err != nil {
			continue
		}

		estadoTransacoes[entrada.TxID] = entrada.Tipo
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("erro durante a leitura sequencial do log: %w", err)
	}

	return estadoTransacoes, nil
}

func (l *LogTransacoes) Fechar() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.ficheiro != nil {
		_ = l.ficheiro.Sync()
		return l.ficheiro.Close()
	}
	return nil
}
