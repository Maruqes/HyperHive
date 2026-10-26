package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

func StrictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("only one JSON value is allowed")
	}
	return nil
}
func EncodeJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}
func StreamMap(streams []Stream) map[string]Stream {
	result := map[string]Stream{}
	for _, stream := range streams {
		result[stream.Name] = stream
	}
	return result
}
