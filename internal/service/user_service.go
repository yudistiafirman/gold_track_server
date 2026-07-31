package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"gold-track-be/internal/model"
	"gold-track-be/internal/repository"
	"gold-track-be/pkg/apperror"
)

const minPasswordLength = 8

var allowedRoles = map[string]struct{}{
	"SUPER_ADMIN": {},
	"ADMIN":       {},
	"KASIR":       {},
}

// UserSummary is the public-facing view of a user: only PublicID (UUID) ever
// leaves this layer, the internal bigint PK stays in model.User/the repository.
type UserSummary struct {
	PublicID    string
	Name        string
	Email       string
	Role        string
	IsActive    bool
	LastLoginAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type CreateUserInput struct {
	Name     string
	Email    string
	Password string
	Role     string
}

type UpdateUserInput struct {
	PublicID string
	Name     string
	Email    string
	Role     string
	IsActive bool
	// Password is optional: empty means keep the existing password.
	Password string
}

type UserService interface {
	Create(ctx context.Context, input CreateUserInput) (UserSummary, error)
	List(ctx context.Context) ([]UserSummary, error)
	Get(ctx context.Context, publicID string) (UserSummary, error)
	Update(ctx context.Context, input UpdateUserInput) (UserSummary, error)
	// Deactivate soft-deletes the target user, identified by public_id.
	// actingPublicID is the requester's own public_id (from JWT claims) —
	// deactivating your own account is rejected.
	Deactivate(ctx context.Context, actingPublicID, targetPublicID string) error
}

type userService struct {
	userRepo repository.UserRepository
}

func NewUserService(userRepo repository.UserRepository) UserService {
	return &userService{userRepo: userRepo}
}

func (s *userService) Create(ctx context.Context, input CreateUserInput) (UserSummary, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Email = strings.TrimSpace(input.Email)

	if err := validateUserFields(input.Name, input.Email, input.Role); err != nil {
		return UserSummary{}, err
	}
	if len(input.Password) < minPasswordLength {
		return UserSummary{}, apperror.BadRequest("password minimal 8 karakter", nil)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return UserSummary{}, apperror.Internal("failed to hash password", err)
	}

	user := &model.User{
		Name:         input.Name,
		Email:        input.Email,
		PasswordHash: string(hash),
		Role:         input.Role,
		IsActive:     true,
	}

	created, err := s.userRepo.Create(ctx, user)
	if err != nil {
		if errors.Is(err, repository.ErrEmailTaken) {
			return UserSummary{}, apperror.Conflict("email sudah dipakai", nil)
		}
		return UserSummary{}, apperror.Internal("failed to create user", err)
	}

	return toUserSummary(created), nil
}

func (s *userService) List(ctx context.Context) ([]UserSummary, error) {
	users, err := s.userRepo.List(ctx)
	if err != nil {
		return nil, apperror.Internal("failed to list users", err)
	}

	summaries := make([]UserSummary, 0, len(users))
	for i := range users {
		summaries = append(summaries, toUserSummary(&users[i]))
	}
	return summaries, nil
}

func (s *userService) Get(ctx context.Context, publicID string) (UserSummary, error) {
	user, err := s.userRepo.FindByPublicID(ctx, publicID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return UserSummary{}, apperror.NotFound("user tidak ditemukan", nil)
		}
		return UserSummary{}, apperror.Internal("failed to fetch user", err)
	}
	return toUserSummary(user), nil
}

func (s *userService) Update(ctx context.Context, input UpdateUserInput) (UserSummary, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Email = strings.TrimSpace(input.Email)

	if err := validateUserFields(input.Name, input.Email, input.Role); err != nil {
		return UserSummary{}, err
	}

	existing, err := s.userRepo.FindByPublicID(ctx, input.PublicID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return UserSummary{}, apperror.NotFound("user tidak ditemukan", nil)
		}
		return UserSummary{}, apperror.Internal("failed to fetch user", err)
	}

	passwordHash := existing.PasswordHash
	if input.Password != "" {
		if len(input.Password) < minPasswordLength {
			return UserSummary{}, apperror.BadRequest("password minimal 8 karakter", nil)
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
		if err != nil {
			return UserSummary{}, apperror.Internal("failed to hash password", err)
		}
		passwordHash = string(hash)
	}

	existing.Name = input.Name
	existing.Email = input.Email
	existing.Role = input.Role
	existing.IsActive = input.IsActive
	existing.PasswordHash = passwordHash

	if err := s.userRepo.Update(ctx, existing); err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return UserSummary{}, apperror.NotFound("user tidak ditemukan", nil)
		}
		if errors.Is(err, repository.ErrEmailTaken) {
			return UserSummary{}, apperror.Conflict("email sudah dipakai", nil)
		}
		return UserSummary{}, apperror.Internal("failed to update user", err)
	}

	return toUserSummary(existing), nil
}

func (s *userService) Deactivate(ctx context.Context, actingPublicID, targetPublicID string) error {
	if actingPublicID == targetPublicID {
		return apperror.BadRequest("tidak bisa menonaktifkan akun sendiri", nil)
	}

	if err := s.userRepo.SetActive(ctx, targetPublicID, false); err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return apperror.NotFound("user tidak ditemukan", nil)
		}
		return apperror.Internal("failed to deactivate user", err)
	}
	return nil
}

func validateUserFields(name, email, role string) error {
	if name == "" || email == "" || role == "" {
		return apperror.BadRequest("name, email, dan role wajib diisi", nil)
	}
	if _, ok := allowedRoles[role]; !ok {
		return apperror.BadRequest("role harus salah satu dari SUPER_ADMIN, ADMIN, KASIR", nil)
	}
	return nil
}

func toUserSummary(u *model.User) UserSummary {
	return UserSummary{
		PublicID:    u.PublicID,
		Name:        u.Name,
		Email:       u.Email,
		Role:        u.Role,
		IsActive:    u.IsActive,
		LastLoginAt: u.LastLoginAt,
		CreatedAt:   u.CreatedAt,
		UpdatedAt:   u.UpdatedAt,
	}
}
