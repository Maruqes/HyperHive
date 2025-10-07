package info

import (
	"context"
	"fmt"
	"time"

	"github.com/coreos/go-systemd/v22/dbus"
	"github.com/coreos/go-systemd/v22/sdjournal"
)

type ServicesInfoStruct struct{}

var ServicesInfo ServicesInfoStruct

type Service struct {
	Name        string
	Description string
	LoadState   string
	ActiveState string
	SubState    string
}

func (s *ServicesInfoStruct) GetServices() ([]Service, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := dbus.NewSystemdConnectionContext(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	units, err := conn.ListUnitsContext(ctx)
	if err != nil {
		return nil, err
	}

	var services []Service
	for _, u := range units {
		service := Service{
			Name:        u.Name,
			Description: u.Description,
			LoadState:   u.LoadState,
			ActiveState: u.ActiveState,
			SubState:    u.SubState,
		}
		services = append(services, service)
	}
	return services, nil
}

func (s *ServicesInfoStruct) GetServiceStatus(name string) (Service, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := dbus.NewSystemdConnectionContext(ctx)
	if err != nil {
		return Service{}, err
	}
	defer conn.Close()

	unitStatus, err := conn.GetUnitPropertiesContext(ctx, name)
	if err != nil {
		return Service{}, err
	}

	service := Service{
		Name:        name,
		Description: unitStatus["Description"].(string),
		LoadState:   unitStatus["LoadState"].(string),
		ActiveState: unitStatus["ActiveState"].(string),
		SubState:    unitStatus["SubState"].(string),
	}

	return service, nil
}

func (s *ServicesInfoStruct) GetLogs(name string, lines int) (string, error) {
	j, err := sdjournal.NewJournal()
	if err != nil {
		return "", err
	}
	defer j.Close()
	err = j.AddMatch("UNIT=" + name)
	if err != nil {
		return "", err
	}

	var logs string
	count := 0
	for {
		if count >= lines {
			break
		}
		n, err := j.Next()
		if err != nil {
			return "", err
		}
		if n == 0 {
			break
		}
		entry, err := j.GetEntry()
		if err != nil {
			return "", err
		}
		ts := time.Unix(0, int64(entry.RealtimeTimestamp)*int64(time.Microsecond)).Format(time.RFC3339)
		logs += fmt.Sprintf("%s %s\n", ts, entry.Fields["MESSAGE"])
		count++
	}
	return logs, nil
}

func (s *ServicesInfoStruct) StartService(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := dbus.NewSystemdConnectionContext(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.StartUnitContext(ctx, name, "replace", nil)
	if err != nil {
		return err
	}
	return nil
}

func (s *ServicesInfoStruct) StopService(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := dbus.NewSystemdConnectionContext(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.StopUnitContext(ctx, name, "replace", nil)
	if err != nil {
		return err
	}
	return nil
}

func (s *ServicesInfoStruct) RestartService(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := dbus.NewSystemdConnectionContext(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.RestartUnitContext(ctx, name, "replace", nil)
	if err != nil {
		return err
	}
	return nil
}

func (s *ServicesInfoStruct) EnableService(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := dbus.NewSystemdConnectionContext(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	_, _, err = conn.EnableUnitFilesContext(ctx, []string{name}, false, true)
	if err != nil {
		return fmt.Errorf("failed to enable service %s: %w", name, err)
	}
	return nil
}

func (s *ServicesInfoStruct) DisableService(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := dbus.NewSystemdConnectionContext(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.DisableUnitFilesContext(ctx, []string{name}, false)
	if err != nil {
		return fmt.Errorf("failed to disable service %s: %w", name, err)
	}
	return nil
}
