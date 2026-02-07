package main

import (
	"fmt"
	"os"
	"slave/virsh"
)

func main() {
	sockets, err := virsh.GetCPUSockets()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	virsh.PrintCPUSockets(sockets)
}
