package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var mutexArquivo sync.Mutex

// DiretorioDados e a pasta de persistencia da empresa (configuravel no main).
// Cada empresa deve usar a sua propria pasta: nao ha armazenamento compartilhado.
var DiretorioDados = "dados"

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
