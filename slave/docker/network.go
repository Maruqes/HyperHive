package docker

type Network struct{}

func (*Network) Create(url string) error {
	return nil
}

func (*Network) Remove(url string) error {
	return nil
}

}}

