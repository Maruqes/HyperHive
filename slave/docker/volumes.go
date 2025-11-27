package docker

type Volume struct{}

func (*Volume) Create(url string) error {
	return nil
}

func (*Volume) Remove(url string) error {
	return nil
}
