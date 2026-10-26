package service

import (
	"context"
	"strings"

	"hyperhive/support/internal/domain"
)

func (service *Service) AuthenticateApp(id, secret string) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.store.AuthenticateApp(id, secret)
}
func (service *Service) Ingest(ctx context.Context, id, secret string, body domain.IngestRequest) (int, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if err := service.store.AuthenticateApp(id, secret); err != nil {
		return 0, err
	}
	if !body.Heartbeat && body.Health == nil && len(body.Streams) == 0 && len(body.Samples) == 0 {
		return 0, domain.Invalid("empty ingestion batch")
	}
	if len(body.Samples) > 1000 || len(body.Streams) > 100 {
		return 0, domain.Invalid("batch limits exceeded")
	}
	if body.InstanceID != nil && (!domain.ValidLength(*body.InstanceID, 1, 128) || strings.TrimSpace(*body.InstanceID) == "") {
		return 0, domain.Invalid("invalid instance_id")
	}
	if body.Health != nil {
		if body.Health.Status != "healthy" && body.Health.Status != "degraded" && body.Health.Status != "unhealthy" {
			return 0, domain.Invalid("invalid health status")
		}
		if !domain.ValidLength(body.Health.Message, 0, 1024) {
			return 0, domain.Invalid("health message is too long")
		}
	}
	app, err := service.store.App(id, service.now())
	if err != nil {
		return 0, err
	}
	existing, err := service.store.Streams(id)
	if err != nil {
		return 0, err
	}
	definitions := domain.StreamMap(existing)
	metadataChanged := false
	for _, stream := range body.Streams {
		if !domain.ValidLength(stream.Name, 1, 128) || strings.TrimSpace(stream.Name) == "" || !domain.ValidStreamType(stream.Type) {
			return 0, domain.Invalid("invalid stream definition")
		}
		previous, exists := definitions[stream.Name]
		if exists && previous.Type != stream.Type {
			return 0, domain.Conflict("stream type is immutable")
		}
		if !exists || previous != stream {
			metadataChanged = true
		}
		definitions[stream.Name] = stream
	}
	if len(definitions) > 256 {
		return 0, domain.Invalid("maximum 256 streams per App")
	}
	if len(body.Streams) > 0 {
		layout, err := service.store.Layout(id)
		if err != nil {
			return 0, err
		}
		for _, widget := range layout.Widgets {
			if stream, exists := definitions[widget.Stream]; exists && widget.StreamType != "" && widget.StreamType != stream.Type {
				return 0, domain.Conflict("stream type conflicts with configured widget stream_type")
			}
		}
	}
	for _, sample := range body.Samples {
		stream, exists := definitions[sample.Stream]
		if !exists {
			return 0, domain.Invalid("sample references undeclared stream")
		}
		if err := domain.ValidateValue(stream.Type, sample.Value); err != nil {
			return 0, err
		}
	}
	participant := domain.Instance{Health: "unknown"}
	discovered := false
	if body.InstanceID != nil {
		participant.ID = *body.InstanceID
		found := false
		for _, instance := range app.Instances {
			if instance.ID == participant.ID {
				participant = instance
				found = true
				break
			}
		}
		if !found {
			if len(app.Instances) >= 1024 {
				return 0, domain.Invalid("maximum 1024 instances per App")
			}
			discovered = true
		}
	} else {
		direct, err := service.store.DirectParticipant(id)
		if err != nil {
			return 0, err
		}
		if direct != nil {
			participant = *direct
		} else {
			discovered = true
		}
	}
	now := service.now()
	if body.Heartbeat {
		participant.LastHeartbeat = &now
	}
	if body.Health != nil {
		participant.Health = body.Health.Status
		participant.HealthMessage = body.Health.Message
	}
	domain.ParticipantState(&participant, app.Timeout, now)

	samples, err := service.store.RecordBatch(ctx, id, body, participant, now)
	if err != nil {
		return 0, err
	}
	if metadataChanged || discovered {
		service.changed(id)
	}
	for start := 0; start < len(samples); start += 32 {
		end := min(start+32, len(samples))
		service.hub.Publish(map[string]any{"type": "samples", "app_id": id, "samples": samples[start:end]})
	}
	app, err = service.store.App(id, service.now())
	if err != nil {
		return 0, err
	}
	service.publishState(app)
	return len(samples), nil
}
