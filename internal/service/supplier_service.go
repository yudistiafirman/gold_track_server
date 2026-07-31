package service

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"gold-track-be/internal/model"
	"gold-track-be/internal/repository"
	"gold-track-be/pkg/apperror"
)

const (
	defaultSupplierPage  = 1
	defaultSupplierLimit = 20
	maxSupplierLimit     = 100
)

// SupplierSummary is the public-facing view of a supplier: only PublicID
// (UUID) ever leaves this layer, the internal bigint PK stays in
// model.Supplier/the repository.
type SupplierSummary struct {
	PublicID  string
	Name      string
	Phone     string
	Address   string
	Notes     string
	IsActive  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type CreateSupplierInput struct {
	Name    string
	Phone   string
	Address string
	Notes   string
}

type UpdateSupplierInput struct {
	PublicID string
	Name     string
	Phone    string
	Address  string
	Notes    string
	IsActive bool
}

type ListSuppliersInput struct {
	Search string
	Page   int
	Limit  int
}

type SupplierListResult struct {
	Items      []SupplierSummary
	Page       int
	Limit      int
	Total      int
	TotalPages int
}

type SupplierService interface {
	Create(ctx context.Context, input CreateSupplierInput) (SupplierSummary, error)
	List(ctx context.Context, input ListSuppliersInput) (SupplierListResult, error)
	Get(ctx context.Context, publicID string) (SupplierSummary, error)
	Update(ctx context.Context, input UpdateSupplierInput) (SupplierSummary, error)
	Deactivate(ctx context.Context, publicID string) error
}

type supplierService struct {
	supplierRepo repository.SupplierRepository
}

func NewSupplierService(supplierRepo repository.SupplierRepository) SupplierService {
	return &supplierService{supplierRepo: supplierRepo}
}

func (s *supplierService) Create(ctx context.Context, input CreateSupplierInput) (SupplierSummary, error) {
	name := strings.TrimSpace(input.Name)
	if err := validateSupplierName(name); err != nil {
		return SupplierSummary{}, err
	}

	supplier := &model.Supplier{
		Name:     name,
		Phone:    nilIfEmpty(input.Phone),
		Address:  nilIfEmpty(input.Address),
		Notes:    nilIfEmpty(input.Notes),
		IsActive: true,
	}

	created, err := s.supplierRepo.Create(ctx, supplier)
	if err != nil {
		return SupplierSummary{}, apperror.Internal("failed to create supplier", err)
	}

	return toSupplierSummary(created), nil
}

func (s *supplierService) List(ctx context.Context, input ListSuppliersInput) (SupplierListResult, error) {
	page := input.Page
	if page <= 0 {
		page = defaultSupplierPage
	}
	limit := input.Limit
	if limit <= 0 {
		limit = defaultSupplierLimit
	}
	if limit > maxSupplierLimit {
		limit = maxSupplierLimit
	}

	suppliers, total, err := s.supplierRepo.List(ctx, repository.SupplierFilter{
		Search: strings.TrimSpace(input.Search),
		Page:   page,
		Limit:  limit,
	})
	if err != nil {
		return SupplierListResult{}, apperror.Internal("failed to list suppliers", err)
	}

	items := make([]SupplierSummary, 0, len(suppliers))
	for i := range suppliers {
		items = append(items, toSupplierSummary(&suppliers[i]))
	}

	return SupplierListResult{
		Items:      items,
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: int(math.Ceil(float64(total) / float64(limit))),
	}, nil
}

func (s *supplierService) Get(ctx context.Context, publicID string) (SupplierSummary, error) {
	supplier, err := s.supplierRepo.FindByPublicID(ctx, publicID)
	if err != nil {
		if errors.Is(err, repository.ErrSupplierNotFound) {
			return SupplierSummary{}, apperror.NotFound("supplier tidak ditemukan", nil)
		}
		return SupplierSummary{}, apperror.Internal("failed to fetch supplier", err)
	}
	return toSupplierSummary(supplier), nil
}

func (s *supplierService) Update(ctx context.Context, input UpdateSupplierInput) (SupplierSummary, error) {
	name := strings.TrimSpace(input.Name)
	if err := validateSupplierName(name); err != nil {
		return SupplierSummary{}, err
	}

	existing, err := s.supplierRepo.FindByPublicID(ctx, input.PublicID)
	if err != nil {
		if errors.Is(err, repository.ErrSupplierNotFound) {
			return SupplierSummary{}, apperror.NotFound("supplier tidak ditemukan", nil)
		}
		return SupplierSummary{}, apperror.Internal("failed to fetch supplier", err)
	}

	existing.Name = name
	existing.Phone = nilIfEmpty(input.Phone)
	existing.Address = nilIfEmpty(input.Address)
	existing.Notes = nilIfEmpty(input.Notes)
	existing.IsActive = input.IsActive

	if err := s.supplierRepo.Update(ctx, existing); err != nil {
		if errors.Is(err, repository.ErrSupplierNotFound) {
			return SupplierSummary{}, apperror.NotFound("supplier tidak ditemukan", nil)
		}
		return SupplierSummary{}, apperror.Internal("failed to update supplier", err)
	}

	return toSupplierSummary(existing), nil
}

func (s *supplierService) Deactivate(ctx context.Context, publicID string) error {
	if err := s.supplierRepo.SetActive(ctx, publicID, false); err != nil {
		if errors.Is(err, repository.ErrSupplierNotFound) {
			return apperror.NotFound("supplier tidak ditemukan", nil)
		}
		return apperror.Internal("failed to deactivate supplier", err)
	}
	return nil
}

func validateSupplierName(name string) error {
	if name == "" {
		return apperror.BadRequest("name wajib diisi", nil)
	}
	return nil
}

func nilIfEmpty(s string) *string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func toSupplierSummary(s *model.Supplier) SupplierSummary {
	phone, address, notes := "", "", ""
	if s.Phone != nil {
		phone = *s.Phone
	}
	if s.Address != nil {
		address = *s.Address
	}
	if s.Notes != nil {
		notes = *s.Notes
	}
	return SupplierSummary{
		PublicID:  s.PublicID,
		Name:      s.Name,
		Phone:     phone,
		Address:   address,
		Notes:     notes,
		IsActive:  s.IsActive,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
	}
}
