// Package usage implementa el registro y agregación del consumo de tokens.
package usage

import (
	"context"
	"log/slog"
	"sort"

	"github.com/forgen/forgen/internal/core/domain"
	"github.com/forgen/forgen/internal/core/ports"
)

// Service registra y agrega el uso de tokens por modelo.
type Service struct {
	store  ports.UsageStore
	logger *slog.Logger
}

// NewService construye el servicio de uso.
func NewService(store ports.UsageStore, logger *slog.Logger) *Service {
	return &Service{store: store, logger: logger}
}

// Record implementa ports.UsageRecorder (persiste un registro).
func (s *Service) Record(ctx context.Context, record domain.UsageRecord) error {
	if err := s.store.Append(ctx, record); err != nil {
		s.logger.Warn("no se pudo registrar uso", "err", err)
		return err
	}
	return nil
}

// List devuelve los registros recientes.
func (s *Service) List(ctx context.Context, limit int) ([]domain.UsageRecord, error) {
	return s.store.List(ctx, limit)
}

// Summarize agrega los registros por modelo.
func Summarize(records []domain.UsageRecord) []domain.UsageSummary {
	byModel := map[string]*domain.UsageSummary{}
	order := []string{}

	for _, record := range records {
		key := record.Key()
		summary, ok := byModel[key]
		if !ok {
			summary = &domain.UsageSummary{Model: key}
			byModel[key] = summary
			order = append(order, key)
		}
		summary.Requests++
		summary.InputTokens += record.InputTokens
		summary.OutputTokens += record.OutputTokens
	}

	summaries := make([]domain.UsageSummary, 0, len(order))
	for _, key := range order {
		summaries = append(summaries, *byModel[key])
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].InputTokens+summaries[i].OutputTokens > summaries[j].InputTokens+summaries[j].OutputTokens
	})
	return summaries
}

var _ ports.UsageRecorder = (*Service)(nil)
