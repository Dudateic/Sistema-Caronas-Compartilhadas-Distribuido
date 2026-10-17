package rest

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type ServidorREST struct {
	httpServer *http.Server
}

func NovoServidorREST(porta int, handlers *HandlersREST) *ServidorREST {
	mux := http.NewServeMux()
	handlers.RegistrarRotas(mux)

	return &ServidorREST{
		httpServer: &http.Server{
			Addr:         fmt.Sprintf(":%d", porta),
			Handler:      mux,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
		},
	}
}

func (s *ServidorREST) Iniciar() error {
	return s.httpServer.ListenAndServe()
}

func (s *ServidorREST) Parar() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return s.httpServer.Shutdown(ctx)
}
