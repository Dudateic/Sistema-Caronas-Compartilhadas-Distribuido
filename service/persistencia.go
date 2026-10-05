package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var mutexArquivo sync.Mutex

var DiretorioDados string

func init() {
	if env := os.Getenv("DADOS_DIR"); env != "" {
		DiretorioDados = env
	} else {
		DiretorioDados = filepath.Join(os.TempDir(), "vaijunto_dados")
	}
}

func SalvarJSON(nomeArquivo string, dados any) error {
	mutexArquivo.Lock()
	defer mutexArquivo.Unlock()

	caminho := filepath.Join(DiretorioDados, nomeArquivo)

	if err := os.MkdirAll(filepath.Dir(caminho), 0755); err != nil {
		return fmt.Errorf("falha ao criar diretorio de persistencia: %w", err)
	}

	conteudo, err := json.MarshalIndent(dados, "", "  ")
	if err != nil {
		return fmt.Errorf("falha ao serializar dados para JSON: %w", err)
	}

	if err := os.WriteFile(caminho, conteudo, 0644); err != nil {
		return fmt.Errorf("falha ao gravar arquivo %s: %w", caminho, err)
	}

	return nil
}

func CarregarJSON(nomeArquivo string, destino any) error {
	mutexArquivo.Lock()
	defer mutexArquivo.Unlock()

	caminho := filepath.Join(DiretorioDados, nomeArquivo)

	if _, err := os.Stat(caminho); os.IsNotExist(err) {
		return nil
	}

	conteudo, err := os.ReadFile(caminho)
	if err != nil {
		return fmt.Errorf("falha ao ler arquivo %s: %w", caminho, err)
	}

	if len(conteudo) == 0 {
		return nil
	}

	if err := json.Unmarshal(conteudo, destino); err != nil {
		return fmt.Errorf("falha ao desserializar conteudo de %s: %w", caminho, err)
	}

	return nil
}
