package docker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"

	"github.com/docker/docker/api/types/image"
	cliTypes "github.com/docker/cli/cli/config/types"
)

type Image struct{}

func encodeEmptyRegistryAuth(registry string) (string, error) {
	ac := cliTypes.AuthConfig{
		Username:      "",
		Password:      "",
		ServerAddress: registry,
	}

	b, err := json.Marshal(ac)
	if err != nil {
		return "", err
	}

	return base64.URLEncoding.EncodeToString(b), nil
}

func (i *Image) Download(ctx context.Context, imageRef string, registry string) error {
	pullOpts := image.PullOptions{}

	if registry != "" {
		encoded, err := encodeEmptyRegistryAuth(registry)
		if err != nil {
			return err
		}
		pullOpts.RegistryAuth = encoded
	}

	rc, err := cli.ImagePull(ctx, imageRef, pullOpts)
	if err != nil {
		return err
	}
	defer rc.Close()

	_, err = io.Copy(io.Discard, rc)
	return err
}

func (*Image) Remove(ctx context.Context, imageID string, force, pruneChild bool) error {
	_, err := cli.ImageRemove(ctx, imageID, image.RemoveOptions{Force: force, PruneChildren: pruneChild})
	return err
}

func (*Image) List(ctx context.Context) ([]image.Summary, error) {
	// Lista todas as imagens (All: true == mostra também dangling, etc.)
	images, err := cli.ImageList(ctx, image.ListOptions{
		All: true,
	})
	if err != nil {
		return nil, err
	}
	return images, nil
}
