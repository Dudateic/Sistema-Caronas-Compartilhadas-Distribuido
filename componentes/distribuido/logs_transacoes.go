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
	LogInicio  TipoRegistroLog = "INICIO"  // coordenador: transação iniciada (lista de participantes)
	LogPrepare TipoRegistroLog = "PREPARE" // participante: voto SIM gravado
	LogCommit  TipoRegistroLog = "COMMIT"  // decisão global de confirmar
	LogAbort   TipoRegistroLog = "ABORT"   // decisão global de abortar
	LogFim     TipoRegistroLog = "FIM"     // decisão já aplicada por completo
)

type EntradaLog struct {
	Timestamp time.Time       `json:"timestamp"`
	TxID      string          `json:"tx_id"`
	Tipo      TipoRegistroLog `json:"tipo"`
	RecursoID string          `json:"recurso_id"`
	Detalhes  string          `json:"detalhes,omitempty"`
}

// LogTransacoes é um write-ahead log append-only com fsync a cada registro.
// Cada linha é um JSON. Um registro só é considerado gravado depois do fsync.
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

	return &LogTransacoes{caminho: caminho, ficheiro: f}, nil
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

// Entradas lê todo o log, em ordem de gravação. Linhas corrompidas (por exemplo,
// uma escrita interrompida pela queda) são ignoradas.
func (l *LogTransacoes) Entradas() ([]EntradaLog, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	fLeitura, err := os.Open(l.caminho)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("falha ao abrir log para leitura de recuperacao: %w", err)
	}
	defer fLeitura.Close()

	var entradas []EntradaLog
	scanner := bufio.NewScanner(fLeitura)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		linha := scanner.Bytes()
		if len(linha) == 0 {
			continue
		}
		var e EntradaLog
		if err := json.Unmarshal(linha, &e); err != nil {
			continue
		}
		entradas = append(entradas, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("erro durante a leitura sequencial do log: %w", err)
	}
	return entradas, nil
}

// RecuperarEstado devolve o último tipo de registro de cada transação.
func (l *LogTransacoes) RecuperarEstado() (map[string]TipoRegistroLog, error) {
	entradas, err := l.Entradas()
	if err != nil {
		return nil, err
	}
	estado := make(map[string]TipoRegistroLog)
	for _, e := range entradas {
		estado[e.TxID] = e.Tipo
	}
	return estado, nil
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
