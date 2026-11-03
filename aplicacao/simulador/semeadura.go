package simulador

import (
	"fmt"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/cliente_servidor"
	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/modelos"
)

func SemearCatalogoCaronas(alvos []string, timeout time.Duration, assentosPorTrecho int) {
	rotasIniciais := [][]string{
		{"SSA", "FSA", "VIT"},
		{"VIT", "BRU", "CLE"},
		{"SSA", "VIT", "CLE"},
		{"FSA", "VIT", "CLE"},
		{"SSA", "CLE"},
	}

	for i, alvo := range alvos {
		cli := NovoClienteTCP(alvo, timeout)
		defer cli.Fechar()

		motorista := fmt.Sprintf("motorista_seed_%d", i+1)

		_ = cli.Executar(map[string]any{
			"tipo":         cliente_servidor.TipoCadastroReq,
			"nome":         motorista,
			"login":        motorista,
			"senha":        "123456",
			"tipo_usuario": string(modelos.UsuarioMotorista),
		}, cliente_servidor.TipoCadastroRes, &cliente_servidor.RespostaSimples{})

		for _, rota := range rotasIniciais {
			pubReq := map[string]any{
				"tipo":             cliente_servidor.TipoPublicarCaronaReq,
				"motorista":        motorista,
				"rota":             rota,
				"data":             "2026-10-30",
				"horario":          "12:00",
				"assentos_totais":  assentosPorTrecho,
				"preco_por_trecho": 15.0,
			}
			var pubRes cliente_servidor.RespostaPublicarCarona
			_ = cli.Executar(pubReq, cliente_servidor.TipoPublicarCaronaRes, &pubRes)
		}
	}
}
