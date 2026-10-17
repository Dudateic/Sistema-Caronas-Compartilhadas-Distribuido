package servicos

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// NivelLog define a severidade da mensagem
type NivelLog string

const (
	Info    NivelLog = "INFO"
	Aviso   NivelLog = "AVISO"
	Erro    NivelLog = "ERRO"
	two_PC  NivelLog = "2PC"
	Lamport NivelLog = "LAMPORT"
)

// RegistadorEventos gere o log estruturado do no
type RegistadorEventos struct {
	idEmpresa string
	ficheiro  *os.File
	logger    *log.Logger
	mu        sync.Mutex
}

// NovoRegistadorEventos cria o registador gravando em stdout e no ficheiro local da empresa
func NovoRegistadorEventos(idEmpresa, diretorioDados string) (*RegistadorEventos, error) {
	if err := os.MkdirAll(diretorioDados, 0755); err != nil {
		return nil, fmt.Errorf("falha ao criar pasta de logs: %w", err)
	}

	caminhoLog := filepath.Join(diretorioDados, "servidor.log")
	f, err := os.OpenFile(caminhoLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("falha ao abrir servidor.log: %w", err)
	}

	// Escrita duplicada: console + arquivo
	multiplo := io.MultiWriter(os.Stdout, f)
	logger := log.New(multiplo, "", 0)

	return &RegistadorEventos{
		idEmpresa: idEmpresa,
		ficheiro:  f,
		logger:    logger,
	}, nil
}

// Registar grava o evento com data, empresa e nivel de severidade
func (r *RegistadorEventos) Registar(nivel NivelLog, formato string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	mensagem := fmt.Sprintf(formato, args...)
	linha := fmt.Sprintf("[%s] [%s] [%s] %s", timestamp, r.idEmpresa, nivel, mensagem)

	r.logger.Println(linha)
}

// Fechar encerra o ficheiro de log
func (r *RegistadorEventos) Fechar() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.ficheiro != nil {
		return r.ficheiro.Close()
	}
	return nil
}
