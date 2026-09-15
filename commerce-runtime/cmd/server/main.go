package main

import (
	"context"
	"log"

	"github.com/stablepay/commerce-runtime/config"
	mysqlrepo "github.com/stablepay/commerce-runtime/internal/infrastructure/mysql"
)

func main() {
	cfg := config.FromEnv()
	if cfg.MySQLDSN == "" {
		log.Println("commerce-runtime S1 domain module ready; set COMMERCE_RUNTIME_MYSQL_DSN to run MySQL migrations")
		return
	}
	db, err := mysqlrepo.Open(cfg.MySQLDSN)
	if err != nil {
		log.Fatal(err)
	}
	if err := mysqlrepo.AutoMigrate(context.Background(), db); err != nil {
		log.Fatal(err)
	}
	log.Printf("commerce-runtime persistence ready (%s)", cfg.RuntimeVersion)
}
