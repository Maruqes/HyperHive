package domain

import "time"

func ParticipantState(participant *Instance, timeout int, now time.Time) {
	participant.Connectivity = "offline"
	if participant.LastHeartbeat != nil && now.Sub(*participant.LastHeartbeat) < time.Duration(timeout)*time.Second {
		participant.Connectivity = "online"
	}
	if participant.Health == "" {
		participant.Health = "unknown"
	}
}
func Aggregate(app *App, direct *Instance, now time.Time) {
	app.Connectivity = "unknown"
	app.Health = "unknown"
	app.HealthMessage = ""
	app.LastHeartbeat = nil
	participants := []Instance{}
	if direct != nil {
		ParticipantState(direct, app.Timeout, now)
		participants = append(participants, *direct)
	}
	for index := range app.Instances {
		ParticipantState(&app.Instances[index], app.Timeout, now)
		participants = append(participants, app.Instances[index])
	}
	if len(participants) == 0 {
		return
	}
	app.Connectivity = "online"
	app.Health = "healthy"
	priority := map[string]int{"healthy": 0, "unknown": 1, "degraded": 2, "unhealthy": 3}
	for _, participant := range participants {
		if participant.Connectivity == "offline" {
			app.Connectivity = "offline"
		}
		if priority[participant.Health] >= priority[app.Health] {
			app.Health = participant.Health
			app.HealthMessage = participant.HealthMessage
		}
		if participant.LastHeartbeat != nil && (app.LastHeartbeat == nil || participant.LastHeartbeat.After(*app.LastHeartbeat)) {
			app.LastHeartbeat = participant.LastHeartbeat
		}
	}
}
