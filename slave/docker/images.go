package docker

type Image struct{}

func (*Image) Download(image string) error {
	return nil
}

func (*Image) Remove(image string) error {
	return nil
}

func (*Image) List() error {
	return nil
}
