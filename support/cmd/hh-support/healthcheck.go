package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

func healthcheck(address string) error {
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	response, err := client.Get("http://" + address + "/healthz")
	if err != nil {
		return errors.New("support healthcheck could not reach the service")
	}
	defer response.Body.Close()
	var result struct {
		Status string `json:"status"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 1024)).Decode(&result) != nil || result.Status != "ok" {
		return errors.New("support healthcheck failed")
	}
	return nil
}
