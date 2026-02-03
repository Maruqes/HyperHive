package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"slave/pci"
)

type appState struct {
	lastHostDevices []pci.HostPCIDevice
}

func main() {
	reader := bufio.NewReader(os.Stdin)
	state := &appState{}

	for {
		clearScreen()
		printMenu()

		choice, err := readLine(reader, "Choose an option: ")
		if err != nil {
			fmt.Printf("error reading input: %v\n", err)
			return
		}

		switch strings.TrimSpace(strings.ToLower(choice)) {
		case "1":
			listHostPCIs(state)
			waitEnter(reader)
		case "2":
			listHostPCIsWithIOMMU(state)
			waitEnter(reader)
		case "3":
			listHostGPUs()
			waitEnter(reader)
		case "4":
			listVMPCIs(reader)
			waitEnter(reader)
		case "5":
			attachPCIToVM(reader, state)
			waitEnter(reader)
		case "6":
			detachPCIFromVM(reader, state)
			waitEnter(reader)
		case "7":
			returnPCIToHost(reader, state)
			waitEnter(reader)
		case "8":
			parseAddress(reader)
			waitEnter(reader)
		case "q", "quit", "exit", "9":
			fmt.Println("Exiting.")
			return
		default:
			fmt.Println("Invalid option.")
			waitEnter(reader)
		}
	}
}

func printMenu() {
	fmt.Println("=== PCI Mini TUI (local test) ===")
	fmt.Println("Note: you usually need root/libvirt privileges for attach/detach.")
	fmt.Println()
	fmt.Println("1) List host PCI devices")
	fmt.Println("2) List host PCI devices with IOMMU")
	fmt.Println("3) List host GPUs")
	fmt.Println("4) List VM PCI devices")
	fmt.Println("5) Attach PCI to VM")
	fmt.Println("6) Detach PCI from VM (and return to host)")
	fmt.Println("7) Return PCI to host")
	fmt.Println("8) Normalize/validate PCI address")
	fmt.Println("9) Exit")
	fmt.Println()
}

func listHostPCIs(state *appState) {
	devs, err := pci.ListHostPCIDevices()
	if err != nil {
		fmt.Printf("error listing host PCI devices: %v\n", err)
		return
	}
	state.lastHostDevices = devs
	printHostDevices(devs)
}

func listHostPCIsWithIOMMU(state *appState) {
	devs, err := pci.ListHostPCIDevicesWithIOMMU()
	if err != nil {
		fmt.Printf("error listing host PCI devices with IOMMU: %v\n", err)
		return
	}
	state.lastHostDevices = devs
	printHostDevices(devs)
}

func listHostGPUs() {
	devs, err := pci.ListHostGPUs()
	if err != nil {
		fmt.Printf("error listing host GPUs: %v\n", err)
		return
	}
	printHostDevices(devs)
}

func listVMPCIs(reader *bufio.Reader) {
	vm, err := readLine(reader, "VM name: ")
	if err != nil {
		fmt.Printf("error reading VM name: %v\n", err)
		return
	}

	devs, err := pci.ListVMPCIDevices(vm)
	if err != nil {
		fmt.Printf("error listing PCI devices for VM %q: %v\n", vm, err)
		return
	}

	if len(devs) == 0 {
		fmt.Println("No PCI passthrough devices found for this VM.")
		return
	}

	fmt.Printf("\nPCI devices on VM %q:\n", vm)
	fmt.Printf("%-4s %-14s %-8s %-10s\n", "#", "BDF", "Managed", "Alias")
	for i, d := range devs {
		fmt.Printf("%-4d %-14s %-8v %-10s\n", i+1, d.Address, d.Managed, emptyToDash(d.Alias))
	}
}

func attachPCIToVM(reader *bufio.Reader, state *appState) {
	vm, err := readLine(reader, "VM name: ")
	if err != nil {
		fmt.Printf("error reading VM name: %v\n", err)
		return
	}
	ref, err := askPCIRef(reader, state.lastHostDevices)
	if err != nil {
		fmt.Printf("PCI address error: %v\n", err)
		return
	}

	if err := pci.AttachPCIToVM(vm, ref); err != nil {
		fmt.Printf("error attaching PCI %s to VM %s: %v\n", ref, vm, err)
		return
	}

	fmt.Printf("ok: PCI %s attached to VM %s\n", ref, vm)
}

func detachPCIFromVM(reader *bufio.Reader, state *appState) {
	vm, err := readLine(reader, "VM name: ")
	if err != nil {
		fmt.Printf("error reading VM name: %v\n", err)
		return
	}
	ref, err := askPCIRef(reader, state.lastHostDevices)
	if err != nil {
		fmt.Printf("PCI address error: %v\n", err)
		return
	}

	if err := pci.DetachPCIFromVM(vm, ref); err != nil {
		fmt.Printf("error detaching PCI %s from VM %s: %v\n", ref, vm, err)
		return
	}

	fmt.Printf("ok: PCI %s detached from VM %s and returned to host\n", ref, vm)
}

func returnPCIToHost(reader *bufio.Reader, state *appState) {
	ref, err := askPCIRef(reader, state.lastHostDevices)
	if err != nil {
		fmt.Printf("PCI address error: %v\n", err)
		return
	}

	if err := pci.ReturnPCIToHost(ref); err != nil {
		fmt.Printf("error returning PCI %s to host: %v\n", ref, err)
		return
	}

	fmt.Printf("ok: PCI %s returned to host\n", ref)
}

func parseAddress(reader *bufio.Reader) {
	raw, err := readLine(reader, "PCI address (e.g. 0000:65:00.0, 65:00.0, pci_0000_65_00_0): ")
	if err != nil {
		fmt.Printf("error reading PCI address: %v\n", err)
		return
	}

	addr, err := pci.ParsePCIAddress(raw)
	if err != nil {
		fmt.Printf("invalid: %v\n", err)
		return
	}

	fmt.Printf("normalized: %s\n", addr.String())
}

func printHostDevices(devs []pci.HostPCIDevice) {
	if len(devs) == 0 {
		fmt.Println("No PCI devices.")
		return
	}

	fmt.Printf("\n%-4s %-14s %-9s %-9s %-16s %-24s %-20s\n", "#", "BDF", "GPU", "Driver", "VendorID:ProdID", "Vendor/Product", "AttachedVMs")
	for i, d := range devs {
		ids := fmt.Sprintf("%s:%s", emptyToDash(d.VendorID), emptyToDash(d.ProductID))
		label := strings.TrimSpace(strings.TrimSpace(d.Vendor) + " " + strings.TrimSpace(d.Product))
		if label == "" {
			label = "-"
		}

		attached := "-"
		if len(d.AttachedToVMs) > 0 {
			attached = strings.Join(d.AttachedToVMs, ",")
		}

		fmt.Printf(
			"%-4d %-14s %-9v %-9s %-16s %-24s %-20s\n",
			i+1,
			d.Address,
			d.IsGPU,
			emptyToDash(d.Driver),
			ids,
			label,
			attached,
		)
	}
	fmt.Println()
	fmt.Println("Tip: in attach/detach/return options you can use the # index from this table.")
}

func askPCIRef(reader *bufio.Reader, last []pci.HostPCIDevice) (string, error) {
	prompt := "PCI (BDF or # from last list): "
	if len(last) == 0 {
		prompt = "PCI (e.g. 0000:65:00.0): "
	}
	raw, err := readLine(reader, prompt)
	if err != nil {
		return "", err
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty PCI address")
	}

	if idx, convErr := strconv.Atoi(raw); convErr == nil {
		if len(last) == 0 {
			return "", fmt.Errorf("no previous listing in memory to use index")
		}
		if idx < 1 || idx > len(last) {
			return "", fmt.Errorf("index out of range (1..%d)", len(last))
		}
		return last[idx-1].Address, nil
	}

	addr, err := pci.ParsePCIAddress(raw)
	if err != nil {
		return "", err
	}
	return addr.String(), nil
}

func readLine(reader *bufio.Reader, prompt string) (string, error) {
	fmt.Print(prompt)
	text, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(text), nil
}

func waitEnter(reader *bufio.Reader) {
	fmt.Print("\nPress ENTER to continue...")
	_, _ = reader.ReadString('\n')
}

func clearScreen() {
	fmt.Print("\033[H\033[2J")
}

func emptyToDash(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "-"
	}
	return s
}
