package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"gold-track-be/internal/model"
	"gold-track-be/internal/repository"
	"gold-track-be/pkg/apperror"
)

const (
	defaultPOPage  = 1
	defaultPOLimit = 20
	maxPOLimit     = 100
)

var allowedPOStatuses = map[string]struct{}{
	"BELUM_DITERIMA": {},
	"DITERIMA":       {},
	"DIBATALKAN":     {},
}

type PurchaseOrderItemSummary struct {
	PublicID      string
	Product       ProductRef
	Quantity      int
	PurchasePrice float64
}

// ReceivedUnitSummary is one newly created stock item from a receive call —
// the admin who just received a PO needs these barcodes on hand to print
// labels immediately, same reasoning BE-702/BE-801 had for putting
// stock_item_id/barcode on each transaction item.
type ReceivedUnitSummary struct {
	StockItemPublicID string
	Barcode           string
	ProductName       string
	SerialNumber      string
	Condition         string
	ProductionYear    *int // optional
}

// PurchaseOrderSummary is the public-facing view of a PO: only PublicID
// (UUID) ever leaves this layer. Items is empty for list rows (header
// only); ReceivedUnits is only ever populated by Receive's response.
type PurchaseOrderSummary struct {
	PublicID      string
	POCode        string
	Supplier      ProductRef
	TotalAmount   float64
	Status        string
	Notes         string
	Items         []PurchaseOrderItemSummary
	ReceivedUnits []ReceivedUnitSummary
	CreatedAt     time.Time
	ReceivedAt    *time.Time
}

type CreatePOItemInput struct {
	ProductPublicID string
	Quantity        int
	PurchasePrice   float64
}

type CreatePOInput struct {
	SupplierPublicID  string
	Notes             string
	Items             []CreatePOItemInput
	CreatedByPublicID string
}

type ListPOInput struct {
	Status string
	Page   int
	Limit  int
}

type PurchaseOrderListResult struct {
	Items      []PurchaseOrderSummary
	Page       int
	Limit      int
	Total      int
	TotalPages int
}

// ReceivePOSerialInput is one physical unit arriving — condition is
// captured per serial since a single shipment isn't guaranteed to be
// uniformly GOOD or BAD.
type ReceivePOSerialInput struct {
	SerialNumber   string
	Condition      string
	ProductionYear *int // optional
}

type ReceivePOItemInput struct {
	ProductPublicID string
	Serials         []ReceivePOSerialInput
}

type ReceivePOInput struct {
	PublicID          string
	Items             []ReceivePOItemInput
	CreatedByPublicID string
}

type PurchaseOrderService interface {
	Create(ctx context.Context, input CreatePOInput) (PurchaseOrderSummary, error)
	List(ctx context.Context, input ListPOInput) (PurchaseOrderListResult, error)
	Get(ctx context.Context, publicID string) (PurchaseOrderSummary, error)
	Receive(ctx context.Context, input ReceivePOInput) (PurchaseOrderSummary, error)
	Cancel(ctx context.Context, publicID string) error
}

type purchaseOrderService struct {
	purchaseOrderRepo repository.PurchaseOrderRepository
	productRepo       repository.ProductRepository
	supplierRepo      repository.SupplierRepository
	userRepo          repository.UserRepository
}

func NewPurchaseOrderService(
	purchaseOrderRepo repository.PurchaseOrderRepository,
	productRepo repository.ProductRepository,
	supplierRepo repository.SupplierRepository,
	userRepo repository.UserRepository,
) PurchaseOrderService {
	return &purchaseOrderService{
		purchaseOrderRepo: purchaseOrderRepo,
		productRepo:       productRepo,
		supplierRepo:      supplierRepo,
		userRepo:          userRepo,
	}
}

func (s *purchaseOrderService) Create(ctx context.Context, input CreatePOInput) (PurchaseOrderSummary, error) {
	if input.SupplierPublicID == "" {
		return PurchaseOrderSummary{}, apperror.BadRequest("supplier_id wajib diisi", nil)
	}
	if len(input.Items) == 0 {
		return PurchaseOrderSummary{}, apperror.BadRequest("items wajib diisi minimal 1 produk", nil)
	}
	for _, item := range input.Items {
		if item.ProductPublicID == "" {
			return PurchaseOrderSummary{}, apperror.BadRequest("product_id wajib diisi di setiap item", nil)
		}
		if item.Quantity <= 0 {
			return PurchaseOrderSummary{}, apperror.BadRequest("quantity setiap item harus lebih besar dari 0", nil)
		}
		if item.PurchasePrice <= 0 {
			return PurchaseOrderSummary{}, apperror.BadRequest("purchase_price setiap item harus lebih besar dari 0", nil)
		}
	}

	supplier, err := s.supplierRepo.FindByPublicID(ctx, input.SupplierPublicID)
	if err != nil {
		if errors.Is(err, repository.ErrSupplierNotFound) {
			return PurchaseOrderSummary{}, apperror.NotFound("supplier tidak ditemukan", nil)
		}
		return PurchaseOrderSummary{}, apperror.Internal("failed to fetch supplier", err)
	}

	creator, err := s.userRepo.FindByPublicID(ctx, input.CreatedByPublicID)
	if err != nil {
		return PurchaseOrderSummary{}, apperror.Internal("failed to resolve acting user", err)
	}

	repoItems := make([]repository.CreatePOItemInput, 0, len(input.Items))
	productRefs := make([]ProductRef, 0, len(input.Items))
	for _, item := range input.Items {
		product, err := s.productRepo.FindByPublicID(ctx, item.ProductPublicID)
		if err != nil {
			if errors.Is(err, repository.ErrProductNotFound) {
				return PurchaseOrderSummary{}, apperror.NotFound("produk tidak ditemukan", nil)
			}
			return PurchaseOrderSummary{}, apperror.Internal("failed to fetch product", err)
		}
		if !product.IsActive {
			return PurchaseOrderSummary{}, apperror.BadRequest("produk sudah diarsipkan, tidak bisa menambah stok", nil)
		}
		repoItems = append(repoItems, repository.CreatePOItemInput{
			ProductID:     product.ID,
			Quantity:      item.Quantity,
			PurchasePrice: item.PurchasePrice,
		})
		productRefs = append(productRefs, ProductRef{PublicID: product.PublicID, Name: product.Name})
	}

	po, items, err := s.purchaseOrderRepo.CreateWithGeneratedCode(ctx, repository.CreatePOInput{
		SupplierID: supplier.ID,
		Notes:      nilIfEmpty(input.Notes),
		CreatedBy:  creator.ID,
		Items:      repoItems,
	})
	if err != nil {
		if errors.Is(err, repository.ErrPOCodeGenerationFailed) {
			return PurchaseOrderSummary{}, apperror.Conflict("gagal membuat kode PO unik, coba lagi", nil)
		}
		return PurchaseOrderSummary{}, apperror.Internal("failed to create purchase order", err)
	}

	itemSummaries := make([]PurchaseOrderItemSummary, 0, len(items))
	for i, it := range items {
		itemSummaries = append(itemSummaries, PurchaseOrderItemSummary{
			PublicID:      it.PublicID,
			Product:       productRefs[i],
			Quantity:      it.Quantity,
			PurchasePrice: it.PurchasePrice,
		})
	}

	return toPurchaseOrderSummary(po, ProductRef{PublicID: supplier.PublicID, Name: supplier.Name}, itemSummaries, nil), nil
}

func (s *purchaseOrderService) List(ctx context.Context, input ListPOInput) (PurchaseOrderListResult, error) {
	page := input.Page
	if page <= 0 {
		page = defaultPOPage
	}
	limit := input.Limit
	if limit <= 0 {
		limit = defaultPOLimit
	}
	if limit > maxPOLimit {
		limit = maxPOLimit
	}

	filter := repository.POFilter{Page: page, Limit: limit}
	if input.Status != "" {
		if _, ok := allowedPOStatuses[input.Status]; !ok {
			return PurchaseOrderListResult{}, apperror.BadRequest("status harus BELUM_DITERIMA, DITERIMA, atau DIBATALKAN", nil)
		}
		status := input.Status
		filter.Status = &status
	}

	pos, total, err := s.purchaseOrderRepo.List(ctx, filter)
	if err != nil {
		return PurchaseOrderListResult{}, apperror.Internal("failed to list purchase orders", err)
	}

	items := make([]PurchaseOrderSummary, 0, len(pos))
	for i := range pos {
		items = append(items, toPurchaseOrderSummary(
			&pos[i].PurchaseOrder,
			ProductRef{PublicID: pos[i].SupplierPublicID, Name: pos[i].SupplierName},
			nil, nil,
		))
	}

	return PurchaseOrderListResult{
		Items:      items,
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: int(math.Ceil(float64(total) / float64(limit))),
	}, nil
}

func (s *purchaseOrderService) Get(ctx context.Context, publicID string) (PurchaseOrderSummary, error) {
	po, items, err := s.purchaseOrderRepo.FindByPublicID(ctx, publicID)
	if err != nil {
		if errors.Is(err, repository.ErrPurchaseOrderNotFound) {
			return PurchaseOrderSummary{}, apperror.NotFound("purchase order tidak ditemukan", nil)
		}
		return PurchaseOrderSummary{}, apperror.Internal("failed to fetch purchase order", err)
	}

	return toPurchaseOrderSummary(
		&po.PurchaseOrder,
		ProductRef{PublicID: po.SupplierPublicID, Name: po.SupplierName},
		toPurchaseOrderItemSummaries(items),
		nil,
	), nil
}

func (s *purchaseOrderService) Receive(ctx context.Context, input ReceivePOInput) (PurchaseOrderSummary, error) {
	_, poItems, err := s.purchaseOrderRepo.FindByPublicID(ctx, input.PublicID)
	if err != nil {
		if errors.Is(err, repository.ErrPurchaseOrderNotFound) {
			return PurchaseOrderSummary{}, apperror.NotFound("purchase order tidak ditemukan", nil)
		}
		return PurchaseOrderSummary{}, apperror.Internal("failed to fetch purchase order", err)
	}

	type resolvedItem struct {
		productID   int64
		productName string
		serials     []repository.ReceiveSerialInput
	}

	seenSerials := make(map[string]struct{})
	resolved := make([]resolvedItem, 0, len(input.Items))
	requestQuantities := make(map[int64]int, len(input.Items))

	for _, item := range input.Items {
		if len(item.Serials) == 0 {
			return PurchaseOrderSummary{}, apperror.UnprocessableEntity("serials wajib diisi di setiap item", nil)
		}

		repoSerials := make([]repository.ReceiveSerialInput, 0, len(item.Serials))
		for _, serial := range item.Serials {
			trimmed := strings.TrimSpace(serial.SerialNumber)
			if trimmed == "" {
				return PurchaseOrderSummary{}, apperror.UnprocessableEntity("serial_number wajib diisi di setiap unit", nil)
			}
			if _, ok := allowedConditions[serial.Condition]; !ok {
				return PurchaseOrderSummary{}, apperror.UnprocessableEntity("condition wajib diisi dan harus GOOD atau BAD di setiap unit", nil)
			}
			if _, dup := seenSerials[trimmed]; dup {
				return PurchaseOrderSummary{}, apperror.Conflict("serial_number tidak boleh sama antar unit dalam satu penerimaan", nil)
			}
			seenSerials[trimmed] = struct{}{}
			if err := validateProductionYear(serial.ProductionYear); err != nil {
				return PurchaseOrderSummary{}, err
			}
			repoSerials = append(repoSerials, repository.ReceiveSerialInput{
				SerialNumber:   trimmed,
				Condition:      serial.Condition,
				ProductionYear: serial.ProductionYear,
			})
		}

		product, err := s.productRepo.FindByPublicID(ctx, item.ProductPublicID)
		if err != nil {
			if errors.Is(err, repository.ErrProductNotFound) {
				return PurchaseOrderSummary{}, apperror.NotFound("produk tidak ditemukan", nil)
			}
			return PurchaseOrderSummary{}, apperror.Internal("failed to fetch product", err)
		}

		resolved = append(resolved, resolvedItem{
			productID:   product.ID,
			productName: product.Name,
			serials:     repoSerials,
		})
		requestQuantities[product.ID] = len(item.Serials)
	}

	poQuantities := make(map[int64]int, len(poItems))
	for _, poItem := range poItems {
		poQuantities[poItem.ProductID] = poItem.Quantity
	}
	if len(requestQuantities) != len(poQuantities) {
		return PurchaseOrderSummary{}, apperror.BadRequest("items harus mencakup semua produk di PO ini, tidak kurang tidak lebih", nil)
	}
	for productID, quantity := range poQuantities {
		given, ok := requestQuantities[productID]
		if !ok {
			return PurchaseOrderSummary{}, apperror.BadRequest("items harus mencakup semua produk di PO ini, tidak kurang tidak lebih", nil)
		}
		if given != quantity {
			return PurchaseOrderSummary{}, apperror.BadRequest(
				fmt.Sprintf("jumlah serial untuk salah satu produk harus %d sesuai quantity PO", quantity), nil)
		}
	}

	creator, err := s.userRepo.FindByPublicID(ctx, input.CreatedByPublicID)
	if err != nil {
		return PurchaseOrderSummary{}, apperror.Internal("failed to resolve acting user", err)
	}

	repoItems := make([]repository.ReceiveItemInput, 0, len(resolved))
	productNames := make(map[int64]string, len(resolved))
	for _, r := range resolved {
		repoItems = append(repoItems, repository.ReceiveItemInput{
			ProductID: r.productID,
			Serials:   r.serials,
		})
		productNames[r.productID] = r.productName
	}

	units, err := s.purchaseOrderRepo.Receive(ctx, input.PublicID, repository.ReceivePOInput{
		CreatedBy: creator.ID,
		Items:     repoItems,
	})
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrPurchaseOrderNotReceivable):
			return PurchaseOrderSummary{}, apperror.Conflict("PO sudah diterima atau dibatalkan, tidak bisa diterima lagi", nil)
		case errors.Is(err, repository.ErrSerialNumberTaken):
			return PurchaseOrderSummary{}, apperror.Conflict("serial_number sudah dipakai", nil)
		case errors.Is(err, repository.ErrBarcodeGenerationFailed):
			return PurchaseOrderSummary{}, apperror.Conflict("gagal membuat barcode unik, coba lagi", nil)
		default:
			return PurchaseOrderSummary{}, apperror.Internal("failed to receive purchase order", err)
		}
	}

	receivedUnits := make([]ReceivedUnitSummary, 0, len(units))
	for _, u := range units {
		receivedUnits = append(receivedUnits, ReceivedUnitSummary{
			StockItemPublicID: u.PublicID,
			Barcode:           u.Barcode,
			ProductName:       productNames[u.ProductID],
			SerialNumber:      u.SerialNumber,
			Condition:         u.Condition,
			ProductionYear:    u.ProductionYear,
		})
	}

	updatedPO, updatedItems, err := s.purchaseOrderRepo.FindByPublicID(ctx, input.PublicID)
	if err != nil {
		return PurchaseOrderSummary{}, apperror.Internal("failed to fetch updated purchase order", err)
	}

	return toPurchaseOrderSummary(
		&updatedPO.PurchaseOrder,
		ProductRef{PublicID: updatedPO.SupplierPublicID, Name: updatedPO.SupplierName},
		toPurchaseOrderItemSummaries(updatedItems),
		receivedUnits,
	), nil
}

func (s *purchaseOrderService) Cancel(ctx context.Context, publicID string) error {
	ok, err := s.purchaseOrderRepo.Cancel(ctx, publicID)
	if err != nil {
		return apperror.Internal("failed to cancel purchase order", err)
	}
	if ok {
		return nil
	}

	if _, _, err := s.purchaseOrderRepo.FindByPublicID(ctx, publicID); err != nil {
		if errors.Is(err, repository.ErrPurchaseOrderNotFound) {
			return apperror.NotFound("purchase order tidak ditemukan", nil)
		}
		return apperror.Internal("failed to fetch purchase order", err)
	}
	return apperror.Conflict("PO sudah diterima atau dibatalkan, tidak bisa dibatalkan", nil)
}

func toPurchaseOrderItemSummaries(items []repository.PurchaseOrderItemWithProduct) []PurchaseOrderItemSummary {
	summaries := make([]PurchaseOrderItemSummary, 0, len(items))
	for _, it := range items {
		summaries = append(summaries, PurchaseOrderItemSummary{
			PublicID:      it.PublicID,
			Product:       ProductRef{PublicID: it.ProductPublicID, Name: it.ProductName},
			Quantity:      it.Quantity,
			PurchasePrice: it.PurchasePrice,
		})
	}
	return summaries
}

func toPurchaseOrderSummary(po *model.PurchaseOrder, supplier ProductRef, items []PurchaseOrderItemSummary, receivedUnits []ReceivedUnitSummary) PurchaseOrderSummary {
	notes := ""
	if po.Notes != nil {
		notes = *po.Notes
	}
	return PurchaseOrderSummary{
		PublicID:      po.PublicID,
		POCode:        po.POCode,
		Supplier:      supplier,
		TotalAmount:   po.TotalAmount,
		Status:        po.Status,
		Notes:         notes,
		Items:         items,
		ReceivedUnits: receivedUnits,
		CreatedAt:     po.CreatedAt,
		ReceivedAt:    po.ReceivedAt,
	}
}
