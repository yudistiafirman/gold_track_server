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

// shopSettingKeys are the only settings keys exposed through this service —
// scoped to the shop's own display data (name/address/phone), the same
// keys transactionService reads for receipts.
var shopSettingKeys = []string{"shop_name", "shop_address", "shop_phone"}

type SettingSummary struct {
	Key         string
	Value       string
	Description string
	UpdatedAt   time.Time
}

// UpdateShopSettingsInput fields are pointers so a client can update just
// one key (e.g. only shop_phone) without clobbering the others.
type UpdateShopSettingsInput struct {
	ShopName          *string
	ShopAddress       *string
	ShopPhone         *string
	UpdatedByPublicID string
}

type SettingsService interface {
	ListShopSettings(ctx context.Context) ([]SettingSummary, error)
	UpdateShopSettings(ctx context.Context, input UpdateShopSettingsInput) ([]SettingSummary, error)
}

type settingsService struct {
	settingsRepo repository.SettingsRepository
	userRepo     repository.UserRepository
}

func NewSettingsService(settingsRepo repository.SettingsRepository, userRepo repository.UserRepository) SettingsService {
	return &settingsService{settingsRepo: settingsRepo, userRepo: userRepo}
}

func (s *settingsService) ListShopSettings(ctx context.Context) ([]SettingSummary, error) {
	settings, err := s.settingsRepo.List(ctx, shopSettingKeys)
	if err != nil {
		return nil, apperror.Internal("failed to list settings", err)
	}

	summaries := make([]SettingSummary, 0, len(settings))
	for i := range settings {
		summaries = append(summaries, toSettingSummary(&settings[i]))
	}
	return summaries, nil
}

func (s *settingsService) UpdateShopSettings(ctx context.Context, input UpdateShopSettingsInput) ([]SettingSummary, error) {
	updates := map[string]*string{
		"shop_name":    input.ShopName,
		"shop_address": input.ShopAddress,
		"shop_phone":   input.ShopPhone,
	}

	hasUpdate := false
	for _, v := range updates {
		if v != nil {
			hasUpdate = true
			break
		}
	}
	if !hasUpdate {
		return nil, apperror.BadRequest("minimal satu field harus diisi", nil)
	}

	actor, err := s.userRepo.FindByPublicID(ctx, input.UpdatedByPublicID)
	if err != nil {
		return nil, apperror.Internal("failed to resolve acting user", err)
	}

	for _, key := range shopSettingKeys {
		value := updates[key]
		if value == nil {
			continue
		}
		trimmed := strings.TrimSpace(*value)
		if trimmed == "" {
			return nil, apperror.BadRequest(key+" tidak boleh kosong", nil)
		}

		if _, err := s.settingsRepo.UpdateValue(ctx, key, trimmed, actor.ID); err != nil {
			if errors.Is(err, repository.ErrSettingNotFound) {
				return nil, apperror.Internal("setting "+key+" belum di-seed", err)
			}
			return nil, apperror.Internal("failed to update setting", err)
		}
	}

	return s.ListShopSettings(ctx)
}

func toSettingSummary(s *model.Setting) SettingSummary {
	description := ""
	if s.Description != nil {
		description = *s.Description
	}
	return SettingSummary{
		Key:         s.Key,
		Value:       s.Value,
		Description: description,
		UpdatedAt:   s.UpdatedAt,
	}
}
