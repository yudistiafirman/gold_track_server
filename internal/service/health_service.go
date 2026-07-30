package service

import (
	"context"

	"gold-track-be/internal/repository"
)

type HealthStatus struct {
	Status   string `json:"status"`
	Database string `json:"database"`
}

type HealthService interface {
	Check(ctx context.Context) (HealthStatus, error)
}

type healthService struct {
	healthRepo repository.HealthRepository
}

func NewHealthService(healthRepo repository.HealthRepository) HealthService {
	return &healthService{healthRepo: healthRepo}
}

func (s *healthService) Check(ctx context.Context) (HealthStatus, error) {
	if err := s.healthRepo.Ping(ctx); err != nil {
		return HealthStatus{Status: "down", Database: "down"}, err
	}
	return HealthStatus{Status: "up", Database: "up"}, nil
}
