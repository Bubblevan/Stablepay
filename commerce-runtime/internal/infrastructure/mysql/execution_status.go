package mysql

import (
	"context"
	"errors"
	"time"

	"github.com/stablepay/commerce-runtime/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type EpisodeExecutionStatusModel struct {
	EpisodeID        string     `gorm:"column:episode_id;type:varchar(128);primaryKey"`
	Status           string     `gorm:"column:status;type:varchar(24);not null"`
	AttemptCount     int        `gorm:"column:attempt_count;not null"`
	LastStartedAt    time.Time  `gorm:"column:last_started_at;not null"`
	LastFinishedAt   *time.Time `gorm:"column:last_finished_at"`
	LastErrorCode    string     `gorm:"column:last_error_code;type:varchar(128)"`
	LastErrorMessage string     `gorm:"column:last_error_message;type:varchar(512)"`
	LastErrorAt      *time.Time `gorm:"column:last_error_at"`
	NextRetryAt      *time.Time `gorm:"column:next_retry_at"`
	UpdatedAt        time.Time  `gorm:"column:updated_at;not null"`
}

func (EpisodeExecutionStatusModel) TableName() string { return "episode_execution_status" }

func (s *Store) GetEpisodeExecutionStatus(ctx context.Context, episodeID string) (*repository.EpisodeExecutionStatus, error) {
	var row EpisodeExecutionStatusModel
	err := s.db.WithContext(ctx).Where("episode_id = ?", episodeID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return modelToExecutionStatus(row)
}

func (s *Store) UpsertEpisodeExecutionStatus(ctx context.Context, value *repository.EpisodeExecutionStatus) error {
	model, err := executionStatusToModel(value)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "episode_id"}}, UpdateAll: true}).Create(model).Error
}

func executionStatusToModel(value *repository.EpisodeExecutionStatus) (*EpisodeExecutionStatusModel, error) {
	if value == nil || value.Validate() != nil {
		return nil, repository.ErrInvalidExecutionStatus
	}
	return &EpisodeExecutionStatusModel{
		EpisodeID: value.EpisodeID, Status: value.Status, AttemptCount: value.AttemptCount,
		LastStartedAt: value.LastStartedAt, LastFinishedAt: value.LastFinishedAt,
		LastErrorCode: value.LastErrorCode, LastErrorMessage: value.LastErrorMessage, LastErrorAt: value.LastErrorAt,
		NextRetryAt: value.NextRetryAt, UpdatedAt: value.UpdatedAt,
	}, nil
}

func modelToExecutionStatus(value EpisodeExecutionStatusModel) (*repository.EpisodeExecutionStatus, error) {
	result := &repository.EpisodeExecutionStatus{
		EpisodeID: value.EpisodeID, Status: value.Status, AttemptCount: value.AttemptCount,
		LastStartedAt: value.LastStartedAt, LastFinishedAt: value.LastFinishedAt,
		LastErrorCode: value.LastErrorCode, LastErrorMessage: value.LastErrorMessage, LastErrorAt: value.LastErrorAt,
		NextRetryAt: value.NextRetryAt, UpdatedAt: value.UpdatedAt,
	}
	if err := result.Validate(); err != nil {
		return nil, err
	}
	return result, nil
}
