package service

import (
	"context"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"gold-track-be/internal/repository"
	"gold-track-be/pkg/apperror"
)

// invalidCredentialsMessage is returned for both "email not found" and "wrong
// password" (and inactive accounts) so the endpoint never reveals which case it was.
const invalidCredentialsMessage = "Email atau password salah"

type LoginInput struct {
	Email    string
	Password string
}

type AuthenticatedUser struct {
	ID   int64
	Name string
	Role string
}

type LoginResult struct {
	Token string
	User  AuthenticatedUser
}

type AuthService interface {
	Login(ctx context.Context, input LoginInput) (LoginResult, error)
}

type authService struct {
	userRepo  repository.UserRepository
	jwtSecret []byte
	jwtExpiry time.Duration
}

func NewAuthService(userRepo repository.UserRepository, jwtSecret string, jwtExpiry time.Duration) AuthService {
	return &authService{
		userRepo:  userRepo,
		jwtSecret: []byte(jwtSecret),
		jwtExpiry: jwtExpiry,
	}
}

func (s *authService) Login(ctx context.Context, input LoginInput) (LoginResult, error) {
	user, err := s.userRepo.FindActiveByEmail(ctx, input.Email)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return LoginResult{}, apperror.Unauthorized(invalidCredentialsMessage, nil)
		}
		return LoginResult{}, apperror.Internal("failed to fetch user", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(input.Password)); err != nil {
		return LoginResult{}, apperror.Unauthorized(invalidCredentialsMessage, nil)
	}

	token, err := s.generateToken(user.ID, user.Role)
	if err != nil {
		return LoginResult{}, apperror.Internal("failed to generate token", err)
	}

	if err := s.userRepo.UpdateLastLogin(ctx, user.ID, time.Now()); err != nil {
		return LoginResult{}, apperror.Internal("failed to update last login", err)
	}

	return LoginResult{
		Token: token,
		User: AuthenticatedUser{
			ID:   user.ID,
			Name: user.Name,
			Role: user.Role,
		},
	}, nil
}

func (s *authService) generateToken(userID int64, role string) (string, error) {
	claims := jwt.MapClaims{
		"user_id": userID,
		"role":    role,
		"exp":     time.Now().Add(s.jwtExpiry).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.jwtSecret)
}
