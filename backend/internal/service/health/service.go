package health

import (
	"context"

	"project-framework/internal/model"
)

type Service struct{}

func New() *Service {
	return &Service{}
}

func (*Service) Check(context.Context) model.HealthReport {
	return model.HealthReport{Status: model.HealthStatusOK}
}
