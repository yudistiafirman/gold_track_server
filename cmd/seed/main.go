package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"gold-track-be/internal/config"
	"gold-track-be/internal/database"
)

// defaultExpenseCategories are seeded once; users can add more from the app.
var defaultExpenseCategories = []string{
	"Listrik",
	"Wifi/Internet",
	"ATK",
	"Gaji Karyawan",
	"Sewa Tempat",
	"Transportasi",
	"Lain-lain",
}

// defaultCategories are seeded once; users can add more from the app.
var defaultCategories = []string{
	"Batangan",
	"Koin",
	"Perhiasan",
	"Galeri",
}

// defaultBrands are seeded once; users can add more from the app.
var defaultBrands = []string{
	"Antam",
	"UBS",
	"Galeri24",
	"Lotus Archi",
	"King Halim",
}

type settingSeed struct {
	Key         string
	Value       string
	Description string
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx := context.Background()
	pool, err := database.NewPostgresPool(ctx, cfg.Database)
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		log.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx)

	adminID, err := seedAdminUser(ctx, tx)
	if err != nil {
		log.Fatalf("seed admin user: %v", err)
	}
	fmt.Println("seeded admin user, id =", adminID)

	if err := seedExpenseCategories(ctx, tx); err != nil {
		log.Fatalf("seed expense categories: %v", err)
	}
	fmt.Println("seeded expense categories")

	if err := seedCategories(ctx, tx); err != nil {
		log.Fatalf("seed categories: %v", err)
	}
	fmt.Println("seeded categories")

	if err := seedBrands(ctx, tx); err != nil {
		log.Fatalf("seed brands: %v", err)
	}
	fmt.Println("seeded brands")

	if err := seedShopSettings(ctx, tx, adminID); err != nil {
		log.Fatalf("seed shop settings: %v", err)
	}
	fmt.Println("seeded shop settings")

	if err := tx.Commit(ctx); err != nil {
		log.Fatalf("commit tx: %v", err)
	}

	fmt.Println("seed: done")
}

// seedAdminUser creates the initial SUPER_ADMIN account if it doesn't exist yet.
// The ON CONFLICT branch is a no-op update so RETURNING id still works on repeat
// runs, without touching the password hash of an already-provisioned admin.
func seedAdminUser(ctx context.Context, tx pgx.Tx) (int64, error) {
	name := getEnv("SEED_ADMIN_NAME", "Super Admin")
	email := getEnv("SEED_ADMIN_EMAIL", "admin@goldtrack.local")
	password := getEnv("SEED_ADMIN_PASSWORD", "")
	if password == "" {
		password = "ChangeMe123!"
		log.Println("WARNING: SEED_ADMIN_PASSWORD not set, using default password — change it after first login")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return 0, fmt.Errorf("hash password: %w", err)
	}

	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO users (name, email, password_hash, role, is_active)
		VALUES ($1, $2, $3, 'SUPER_ADMIN', true)
		ON CONFLICT (email) DO UPDATE SET email = EXCLUDED.email
		RETURNING id
	`, name, email, string(hash)).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func seedExpenseCategories(ctx context.Context, tx pgx.Tx) error {
	for _, name := range defaultExpenseCategories {
		_, err := tx.Exec(ctx, `
			INSERT INTO expense_categories (name) VALUES ($1)
			ON CONFLICT (name) DO NOTHING
		`, name)
		if err != nil {
			return fmt.Errorf("insert category %q: %w", name, err)
		}
	}
	return nil
}

func seedCategories(ctx context.Context, tx pgx.Tx) error {
	for _, name := range defaultCategories {
		_, err := tx.Exec(ctx, `
			INSERT INTO categories (name) VALUES ($1)
			ON CONFLICT (lower(name)) DO NOTHING
		`, name)
		if err != nil {
			return fmt.Errorf("insert category %q: %w", name, err)
		}
	}
	return nil
}

func seedBrands(ctx context.Context, tx pgx.Tx) error {
	for _, name := range defaultBrands {
		_, err := tx.Exec(ctx, `
			INSERT INTO brands (name) VALUES ($1)
			ON CONFLICT (lower(name)) DO NOTHING
		`, name)
		if err != nil {
			return fmt.Errorf("insert brand %q: %w", name, err)
		}
	}
	return nil
}

// seedShopSettings only inserts missing keys, so it never overwrites values
// an admin has already configured through the app.
func seedShopSettings(ctx context.Context, tx pgx.Tx, adminID int64) error {
	settings := []settingSeed{
		{"shop_name", getEnv("SEED_SHOP_NAME", "Gold Track Store"), "Nama toko untuk struk & laporan"},
		{"shop_address", getEnv("SEED_SHOP_ADDRESS", "Jl. Contoh No. 1, Jakarta"), "Alamat toko untuk struk"},
		{"shop_phone", getEnv("SEED_SHOP_PHONE", "0800-0000-0000"), "Nomor telepon toko untuk struk"},
	}

	for _, s := range settings {
		_, err := tx.Exec(ctx, `
			INSERT INTO settings (key, value, description, updated_by)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (key) DO NOTHING
		`, s.Key, s.Value, s.Description, adminID)
		if err != nil {
			return fmt.Errorf("insert setting %q: %w", s.Key, err)
		}
	}
	return nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
