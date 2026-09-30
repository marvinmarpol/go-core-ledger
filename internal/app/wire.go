//go:build wireinject

package app

import (
	"github.com/go-chi/chi/v5"
	"github.com/google/wire"
	"github.com/jackc/pgx/v5/pgxpool"
)

func InitializeApp(r *chi.Mux, pool *pgxpool.Pool) error {
	panic(wire.Build(wire.Value(error(nil))))
}
