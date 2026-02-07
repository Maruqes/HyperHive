package main

import (
	"fmt"
	"os"
	"strings"

	"slave/virsh"
)

func main() {
	sockets, err := virsh.GetCPUSockets()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting CPU sockets: %v\n", err)
		os.Exit(1)
	}

	// Print full topology
	virsh.PrintCPUSockets(sockets)

	// Show physical cores per socket
	for _, s := range sockets {
		physCores := virsh.GetPhysicalCores(s)
		fmt.Printf("\nSocket %d: %d physical cores\n", s.SocketID, len(physCores))
		for _, core := range physCores {
			sibStrs := make([]string, len(core.Siblings))
			for i, sib := range core.Siblings {
				sibStrs[i] = fmt.Sprintf("%d", sib)
			}
			fmt.Printf("  Core %d (CPU %d) → siblings: %s\n", core.CoreIndex, core.PhysicalID, fmt.Sprintf("[%s]", joinInts(sibStrs)))
		}
	}

	// Example: validate a pinning config (cores 0-3, with HT, socket 0)
	if len(sockets) > 0 {
		physCores := virsh.GetPhysicalCores(sockets[0])
		maxCore := len(physCores) - 1
		endRange := 3
		if endRange > maxCore {
			endRange = maxCore
		}

		config := virsh.CPUPinningConfig{
			RangeStart:     0,
			RangeEnd:       endRange,
			HyperThreading: true,
			SocketID:       sockets[0].SocketID,
		}

		fmt.Printf("\n--- Test pinning config: cores %d-%d, HT=%v, socket=%d ---\n",
			config.RangeStart, config.RangeEnd, config.HyperThreading, config.SocketID)

		if err := virsh.ValidateCPUPinningConfig(config, sockets); err != nil {
			fmt.Printf("Validation failed: %v\n", err)
		} else {
			fmt.Println("Validation passed!")
		}

		pins, err := virsh.BuildVCPUPins(config, sockets)
		if err != nil {
			fmt.Printf("Build pins failed: %v\n", err)
		} else {
			fmt.Println("vCPU pin mappings:")
			for _, pin := range pins {
				fmt.Printf("  vCPU %d → cpuset %s\n", pin.VCPU, pin.CPUSet)
			}
			fmt.Println("\n--- Generated <cputune> XML ---")
			fmt.Println(virsh.BuildCPUTuneXML(pins, "  "))
		}
	}

	// Show examples with simulated topologies (HT system)
	printExampleCPUTuneXML()
}

func printExampleCPUTuneXML() {
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("EXAMPLE CPU PINNING XML (simulated 28-core HT system)")
	fmt.Println(strings.Repeat("=", 60))

	// Simulate a 28-core HT system (cores 0-27, HT siblings 28-55)
	var cpus []virsh.CPUInfo
	for i := 0; i < 28; i++ {
		cpus = append(cpus, virsh.CPUInfo{
			CPUID:    i,
			Siblings: []int{i, i + 28},
		})
	}
	for i := 28; i < 56; i++ {
		cpus = append(cpus, virsh.CPUInfo{
			CPUID:    i,
			Siblings: []int{i - 28, i},
		})
	}
	simSocket := virsh.CPUSocket{SocketID: 0, CPUs: cpus}
	simSockets := []virsh.CPUSocket{simSocket}

	// Example 1: 4 cores (0-3) com HT
	fmt.Println("\n--- Example 1: cores 0-3, HT=true ---")
	config1 := virsh.CPUPinningConfig{RangeStart: 0, RangeEnd: 3, HyperThreading: true, SocketID: 0}
	fmt.Println("    vcpu count:", virsh.VCPUCount(config1))
	xml1, err := virsh.BuildFullPinningXML(config1, simSockets, "  ")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Println(xml1)
	}

	// Example 2: 4 cores (0-3) sem HT
	fmt.Println("\n--- Example 2: cores 0-3, HT=false ---")
	config2 := virsh.CPUPinningConfig{RangeStart: 0, RangeEnd: 3, HyperThreading: false, SocketID: 0}
	fmt.Println("    vcpu count:", virsh.VCPUCount(config2))
	xml2, err := virsh.BuildFullPinningXML(config2, simSockets, "  ")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Println(xml2)
	}

	// Example 3: 8 cores (4-11) com HT
	fmt.Println("\n--- Example 3: cores 4-11, HT=true ---")
	config3 := virsh.CPUPinningConfig{RangeStart: 4, RangeEnd: 11, HyperThreading: true, SocketID: 0}
	fmt.Println("    vcpu count:", virsh.VCPUCount(config3))
	xml3, err := virsh.BuildFullPinningXML(config3, simSockets, "  ")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Println(xml3)
	}

	// Example 4: 12 cores (0-11) com HT
	fmt.Println("\n--- Example 4: cores 0-11, HT=true ---")
	config4 := virsh.CPUPinningConfig{RangeStart: 0, RangeEnd: 11, HyperThreading: true, SocketID: 0}
	fmt.Println("    vcpu count:", virsh.VCPUCount(config4))
	xml4, err := virsh.BuildFullPinningXML(config4, simSockets, "  ")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Println(xml4)
	}

	// Example 5: All 28 cores com HT
	fmt.Println("\n--- Example 5: all 28 cores (0-27), HT=true ---")
	config5 := virsh.CPUPinningConfig{RangeStart: 0, RangeEnd: 27, HyperThreading: true, SocketID: 0}
	fmt.Println("    vcpu count:", virsh.VCPUCount(config5))
	xml5, err := virsh.BuildFullPinningXML(config5, simSockets, "  ")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Println(xml5)
	}
}

func joinInts(s []string) string {
	result := ""
	for i, v := range s {
		if i > 0 {
			result += ", "
		}
		result += v
	}
	return result
}
