package tun

import (
	"fmt"
	"os/exec"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	TUN_IOCTL_GET_MAC               = 0x270014
	TUN_IOCTL_GET_VERSION           = 0x270018
	TUN_IOCTL_GET_MTU               = 0x27001C
	TUN_IOCTL_SET_MEDIA_STATUS      = 0x270020
	TUN_IOCTL_CONFIG_TUN            = 0x270024
	FILE_DEVICE_UNKNOWN             = 0x00000022
	METHOD_BUFFERED                 = 0
	FILE_ANY_ACCESS                 = 0
)

type TunInterface struct {
	handle   windows.Handle
	name     string
	mtu      int
	readBuf  []byte
	writeBuf []byte
}

func CreateTunInterface(name string) (*TunInterface, error) {
	// Try to create/open WinTun adapter
	devicePath := fmt.Sprintf("\\\\.\\Global\\WINTUN%s", name)
	
	handle, err := windows.CreateFile(
		windows.StringToUTF16Ptr(devicePath),
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_SYSTEM|windows.FILE_FLAG_OVERLAPPED,
		0,
	)
	
	if err != nil {
		// Try alternative method using TAP-Windows adapter
		return createTapInterface(name)
	}

	tun := &TunInterface{
		handle:   handle,
		name:     name,
		mtu:      1500,
		readBuf:  make([]byte, 2000),
		writeBuf: make([]byte, 2000),
	}

	return tun, nil
}

func createTapInterface(name string) (*TunInterface, error) {
	// Fallback to TAP-Windows adapter
	devicePath := "\\\\.\\Global\\tapvpn"
	
	handle, err := windows.CreateFile(
		windows.StringToUTF16Ptr(devicePath),
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_SYSTEM,
		0,
	)
	
	if err != nil {
		return nil, fmt.Errorf("failed to open TAP adapter: %v", err)
	}

	tun := &TunInterface{
		handle:   handle,
		name:     name,
		mtu:      1500,
		readBuf:  make([]byte, 2000),
		writeBuf: make([]byte, 2000),
	}

	// Set interface up
	if err := tun.setInterfaceUp(); err != nil {
		tun.Close()
		return nil, err
	}

	return tun, nil
}

func (t *TunInterface) Read(buf []byte) (int, error) {
	var bytesRead uint32
	
	err := windows.ReadFile(t.handle, buf, &bytesRead, nil)
	if err != nil {
		return 0, err
	}
	
	return int(bytesRead), nil
}

func (t *TunInterface) Write(buf []byte) (int, error) {
	var bytesWritten uint32
	
	err := windows.WriteFile(t.handle, buf, &bytesWritten, nil)
	if err != nil {
		return 0, err
	}
	
	return int(bytesWritten), nil
}

func (t *TunInterface) Close() error {
	if t.handle != windows.InvalidHandle {
		return windows.CloseHandle(t.handle)
	}
	return nil
}

func (t *TunInterface) SetIP(ip, netmask string) error {
	// Use netsh to configure IP address
	cmd := exec.Command("netsh", "interface", "ip", "set", "address", 
		fmt.Sprintf("name=%s", t.name), "static", ip, netmask)
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to set IP: %v, output: %s", err, output)
	}
	
	return nil
}

func (t *TunInterface) AddRoute(dest, gateway string) error {
	cmd := exec.Command("route", "add", dest, gateway)
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to add route: %v, output: %s", err, output)
	}
	
	return nil
}

func (t *TunInterface) setInterfaceUp() error {
	// Set TAP adapter to connected state
	status := uint32(1) // Connected
	var bytesReturned uint32
	
	err := windows.DeviceIoControl(
		t.handle,
		TUN_IOCTL_SET_MEDIA_STATUS,
		(*byte)(unsafe.Pointer(&status)),
		4,
		nil,
		0,
		&bytesReturned,
		nil,
	)
	
	return err
}

func GetAvailableAdapters() ([]string, error) {
	// List available network adapters
	cmd := exec.Command("netsh", "interface", "show", "interface")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	
	lines := strings.Split(string(output), "\n")
	var adapters []string
	
	for _, line := range lines {
		if strings.Contains(line, "TAP") || strings.Contains(line, "TUN") {
			parts := strings.Fields(line)
			if len(parts) > 3 {
				adapters = append(adapters, parts[3])
			}
		}
	}
	
	return adapters, nil
}
