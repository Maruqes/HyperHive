package docker

type Container struct{}

func (*Container) Create(url string) error {
	return nil
}

func (*Container) Remove(url string) error {
	return nil
}

func (*Container) List(url string) error {
	return nil
}

func (*Container) Stop(url string) error {
	return nil
}

func (*Container) Start(url string) error {
	return nil
}
