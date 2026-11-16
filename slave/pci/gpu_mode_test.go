package pci

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fakePCIDevice creates a device in a throwaway sysfs tree, bound to driver when not empty.
func fakePCIDevice(t *testing.T, address, class, vendor, driver string) PCIAddress {
	t.Helper()
	if !strings.HasPrefix(sysfsPCI, os.TempDir()) {
		original := sysfsPCI
		sysfsPCI = t.TempDir()
		t.Cleanup(func() { sysfsPCI = original })
	}
	dev, err := ParsePCIAddress(address)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(devicePath(dev), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"class": class, "vendor": vendor, "driver_override": "(null)"} {
		if err := os.WriteFile(devicePath(dev, name), []byte(value+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if driver != "" {
		driverDir := filepath.Join(sysfsPCI, "drivers", driver)
		if err := os.MkdirAll(driverDir, 0o755); err != nil {
			t.Fatal(err)
		}
		// Real sysfs has the file; os.WriteFile would create it unreadable here.
		if err := os.WriteFile(filepath.Join(driverDir, "unbind"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(driverDir, devicePath(dev, "driver")); err != nil {
			t.Fatal(err)
		}
	}
	return dev
}

func TestWantedDrivers(t *testing.T) {
	nvidia := fakePCIDevice(t, "0000:01:00.0", "0x030000", "0x10de", "")
	audio := fakePCIDevice(t, "0000:01:00.1", "0x040300", "0x10de", "")
	amd := fakePCIDevice(t, "0000:02:00.0", "0x030000", "0x1002", "")
	intel := fakePCIDevice(t, "0000:00:02.0", "0x030000", "0x8086", "")
	unknown := fakePCIDevice(t, "0000:03:00.0", "0x030000", "0x1a03", "")

	for _, tc := range []struct {
		dev  PCIAddress
		mode string
		want []string
	}{
		{nvidia, GPUModeHost, []string{"nvidia"}},
		{audio, GPUModeHost, []string{nativeDriver}},
		{amd, GPUModeHost, []string{"amdgpu"}},
		{intel, GPUModeHost, []string{"i915", "xe"}},
		{nvidia, GPUModeVM, []string{vfioDriver}},
		{audio, GPUModeVM, []string{vfioDriver}},
		{audio, GPUModeNone, []string{driverNone}},
		{unknown, GPUModeNone, []string{driverNone}},
	} {
		got, err := wantedDrivers(tc.dev, tc.mode)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("wantedDrivers(%s, %s) = %v, %v; want %v", tc.dev, tc.mode, got, err, tc.want)
		}
	}
	if _, err := wantedDrivers(unknown, GPUModeHost); err == nil {
		t.Error("host mode on an unsupported vendor must fail")
	}
}

func TestKeepsDriver(t *testing.T) {
	for _, tc := range []struct {
		current string
		wanted  []string
		want    bool
	}{
		{"vfio-pci", []string{vfioDriver}, true},
		{"nvidia", []string{vfioDriver}, false},
		{"xe", []string{"i915", "xe"}, true},
		{"snd_hda_intel", []string{nativeDriver}, true},
		{"vfio-pci", []string{nativeDriver}, false},
		{"", []string{nativeDriver}, false},
		{"nvidia", []string{driverNone}, false},
	} {
		if got := keepsDriver(tc.current, tc.wanted); got != tc.want {
			t.Errorf("keepsDriver(%q, %v) = %v", tc.current, tc.wanted, got)
		}
	}
}

// A GPU that is already on the wanted driver is pinned but never unbound: unbinding
// vfio-pci under a running VM would hang the slave.
func TestApplyDriverNeverUnbindsTheWantedDriver(t *testing.T) {
	for _, tc := range []struct {
		driver string
		wanted []string
		pin    string
	}{
		{"vfio-pci", []string{vfioDriver}, "vfio-pci\n"},
		{"nvidia", []string{"nvidia"}, "nvidia\n"},
		{"snd_hda_intel", []string{nativeDriver}, "\n"},
	} {
		dev := fakePCIDevice(t, "0000:01:00.0", "0x030000", "0x10de", tc.driver)
		// modprobe nvidia_uvm runs after a host bind; make it fail fast instead of loading.
		t.Setenv("PATH", t.TempDir())
		if err := applyDriver(context.Background(), dev, tc.wanted); err != nil {
			t.Fatalf("%s: %v", tc.driver, err)
		}
		if unbound, err := os.ReadFile(devicePath(dev, "driver", "unbind")); err != nil || len(unbound) != 0 {
			t.Errorf("%s: device was unbound (%q, %v)", tc.driver, unbound, err)
		}
		if pin, _ := os.ReadFile(devicePath(dev, "driver_override")); string(pin) != tc.pin {
			t.Errorf("%s: driver_override = %q, want %q", tc.driver, pin, tc.pin)
		}
		if err := os.RemoveAll(sysfsPCI); err != nil {
			t.Fatal(err)
		}
	}
}

func TestApplyDriverNoneUnbindsAndPins(t *testing.T) {
	dev := fakePCIDevice(t, "0000:01:00.0", "0x030000", "0x10de", "nvidia")
	// The fake driver link stays, so the bind check fails; the writes are what is tested.
	err := applyDriver(t.Context(), dev, []string{driverNone})
	if err == nil || !strings.Contains(err.Error(), `expected driver "none"`) {
		t.Fatalf("err = %v", err)
	}
	if unbound, _ := os.ReadFile(filepath.Join(sysfsPCI, "drivers", "nvidia", "unbind")); string(unbound) != dev.String() {
		t.Errorf("unbind = %q", unbound)
	}
	if pin, _ := os.ReadFile(devicePath(dev, "driver_override")); string(pin) != "none\n" {
		t.Errorf("driver_override = %q", pin)
	}
}

func TestEnsureGroupIsFreeReportsDriversLeft(t *testing.T) {
	gpu := fakePCIDevice(t, "0000:01:00.0", "0x030000", "0x1002", "amdgpu")
	audio := fakePCIDevice(t, "0000:01:00.1", "0x040300", "0x1002", "snd_hda_intel")
	group := []PCIAddress{gpu, audio}

	// Host mode on a GPU already in host mode leaves nothing.
	wanted := map[string][]string{gpu.String(): {"amdgpu"}, audio.String(): {nativeDriver}}
	if left, err := ensureGroupIsFree(t.Context(), gpu, group, wanted); err != nil || len(left) != 0 {
		t.Fatalf("host: left = %v, err = %v", left, err)
	}
	// None mode leaves both drivers; no process holds the fake devices.
	wanted = map[string][]string{gpu.String(): {driverNone}, audio.String(): {driverNone}}
	left, err := ensureGroupIsFree(t.Context(), gpu, group, wanted)
	if err != nil || !reflect.DeepEqual(left, []string{"amdgpu", "snd_hda_intel"}) {
		t.Fatalf("none: left = %v, err = %v", left, err)
	}
}

func TestDeviceHolders(t *testing.T) {
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()

	holders, err := deviceHolders([]string{os.DevNull, "/dev/does-not-exist"})
	if err != nil {
		t.Fatal(err)
	}
	self := fmt.Sprintf("%d(", os.Getpid())
	found := false
	for _, holder := range holders {
		found = found || strings.HasPrefix(holder, self)
	}
	if !found {
		t.Fatalf("this process holds %s but was not reported: %v", os.DevNull, holders)
	}

	if holders, err := deviceHolders([]string{"/dev/does-not-exist"}); err != nil || len(holders) != 0 {
		t.Fatalf("missing device: %v, %v", holders, err)
	}
}

func TestWithoutPersistenced(t *testing.T) {
	got := withoutPersistenced([]string{"812(nvidia-persiste)", "4410(python3)"})
	if !reflect.DeepEqual(got, []string{"4410(python3)"}) {
		t.Fatalf("got %v", got)
	}
}

func TestUnmanageHostdevs(t *testing.T) {
	const domain = `<domain type='kvm'>
  <devices>
    <hostdev mode='subsystem' type='pci' managed='yes'>
      <source>
        <address domain='0x0000' bus='0x01' slot='0x00' function='0x0'/>
      </source>
      <rom bar='off'/>
      <address type='pci' domain='0x0000' bus='0x05' slot='0x00' function='0x0'/>
    </hostdev>
    <hostdev mode='subsystem' type='pci' managed='yes'>
      <source>
        <address domain='0x0000' bus='0x09' slot='0x00' function='0x0'/>
      </source>
    </hostdev>
    <hostdev mode='subsystem' type='usb' managed='yes'>
      <source><vendor id='0x1234'/><product id='0xbeef'/></source>
    </hostdev>
  </devices>
</domain>`
	group := map[string]struct{}{"0000:01:00.0": {}, "0000:01:00.1": {}}

	updated, changed := unmanageHostdevs(domain, group)
	if !changed {
		t.Fatal("expected a change")
	}
	// Only the managed attribute of the GPU's hostdev differs.
	want := strings.Replace(domain, "managed='yes'", "managed='no'", 1)
	if updated != want {
		t.Fatalf("unexpected xml:\n%s", updated)
	}
	if again, changed := unmanageHostdevs(updated, group); changed || again != updated {
		t.Fatal("second pass must change nothing")
	}
}

func TestActualGPUMode(t *testing.T) {
	for driver, want := range map[string]string{
		"vfio-pci": GPUModeVM, "none": GPUModeNone, "": GPUModeNone, "nvidia": GPUModeHost, "amdgpu": GPUModeHost,
	} {
		if got := actualGPUMode(driver); got != want {
			t.Errorf("actualGPUMode(%q) = %s, want %s", driver, got, want)
		}
	}
}

func TestSetGPUModeRejectsBadInput(t *testing.T) {
	for _, tc := range []struct{ ref, mode, vm, want string }{
		{"0000:01:00.0", "passthrough", "", "invalid gpu mode"},
		{"0000:01:00.0", "vm", " ", "vm name is required"},
		{"not-a-gpu", "none", "", "invalid pci address"},
	} {
		err := SetGPUMode(t.Context(), tc.ref, tc.mode, tc.vm)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("SetGPUMode(%q, %q, %q) = %v, want %q", tc.ref, tc.mode, tc.vm, err, tc.want)
		}
	}
}
