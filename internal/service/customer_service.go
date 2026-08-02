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
	defaultCustomerPage  = 1
	defaultCustomerLimit = 20
	maxCustomerLimit     = 100
)

var allowedIDTypes = map[string]struct{}{
	"KTP":      {},
	"SIM":      {},
	"PASSPORT": {},
}

// CustomerSummary is the public-facing view of a customer: only PublicID
// (UUID) ever leaves this layer, the internal bigint PK stays in
// model.Customer/the repository.
type CustomerSummary struct {
	PublicID  string
	Name      string
	Phone     string
	Email     string
	IDType    string
	IDNumber  string
	Address   string
	Notes     string
	IsActive  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type CreateCustomerInput struct {
	Name     string
	Phone    string
	Email    string
	IDType   string
	IDNumber string
	Address  string
	Notes    string
}

type UpdateCustomerInput struct {
	PublicID string
	Name     string
	Phone    string
	Email    string
	IDType   string
	IDNumber string
	Address  string
	Notes    string
	IsActive bool
}

type ListCustomersInput struct {
	Search string
	Page   int
	Limit  int
}

type CustomerListResult struct {
	Items      []CustomerSummary
	Page       int
	Limit      int
	Total      int
	TotalPages int
}

type CustomerService interface {
	Create(ctx context.Context, input CreateCustomerInput) (CustomerSummary, error)
	List(ctx context.Context, input ListCustomersInput) (CustomerListResult, error)
	Get(ctx context.Context, publicID string) (CustomerSummary, error)
	Update(ctx context.Context, input UpdateCustomerInput) (CustomerSummary, error)
	Deactivate(ctx context.Context, publicID string) error
}

type customerService struct {
	customerRepo repository.CustomerRepository
}

func NewCustomerService(customerRepo repository.CustomerRepository) CustomerService {
	return &customerService{customerRepo: customerRepo}
}

func (s *customerService) Create(ctx context.Context, input CreateCustomerInput) (CustomerSummary, error) {
	name := strings.TrimSpace(input.Name)
	phone := strings.TrimSpace(input.Phone)
	idType := strings.TrimSpace(input.IDType)
	if err := validateCustomerFields(name, phone, idType); err != nil {
		return CustomerSummary{}, err
	}

	customer := &model.Customer{
		Name:     name,
		Phone:    nilIfEmpty(phone),
		Email:    nilIfEmpty(input.Email),
		IDType:   nilIfEmpty(idType),
		IDNumber: nilIfEmpty(input.IDNumber),
		Address:  nilIfEmpty(input.Address),
		Notes:    nilIfEmpty(input.Notes),
		IsActive: true,
	}

	created, err := s.customerRepo.Create(ctx, customer)
	if err != nil {
		return CustomerSummary{}, apperror.Internal("failed to create customer", err)
	}

	return toCustomerSummary(created), nil
}

func (s *customerService) List(ctx context.Context, input ListCustomersInput) (CustomerListResult, error) {
	page := input.Page
	if page <= 0 {
		page = defaultCustomerPage
	}
	limit := input.Limit
	if limit <= 0 {
		limit = defaultCustomerLimit
	}
	if limit > maxCustomerLimit {
		limit = maxCustomerLimit
	}

	customers, total, err := s.customerRepo.List(ctx, repository.CustomerFilter{
		Search: strings.TrimSpace(input.Search),
		Page:   page,
		Limit:  limit,
	})
	if err != nil {
		return CustomerListResult{}, apperror.Internal("failed to list customers", err)
	}

	items := make([]CustomerSummary, 0, len(customers))
	for i := range customers {
		items = append(items, toCustomerSummary(&customers[i]))
	}

	return CustomerListResult{
		Items:      items,
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: int(math.Ceil(float64(total) / float64(limit))),
	}, nil
}

func (s *customerService) Get(ctx context.Context, publicID string) (CustomerSummary, error) {
	customer, err := s.customerRepo.FindByPublicID(ctx, publicID)
	if err != nil {
		if errors.Is(err, repository.ErrCustomerNotFound) {
			return CustomerSummary{}, apperror.NotFound("pelanggan tidak ditemukan", nil)
		}
		return CustomerSummary{}, apperror.Internal("failed to fetch customer", err)
	}
	return toCustomerSummary(customer), nil
}

func (s *customerService) Update(ctx context.Context, input UpdateCustomerInput) (CustomerSummary, error) {
	name := strings.TrimSpace(input.Name)
	phone := strings.TrimSpace(input.Phone)
	idType := strings.TrimSpace(input.IDType)
	if err := validateCustomerFields(name, phone, idType); err != nil {
		return CustomerSummary{}, err
	}

	existing, err := s.customerRepo.FindByPublicID(ctx, input.PublicID)
	if err != nil {
		if errors.Is(err, repository.ErrCustomerNotFound) {
			return CustomerSummary{}, apperror.NotFound("pelanggan tidak ditemukan", nil)
		}
		return CustomerSummary{}, apperror.Internal("failed to fetch customer", err)
	}

	existing.Name = name
	existing.Phone = nilIfEmpty(phone)
	existing.Email = nilIfEmpty(input.Email)
	existing.IDType = nilIfEmpty(idType)
	existing.IDNumber = nilIfEmpty(input.IDNumber)
	existing.Address = nilIfEmpty(input.Address)
	existing.Notes = nilIfEmpty(input.Notes)
	existing.IsActive = input.IsActive

	if err := s.customerRepo.Update(ctx, existing); err != nil {
		if errors.Is(err, repository.ErrCustomerNotFound) {
			return CustomerSummary{}, apperror.NotFound("pelanggan tidak ditemukan", nil)
		}
		return CustomerSummary{}, apperror.Internal("failed to update customer", err)
	}

	return toCustomerSummary(existing), nil
}

func (s *customerService) Deactivate(ctx context.Context, publicID string) error {
	if err := s.customerRepo.SetActive(ctx, publicID, false); err != nil {
		if errors.Is(err, repository.ErrCustomerNotFound) {
			return apperror.NotFound("pelanggan tidak ditemukan", nil)
		}
		return apperror.Internal("failed to deactivate customer", err)
	}
	return nil
}

func validateCustomerFields(name, phone, idType string) error {
	if name == "" {
		return apperror.BadRequest("name wajib diisi", nil)
	}
	if phone == "" {
		return apperror.BadRequest("phone wajib diisi", nil)
	}
	if idType != "" {
		if _, ok := allowedIDTypes[idType]; !ok {
			return apperror.BadRequest("id_type harus KTP, SIM, atau PASSPORT", nil)
		}
	}
	return nil
}

func toCustomerSummary(c *model.Customer) CustomerSummary {
	phone, email, idType, idNumber, address, notes := "", "", "", "", "", ""
	if c.Phone != nil {
		phone = *c.Phone
	}
	if c.Email != nil {
		email = *c.Email
	}
	if c.IDType != nil {
		idType = *c.IDType
	}
	if c.IDNumber != nil {
		idNumber = *c.IDNumber
	}
	if c.Address != nil {
		address = *c.Address
	}
	if c.Notes != nil {
		notes = *c.Notes
	}
	return CustomerSummary{
		PublicID:  c.PublicID,
		Name:      c.Name,
		Phone:     phone,
		Email:     email,
		IDType:    idType,
		IDNumber:  idNumber,
		Address:   address,
		Notes:     notes,
		IsActive:  c.IsActive,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}
}
