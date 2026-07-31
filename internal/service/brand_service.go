package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"gold-track-be/internal/model"
	"gold-track-be/internal/repository"
	"gold-track-be/pkg/apperror"
)

// BrandSummary is the public-facing view of a brand: only PublicID (UUID)
// ever leaves this layer, the internal bigint PK stays in model.Brand/the
// repository.
type BrandSummary struct {
	PublicID  string
	Name      string
	IsActive  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type CreateBrandInput struct {
	Name string
}

type UpdateBrandInput struct {
	PublicID string
	Name     string
	IsActive bool
}

type BrandService interface {
	Create(ctx context.Context, input CreateBrandInput) (BrandSummary, error)
	List(ctx context.Context) ([]BrandSummary, error)
	Get(ctx context.Context, publicID string) (BrandSummary, error)
	Update(ctx context.Context, input UpdateBrandInput) (BrandSummary, error)
	Deactivate(ctx context.Context, publicID string) error
}

type brandService struct {
	brandRepo repository.BrandRepository
}

func NewBrandService(brandRepo repository.BrandRepository) BrandService {
	return &brandService{brandRepo: brandRepo}
}

func (s *brandService) Create(ctx context.Context, input CreateBrandInput) (BrandSummary, error) {
	name := strings.TrimSpace(input.Name)
	if err := validateBrandName(name); err != nil {
		return BrandSummary{}, err
	}

	brand := &model.Brand{Name: name, IsActive: true}

	created, err := s.brandRepo.Create(ctx, brand)
	if err != nil {
		if errors.Is(err, repository.ErrBrandNameTaken) {
			return BrandSummary{}, apperror.Conflict("nama brand sudah dipakai", nil)
		}
		return BrandSummary{}, apperror.Internal("failed to create brand", err)
	}

	return toBrandSummary(created), nil
}

func (s *brandService) List(ctx context.Context) ([]BrandSummary, error) {
	brands, err := s.brandRepo.List(ctx)
	if err != nil {
		return nil, apperror.Internal("failed to list brands", err)
	}

	summaries := make([]BrandSummary, 0, len(brands))
	for i := range brands {
		summaries = append(summaries, toBrandSummary(&brands[i]))
	}
	return summaries, nil
}

func (s *brandService) Get(ctx context.Context, publicID string) (BrandSummary, error) {
	brand, err := s.brandRepo.FindByPublicID(ctx, publicID)
	if err != nil {
		if errors.Is(err, repository.ErrBrandNotFound) {
			return BrandSummary{}, apperror.NotFound("brand tidak ditemukan", nil)
		}
		return BrandSummary{}, apperror.Internal("failed to fetch brand", err)
	}
	return toBrandSummary(brand), nil
}

func (s *brandService) Update(ctx context.Context, input UpdateBrandInput) (BrandSummary, error) {
	name := strings.TrimSpace(input.Name)
	if err := validateBrandName(name); err != nil {
		return BrandSummary{}, err
	}

	existing, err := s.brandRepo.FindByPublicID(ctx, input.PublicID)
	if err != nil {
		if errors.Is(err, repository.ErrBrandNotFound) {
			return BrandSummary{}, apperror.NotFound("brand tidak ditemukan", nil)
		}
		return BrandSummary{}, apperror.Internal("failed to fetch brand", err)
	}

	existing.Name = name
	existing.IsActive = input.IsActive

	if err := s.brandRepo.Update(ctx, existing); err != nil {
		if errors.Is(err, repository.ErrBrandNotFound) {
			return BrandSummary{}, apperror.NotFound("brand tidak ditemukan", nil)
		}
		if errors.Is(err, repository.ErrBrandNameTaken) {
			return BrandSummary{}, apperror.Conflict("nama brand sudah dipakai", nil)
		}
		return BrandSummary{}, apperror.Internal("failed to update brand", err)
	}

	return toBrandSummary(existing), nil
}

func (s *brandService) Deactivate(ctx context.Context, publicID string) error {
	if err := s.brandRepo.SetActive(ctx, publicID, false); err != nil {
		if errors.Is(err, repository.ErrBrandNotFound) {
			return apperror.NotFound("brand tidak ditemukan", nil)
		}
		return apperror.Internal("failed to deactivate brand", err)
	}
	return nil
}

func validateBrandName(name string) error {
	if name == "" {
		return apperror.BadRequest("name wajib diisi", nil)
	}
	return nil
}

func toBrandSummary(b *model.Brand) BrandSummary {
	return BrandSummary{
		PublicID:  b.PublicID,
		Name:      b.Name,
		IsActive:  b.IsActive,
		CreatedAt: b.CreatedAt,
		UpdatedAt: b.UpdatedAt,
	}
}
