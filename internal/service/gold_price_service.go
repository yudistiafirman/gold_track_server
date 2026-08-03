package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"gold-track-be/internal/model"
	"gold-track-be/internal/repository"
	"gold-track-be/pkg/apperror"
)

// Source picked for the BE-404 sync: logam-mulia-api's "anekalogam" feed
// carries genuine Antam-branded bar prices (unlike e.g. "pegadaian", which
// only reports a 0.01gr denomination). Within that feed, materialType "LM
// Antam produksi tahun 2026" is the one entry per weight that uses this
// consistent label across all denominations (0.5gr..50gr) — the 1gr weight
// also has a one-off "Certicard gramasi 100 gram" entry that was
// deliberately not picked, since it has no equivalent at other weights.
const (
	goldPriceExternalURL        = "https://logam-mulia-api.iamutaki.workers.dev/api/prices/anekalogam"
	goldPriceExternalTimeout    = 5 * time.Second
	goldPriceExternalSource     = "logam-mulia-api:anekalogam"
	goldPriceTargetMaterialType = "LM Antam produksi tahun 2026"
	goldPriceTargetWeightGrams  = 1
)

type externalGoldPriceResponse struct {
	Data []externalGoldPriceItem `json:"data"`
}

type externalGoldPriceItem struct {
	MaterialType string  `json:"materialType"`
	Weight       float64 `json:"weight"`
	SellPrice    float64 `json:"sellPrice"`
	BuybackPrice float64 `json:"buybackPrice"`
}

type GoldPriceSummary struct {
	Source         string
	PriceBuy       float64
	PriceSell      float64
	PriceReference float64
	FetchedAt      time.Time
}

type GoldPriceService interface {
	// GetActive returns the current reference price, or nil if the sync
	// job hasn't produced one yet (fresh deploy).
	GetActive(ctx context.Context) (*GoldPriceSummary, error)
	// SyncOnce fetches the external source once and, on success, replaces
	// the active price row. On any failure (network, timeout, unexpected
	// shape, no matching entry) it returns an error and leaves whatever
	// price row is currently active untouched — callers should log and
	// move on, never let this take the API down.
	SyncOnce(ctx context.Context) error
}

type goldPriceService struct {
	goldPriceRepo repository.GoldPriceRepository
	httpClient    *http.Client
}

func NewGoldPriceService(goldPriceRepo repository.GoldPriceRepository) GoldPriceService {
	return &goldPriceService{
		goldPriceRepo: goldPriceRepo,
		httpClient:    &http.Client{Timeout: goldPriceExternalTimeout},
	}
}

func (s *goldPriceService) GetActive(ctx context.Context) (*GoldPriceSummary, error) {
	active, err := s.goldPriceRepo.GetActive(ctx)
	if err != nil {
		return nil, apperror.Internal("failed to fetch active gold price", err)
	}
	if active == nil {
		return nil, nil
	}
	return toGoldPriceSummary(active), nil
}

func (s *goldPriceService) SyncOnce(ctx context.Context) error {
	fetchCtx, cancel := context.WithTimeout(ctx, goldPriceExternalTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, goldPriceExternalURL, nil)
	if err != nil {
		return fmt.Errorf("build gold price request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch gold price: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch gold price: unexpected status %d", resp.StatusCode)
	}

	var parsed externalGoldPriceResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return fmt.Errorf("decode gold price response: %w", err)
	}

	item, ok := findTargetGoldPriceItem(parsed.Data)
	if !ok {
		return fmt.Errorf("gold price: no entry matching weight=%dgr materialType=%q", goldPriceTargetWeightGrams, goldPriceTargetMaterialType)
	}

	priceReference := item.SellPrice
	source := goldPriceExternalSource
	if _, err := s.goldPriceRepo.ReplaceActive(ctx, &model.GoldPrice{
		PriceBuy:       item.BuybackPrice,
		PriceSell:      item.SellPrice,
		PriceReference: &priceReference,
		Source:         &source,
	}); err != nil {
		return fmt.Errorf("save gold price: %w", err)
	}
	return nil
}

func findTargetGoldPriceItem(items []externalGoldPriceItem) (externalGoldPriceItem, bool) {
	for _, item := range items {
		if item.Weight == goldPriceTargetWeightGrams && item.MaterialType == goldPriceTargetMaterialType {
			return item, true
		}
	}
	return externalGoldPriceItem{}, false
}

func toGoldPriceSummary(p *model.GoldPrice) *GoldPriceSummary {
	source := ""
	if p.Source != nil {
		source = *p.Source
	}
	priceReference := p.PriceSell
	if p.PriceReference != nil {
		priceReference = *p.PriceReference
	}
	return &GoldPriceSummary{
		Source:         source,
		PriceBuy:       p.PriceBuy,
		PriceSell:      p.PriceSell,
		PriceReference: priceReference,
		FetchedAt:      p.EffectiveFrom,
	}
}
