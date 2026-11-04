package services

import (
	"context"
	"errors"
	"fmt"
	"math"

	"512SvMan/db"
)

const (
	networkLayoutMaxPieces = 5000
	networkLayoutMaxID     = 512
	networkLayoutMaxCoord  = 1e6
)

// ErrInvalidNetworkLayout marks a request the caller has to change.
var ErrInvalidNetworkLayout = errors.New("invalid network layout")

func (s *NetworkGraphService) SaveLayout(ctx context.Context, positions map[string]db.NetworkPosition) error {
	if len(positions) > networkLayoutMaxPieces {
		return fmt.Errorf("%w: more than %d pieces", ErrInvalidNetworkLayout, networkLayoutMaxPieces)
	}
	for id, position := range positions {
		if id == "" || len(id) > networkLayoutMaxID {
			return fmt.Errorf("%w: bad piece id", ErrInvalidNetworkLayout)
		}
		if math.Abs(position.X) > networkLayoutMaxCoord || math.Abs(position.Y) > networkLayoutMaxCoord {
			return fmt.Errorf("%w: position out of range", ErrInvalidNetworkLayout)
		}
	}
	return db.SaveNetworkLayout(ctx, positions)
}

func (s *NetworkGraphService) ResetLayout(ctx context.Context) error {
	return db.ResetNetworkLayout(ctx)
}
