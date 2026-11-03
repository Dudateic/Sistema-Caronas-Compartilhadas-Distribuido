package simulador

import (
	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/cliente_servidor"
	"fmt"
	"time"
)

func AuditarInvariantes(alvos []string, timeout time.Duration) {
	fmt.Println("[AUDITORIA] Inspecionando integridade das caronas no consórcio...")

	totalOverbooking := 0
	totalVagasLivres := 0
	totalPassageiros := 0

	for _, alvo := range alvos {
		cli := NovoClienteTCP(alvo, timeout)
		defer cli.Fechar()

		var resp cliente_servidor.RespostaConsultarCaronas
		err := cli.Executar(map[string]any{
			"tipo": cliente_servidor.TipoConsultarCaronasReq,
		}, cliente_servidor.TipoConsultarCaronasRes, &resp)

		if err != nil {
			fmt.Printf("  [!] Falha ao auditar nó %s: %v", alvo, err)
			continue
		}

		for _, c := range resp.Caronas {
			for _, t := range c.Trechos {
				nPass := len(t.Passageiros)
				totalPassageiros += nPass
				totalVagasLivres += t.AssentosLivres

				if nPass+t.AssentosLivres > c.AssentosTotais {
					fmt.Printf("  [VIOLAÇÃO] Overbooking detectado na Carona #%d (%s->%s): %d passageiros + %d livres > %d vagas!",
						c.ID, t.Origem, t.Destino, nPass, t.AssentosLivres, c.AssentosTotais)
					totalOverbooking++
				}
			}
		}
	}

	if totalOverbooking == 0 {
		fmt.Println("  [OK] Invariante preservada: ZERO OVERBOOKING detectado em todas as empresas.")
		fmt.Printf("  [OK] Assentos restantes: %d | Passageiros alocados: %d", totalVagasLivres, totalPassageiros)
	} else {
		fmt.Printf("  [FALHA] Detectados %d trechos inconsistentes no consórcio!", totalOverbooking)
	}
}
