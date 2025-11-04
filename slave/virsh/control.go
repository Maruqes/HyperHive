package virsh

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	libvirt "libvirt.org/go/libvirt"
)

func ShutdownVM(name string) error {
	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(name)
	if err != nil {
		return fmt.Errorf("lookup: %w", err)
	}
	defer dom.Free()

	// If paused, resume so the guest can actually react to shutdown.
	if st, _, _ := dom.GetState(); st == libvirt.DOMAIN_PAUSED {
		_ = dom.Resume()
	}

	// 1) Try guest agent (best; clean)
	// Requires qemu-guest-agent running inside the VM and a <channel> in XML.
	if err := dom.ShutdownFlags(libvirt.DOMAIN_SHUTDOWN_GUEST_AGENT); err != nil {
		// 2) Fallback to ACPI power button (what virsh shutdown does by default)
		_ = dom.ShutdownFlags(libvirt.DOMAIN_SHUTDOWN_ACPI_POWER_BTN)
	}

	// 3) Wait/poll until the VM actually turns off
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		st, _, err := dom.GetState()
		if err != nil {
			return fmt.Errorf("get state: %w", err)
		}
		if st == libvirt.DOMAIN_SHUTOFF {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}

	// 4) Still up? Return a clear error (your caller may choose to Destroy() then).
	return fmt.Errorf("graceful shutdown timed out (agent/ACPI ignored)")
}

func ForceShutdownVM(name string) error {
	connURI := "qemu:///system"
	conn, err := libvirt.NewConnect(connURI)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(name)
	if err != nil {
		return fmt.Errorf("lookup: %w", err)
	}
	defer dom.Free()

	if err := dom.Destroy(); err != nil {
		return fmt.Errorf("force shutdown: %w", err)
	}
	return nil
}

func StartVM(name string) error {
	connURI := "qemu:///system"
	conn, err := libvirt.NewConnect(connURI)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(name)
	if err != nil {
		return fmt.Errorf("lookup: %w", err)
	}
	defer dom.Free()

	if err := dom.Create(); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	return nil
}

func DestroyUndefineVM(name string) error {
	connURI := "qemu:///system"
	conn, err := libvirt.NewConnect(connURI)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(name)
	if err != nil {
		return fmt.Errorf("lookup: %w", err)
	}
	defer dom.Free()

	// Get state
	state, _, err := dom.GetState()
	if err != nil {
		return fmt.Errorf("state: %w", err)
	}

	if state == libvirt.DOMAIN_RUNNING || state == libvirt.DOMAIN_PAUSED || state == libvirt.DOMAIN_BLOCKED {
		if err := dom.Destroy(); err != nil {
			return fmt.Errorf("force shutdown: %w", err)
		}
	}

	//force remove
	if err := dom.UndefineFlags(libvirt.DOMAIN_UNDEFINE_MANAGED_SAVE | libvirt.DOMAIN_UNDEFINE_SNAPSHOTS_METADATA | libvirt.DOMAIN_UNDEFINE_NVRAM); err != nil {
		return fmt.Errorf("undefine: %w", err)
	}
	return nil
}

func RemoveVM(name string) error {
	connURI := "qemu:///system"
	conn, err := libvirt.NewConnect(connURI)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(name)
	if err != nil {
		return fmt.Errorf("lookup: %w", err)
	}
	defer dom.Free()

	xmlDesc, err := dom.GetXMLDesc(0)
	if err != nil {
		return fmt.Errorf("xml: %w", err)
	}

	diskPath, err := diskPathFromDomainXML(xmlDesc)
	if err != nil {
		return fmt.Errorf("detect disk path: %w", err)
	}

	// Destroy and undefine
	err = DestroyUndefineVM(name)
	if err != nil {
		return fmt.Errorf("destroy and undefine: %w", err)
	}

	if diskPath != "" {
		if err := os.Remove(diskPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove disk %s: %w", diskPath, err)
		}

		xmlDir := filepath.Dir(diskPath)
		if xmlDir == "" {
			return fmt.Errorf("cannot determine directory for disk path: %s", diskPath)
		}
		xmlPath := filepath.Join(xmlDir, name+".xml")
		if err := os.Remove(xmlPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove xml %s: %w", xmlPath, err)
		}

		// Remove the main folder
		if err := os.RemoveAll(xmlDir); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove directory %s: %w", xmlDir, err)
		}
	} else {
		return fmt.Errorf("err getting diskPath RemoveVm Slave")
	}

	return nil
}

func UndefineVm(name string) error {
	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(name)
	if err != nil {
		return fmt.Errorf("lookup: %w", err)
	}
	defer dom.Free()

	state, _, err := dom.GetState()
	if err != nil {
		return fmt.Errorf("state: %w", err)
	}

	if state == libvirt.DOMAIN_RUNNING || state == libvirt.DOMAIN_PAUSED || state == libvirt.DOMAIN_BLOCKED {
		if err := dom.Destroy(); err != nil {
			return fmt.Errorf("destroy: %w", err)
		}
	}

	// Undefine without flags so storage/disk is not removed
	if err := dom.UndefineFlags(0); err != nil {
		return fmt.Errorf("undefine: %w", err)
	}

	return nil
}

func RestartVM(name string) error {
	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(name)
	if err != nil {
		return fmt.Errorf("lookup: %w", err)
	}
	defer dom.Free()

	// Force stop
	if err := dom.Destroy(); err != nil {
		return fmt.Errorf("destroy: %w", err)
	}

	// Wait until the VM is fully shut off
	for {
		state, _, err := dom.GetState()
		if err != nil {
			return fmt.Errorf("get state: %w", err)
		}
		if state == libvirt.DOMAIN_SHUTOFF {
			break
		}
		// short non-blocking poll (100ms is fine)
		time.Sleep(100 * time.Millisecond)
	}

	// Restart
	if err := dom.Create(); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	return nil
}

func PauseVM(name string) error {
	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(name)
	if err != nil {
		return fmt.Errorf("lookup: %w", err)
	}
	defer dom.Free()

	state, _, err := dom.GetState()
	if err != nil {
		return fmt.Errorf("get state: %w", err)
	}
	if state != libvirt.DOMAIN_RUNNING {
		stateLabel := domainStateToString(state).String()
		return fmt.Errorf("vm %s must be running to pause (state %s)", name, stateLabel)
	}

	if err := dom.Suspend(); err != nil {
		return fmt.Errorf("pause: %w", err)
	}
	return nil
}

func ResumeVM(name string) error {
	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(name)
	if err != nil {
		return fmt.Errorf("lookup: %w", err)
	}
	defer dom.Free()

	state, _, err := dom.GetState()
	if err != nil {
		return fmt.Errorf("get state: %w", err)
	}
	if state != libvirt.DOMAIN_PAUSED {
		stateLabel := domainStateToString(state).String()
		return fmt.Errorf("vm %s must be paused to resume (state %s)", name, stateLabel)
	}

	if err := dom.Resume(); err != nil {
		return fmt.Errorf("resume: %w", err)
	}
	return nil
}
