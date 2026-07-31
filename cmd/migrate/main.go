package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"gold-track-be/internal/config"
)

const migrationsPath = "file://migrations"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	cmd := os.Args[1]

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	m, err := migrate.New(migrationsPath, cfg.Database.MigrateDSN())
	if err != nil {
		log.Fatalf("init migrate: %v", err)
	}
	defer m.Close()

	if err := run(m, cmd, os.Args[2:]); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		log.Fatalf("migrate %s: %v", cmd, err)
	}

	fmt.Println("migrate:", cmd, "done")
}

func run(m *migrate.Migrate, cmd string, args []string) error {
	switch cmd {
	case "up":
		return m.Up()
	case "down":
		if len(args) > 0 {
			n, err := strconv.Atoi(args[0])
			if err != nil {
				log.Fatalf("invalid step count: %v", err)
			}
			return m.Steps(-n)
		}
		return m.Down()
	case "steps":
		if len(args) < 1 {
			log.Fatal("usage: migrate steps <n>")
		}
		n, err := strconv.Atoi(args[0])
		if err != nil {
			log.Fatalf("invalid step count: %v", err)
		}
		return m.Steps(n)
	case "version":
		v, dirty, err := m.Version()
		if err != nil {
			return err
		}
		fmt.Printf("version=%d dirty=%v\n", v, dirty)
		return nil
	case "force":
		if len(args) < 1 {
			log.Fatal("usage: migrate force <version>")
		}
		v, err := strconv.Atoi(args[0])
		if err != nil {
			log.Fatalf("invalid version: %v", err)
		}
		return m.Force(v)
	default:
		usage()
		os.Exit(1)
		return nil
	}
}

func usage() {
	fmt.Println("usage: migrate <up|down [n]|steps <n>|version|force <version>>")
}
