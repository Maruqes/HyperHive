package info

type NetworkInfoStruct struct{}

var NetworkInfo NetworkInfoStruct

func (n *NetworkInfoStruct) GetInterfaces() ([]string, error) {
	return []string{}, nil
}

func (n *NetworkInfoStruct) GetInterfaceStats() (map[any]any, error) {
	return nil, nil
}

type ProcessInfoStruct struct{}

var ProcessInfo ProcessInfoStruct

func (p *ProcessInfoStruct) GetProcesses() ([]map[string]any, error) {
	return []map[string]any{}, nil
}

//info de processos memoria cpu ram etc etc matar procesos


