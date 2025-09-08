package config

import (
	"context"

	dbm "github.com/cometbft/cometbft-db"

	"github.com/cometbft/cometbft/libs/log"
	"github.com/cometbft/cometbft/libs/service"
)

// ServiceProvider takes a config and a logger and returns a ready to go Node.
type ServiceProvider func(context.Context, *Config, log.Logger) (service.Service, error)

// DBContext specifies config information for loading a new DB.
type DBContext struct {
	ID     string
	Config *Config
}

// DBProvider takes a DBContext and returns an instantiated DB.
type DBProvider func(*DBContext) (dbm.DB, error)

// DefaultDBProvider returns a database using the DBBackend and DBDir
// specified in the Config.
func DefaultDBProvider(ctx *DBContext) (dbm.DB, error) {
	dbType := dbm.BackendType(ctx.Config.DBBackend)

	return dbm.NewDB(ctx.ID, dbType, ctx.Config.DBDir())
}

func DefaultDBProviderWithDBOptions(externalDBOpts map[string]*dbm.NewDatabaseOption) func(ctx *DBContext) (dbm.DB, error) {
	if externalDBOpts == nil {
		return DefaultDBProvider
	}
	return func(ctx *DBContext) (dbm.DB, error) {
		dbType := dbm.BackendType(ctx.Config.DBBackend)
		if dbOpts, ok := externalDBOpts[ctx.ID]; ok {
			return dbm.NewDB(ctx.ID, dbType, ctx.Config.DBDir(), dbOpts)
		}
		return dbm.NewDB(ctx.ID, dbType, ctx.Config.DBDir())
	}
}
