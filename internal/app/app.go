// Package app wires the full application (db pool, repositories, services,
// handlers, router) from config. cmd/api and the e2e test harness both call
// New so they can never drift apart — a repository/service/handler added
// here is automatically wired for both real runs and tests.
package app

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"gold-track-be/internal/config"
	"gold-track-be/internal/database"
	"gold-track-be/internal/handler"
	"gold-track-be/internal/repository"
	"gold-track-be/internal/service"
)

type App struct {
	Pool   *pgxpool.Pool
	Router http.Handler
}

func New(ctx context.Context, cfg *config.Config, log *slog.Logger) (*App, error) {
	dbPool, err := database.NewPostgresPool(ctx, cfg.Database)
	if err != nil {
		return nil, err
	}

	healthRepo := repository.NewHealthRepository(dbPool)
	healthService := service.NewHealthService(healthRepo)
	healthHandler := handler.NewHealthHandler(healthService)

	userRepo := repository.NewUserRepository(dbPool)
	tokenBlacklistRepo := repository.NewTokenBlacklistRepository(dbPool)
	authService := service.NewAuthService(userRepo, tokenBlacklistRepo, cfg.JWT.Secret, cfg.JWT.Expiry)
	authHandler := handler.NewAuthHandler(authService)

	userService := service.NewUserService(userRepo)
	userHandler := handler.NewUserHandler(userService)

	categoryRepo := repository.NewCategoryRepository(dbPool)
	categoryService := service.NewCategoryService(categoryRepo)
	categoryHandler := handler.NewCategoryHandler(categoryService)

	brandRepo := repository.NewBrandRepository(dbPool)
	brandService := service.NewBrandService(brandRepo)
	brandHandler := handler.NewBrandHandler(brandService)

	productRepo := repository.NewProductRepository(dbPool)
	productService := service.NewProductService(productRepo, categoryRepo, brandRepo, userRepo)
	productHandler := handler.NewProductHandler(productService)

	supplierRepo := repository.NewSupplierRepository(dbPool)
	supplierService := service.NewSupplierService(supplierRepo)
	supplierHandler := handler.NewSupplierHandler(supplierService)

	stockItemRepo := repository.NewStockItemRepository(dbPool)
	stockItemService := service.NewStockItemService(stockItemRepo, productRepo, userRepo)
	stockItemHandler := handler.NewStockItemHandler(stockItemService)

	customerRepo := repository.NewCustomerRepository(dbPool)
	customerService := service.NewCustomerService(customerRepo)
	customerHandler := handler.NewCustomerHandler(customerService)

	settingsRepo := repository.NewSettingsRepository(dbPool)

	transactionRepo := repository.NewTransactionRepository(dbPool)
	transactionService := service.NewTransactionService(transactionRepo, stockItemRepo, productRepo, customerRepo, supplierRepo, userRepo, settingsRepo)
	transactionHandler := handler.NewTransactionHandler(transactionService)

	purchaseOrderRepo := repository.NewPurchaseOrderRepository(dbPool)
	purchaseOrderService := service.NewPurchaseOrderService(purchaseOrderRepo, productRepo, supplierRepo, userRepo)
	purchaseOrderHandler := handler.NewPurchaseOrderHandler(purchaseOrderService)

	stockOpnameRepo := repository.NewStockOpnameRepository(dbPool)
	stockOpnameService := service.NewStockOpnameService(stockOpnameRepo, userRepo)
	stockOpnameHandler := handler.NewStockOpnameHandler(stockOpnameService)

	expenseCategoryRepo := repository.NewExpenseCategoryRepository(dbPool)
	expenseCategoryService := service.NewExpenseCategoryService(expenseCategoryRepo)
	expenseCategoryHandler := handler.NewExpenseCategoryHandler(expenseCategoryService)

	expenseRepo := repository.NewExpenseRepository(dbPool)
	expenseService := service.NewExpenseService(expenseRepo, expenseCategoryRepo, userRepo)
	expenseHandler := handler.NewExpenseHandler(expenseService)

	reportRepo := repository.NewReportRepository(dbPool)
	reportService := service.NewReportService(reportRepo)
	reportHandler := handler.NewReportHandler(reportService)

	router := handler.NewRouter(log, healthHandler, authHandler, authService, userHandler, categoryHandler, brandHandler, productHandler, supplierHandler, stockItemHandler, customerHandler, transactionHandler, purchaseOrderHandler, stockOpnameHandler, expenseCategoryHandler, expenseHandler, reportHandler)

	return &App{Pool: dbPool, Router: router}, nil
}
