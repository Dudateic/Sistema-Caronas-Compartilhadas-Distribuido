package servicos

import (
	"encoding/json"
	"fmt"
	"os"
)

func (gc *GerenciadorCaronas) salvarEmDisco() error {
	bytesDados, err := json.MarshalIndent(gc.caronas, "", "  ")
	if err != nil {
		return fmt.Errorf("falha ao serializar dados de caronas: %w", err)
	}

	return os.WriteFile(gc.caminhoPersistencia, bytesDados, 0644)
}

func (gc *GerenciadorCaronas) carregarDoDisco() error {
	conteudo, err := os.ReadFile(gc.caminhoPersistencia)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if len(conteudo) == 0 {
		return nil
	}

	if err := json.Unmarshal(conteudo, &gc.caronas); err != nil {
		return err
	}

	for _, c := range gc.caronas {
		if c.ID >= gc.proximoID {
			gc.proximoID = c.ID + 1
		}
	}
	return nil
}

func (gu *GerenciadorUsuarios) salvarEmDisco() error {
	bytesDados, err := json.MarshalIndent(gu.usuarios, "", "  ")
	if err != nil {
		return fmt.Errorf("falha ao serializar usuarios: %w", err)
	}

	return os.WriteFile(gu.caminhoPersistencia, bytesDados, 0644)
}

func (gu *GerenciadorUsuarios) carregarDoDisco() error {
	conteudo, err := os.ReadFile(gu.caminhoPersistencia)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if len(conteudo) == 0 {
		return nil
	}

	return json.Unmarshal(conteudo, &gu.usuarios)
}
