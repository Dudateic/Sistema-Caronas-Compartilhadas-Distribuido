package servicos

import (
	"testing"

	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/modelos"
)

func tv(emp string, id int, o, d string, preco float64) modelos.TrechoViagem {
	return modelos.TrechoViagem{Empresa: emp, CaronaID: id, Origem: o, Destino: d, Data: "2026-11-10", Preco: preco}
}

func TestBuscaNaoRepeteItinerarios(t *testing.T) {
	trechos := []modelos.TrechoViagem{
		tv("A", 1, "X", "Y", 10),
		tv("A", 1, "X", "Y", 10), // duplicado (local + parceiro)
		tv("B", 2, "Y", "Z", 20),
		tv("B", 2, "Y", "Z", 20), // duplicado
	}
	got := MontarItinerarios(trechos, "x", "z", "")
	if len(got) != 1 {
		t.Fatalf("esperava 1 itinerario, veio %d", len(got))
	}
	if got[0].PrecoTotal != 30 || len(got[0].Trechos) != 2 {
		t.Fatalf("itinerario inesperado: %+v", got[0])
	}
}

func TestBuscaMantemCaronasDistintas(t *testing.T) {
	trechos := []modelos.TrechoViagem{
		tv("A", 1, "X", "Y", 10),
		tv("A", 3, "X", "Y", 12), // outra carona: itinerario legitimamente diferente
		tv("B", 2, "Y", "Z", 20),
	}
	if got := MontarItinerarios(trechos, "X", "Z", ""); len(got) != 2 {
		t.Fatalf("esperava 2 itinerarios, veio %d", len(got))
	}
}

func TestBuscaNaoFazCiclos(t *testing.T) {
	trechos := []modelos.TrechoViagem{
		tv("A", 1, "X", "Y", 1),
		tv("A", 2, "Y", "X", 1),
		tv("B", 3, "Y", "Z", 1),
	}
	got := MontarItinerarios(trechos, "X", "Z", "")
	if len(got) != 1 || len(got[0].Trechos) != 2 {
		t.Fatalf("itinerarios inesperados: %+v", got)
	}
}
