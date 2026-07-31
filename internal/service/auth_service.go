package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"gold-track-be/internal/repository"
	"gold-track-be/pkg/apperror"
)

// invalidCredentialsMessage is returned for both "email not found" and "wrong
// password" (and inactive accounts) so the endpoint never reveals which case it was.
const invalidCredentialsMessage = "Email atau password salah"

const invalidTokenMessage = "Token tidak valid atau sudah tidak berlaku"

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

// Claims is the JWT payload: user_id/role for authorization, plus the
// registered "jti" (used as the blacklist key on logout) and "exp".
type Claims struct {
	UserID int64  `json:"user_id"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

type AuthService interface {
	Login(ctx context.Context, input LoginInput) (LoginResult, error)
	Logout(ctx context.Context, tokenString string) error
}

type authService struct {
	userRepo      repository.UserRepository
	blacklistRepo repository.TokenBlacklistRepository
	jwtSecret     []byte
	jwtExpiry     time.Duration
}

func NewAuthService(
	userRepo repository.UserRepository,
	blacklistRepo repository.TokenBlacklistRepository,
	jwtSecret string,
	jwtExpiry time.Duration,
) AuthService {
	return &authService{
		userRepo:      userRepo,
		blacklistRepo: blacklistRepo,
		jwtSecret:     []byte(jwtSecret),
		jwtExpiry:     jwtExpiry,
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

// Logout blacklists the token's jti until its natural expiry. A token that
// fails verification (bad signature, expired, or already blacklisted) is
// rejected — there is nothing meaningful to invalidate.
func (s *authService) Logout(ctx context.Context, tokenString string) error {
	claims, err := s.verifyToken(ctx, tokenString)
	if err != nil {
		return err
	}

	expiresAt := time.Now().Add(s.jwtExpiry)
	if claims.ExpiresAt != nil {
		expiresAt = claims.ExpiresAt.Time
	}

	if err := s.blacklistRepo.Add(ctx, claims.ID, expiresAt); err != nil {
		return apperror.Internal("failed to invalidate token", err)
	}
	return nil
}

func (s *authService) generateToken(userID int64, role string) (string, error) {
	jti, err := generateJTI()
	if err != nil {
		return "", fmt.Errorf("generate jti: %w", err)
	}

	now := time.Now()
	claims := Claims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.jwtExpiry)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.jwtSecret)
}

// verifyToken checks the JWT signature and expiry, then rejects tokens whose
// jti is already blacklisted (e.g. from a previous logout).
func (s *authService) verifyToken(ctx context.Context, tokenString string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.jwtSecret, nil
	})
	if err != nil || !token.Valid || claims.ID == "" {
		return nil, apperror.Unauthorized(invalidTokenMessage, err)
	}

	blacklisted, err := s.blacklistRepo.IsBlacklisted(ctx, claims.ID)
	if err != nil {
		return nil, apperror.Internal("failed to check token status", err)
	}
	if blacklisted {
		return nil, apperror.Unauthorized(invalidTokenMessage, nil)
	}

	return claims, nil
}

func generateJTI() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
