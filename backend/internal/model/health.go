package model

type HealthStatus string

const HealthStatusOK HealthStatus = "ok"

type HealthReport struct {
	Status HealthStatus
}
