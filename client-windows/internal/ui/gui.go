package ui

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"

	"yagnoetik-vpn-client/internal/client"
)

// mustUTF16Ptr converts a Go string to UTF-16 and returns a pointer to it
func mustUTF16Ptr(s string) *uint16 {
	ptr, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		panic(err)
	}
	return ptr
}

const (
	WS_OVERLAPPEDWINDOW = 0x00CF0000
	WS_VISIBLE          = 0x10000000
	SW_SHOW             = 5
	IDC_ARROW           = 32512
	WHITE_BRUSH         = 0
	WM_DESTROY          = 2
	WM_COMMAND          = 273
	WM_PAINT           = 15
	WM_CLOSE           = 16
	BS_PUSHBUTTON      = 0
	SS_LEFT            = 0
)

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	gdi32                = syscall.NewLazyDLL("gdi32.dll")
	registerClassW       = user32.NewProc("RegisterClassW")
	createWindowExW      = user32.NewProc("CreateWindowExW")
	defWindowProcW       = user32.NewProc("DefWindowProcW")
	getMessageW          = user32.NewProc("GetMessageW")
	translateMessage     = user32.NewProc("TranslateMessage")
	dispatchMessageW     = user32.NewProc("DispatchMessageW")
	postQuitMessage      = user32.NewProc("PostQuitMessage")
	showWindow           = user32.NewProc("ShowWindow")
	updateWindow         = user32.NewProc("UpdateWindow")
	getModuleHandleW     = kernel32.NewProc("GetModuleHandleW")
	loadCursorW          = user32.NewProc("LoadCursorW")
	getStockObject       = gdi32.NewProc("GetStockObject")
	setWindowTextW       = user32.NewProc("SetWindowTextW")
	getWindowTextW       = user32.NewProc("GetWindowTextW")
	messageBoxW          = user32.NewProc("MessageBoxW")
)

type WNDCLASSEX struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     syscall.Handle
	HIcon         syscall.Handle
	HCursor       syscall.Handle
	HbrBackground syscall.Handle
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       syscall.Handle
}

type MSG struct {
	Hwnd    syscall.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

type GUI struct {
	vpnClient      *client.VPNClient
	hwnd           syscall.Handle
	connectBtn     syscall.Handle
	statusLabel    syscall.Handle
	statsLabel     syscall.Handle
	serverAddrEdit syscall.Handle
}

func NewGUI(vpnClient *client.VPNClient) *GUI {
	return &GUI{
		vpnClient: vpnClient,
	}
}

func (g *GUI) Run() error {
	hInstance, err := syscall.LoadLibrary("kernel32.dll")
	if err != nil {
		return fmt.Errorf("LoadLibrary failed: %v", err)
	}
	defer syscall.FreeLibrary(hInstance)

	className := mustUTF16Ptr("YagnoetikVPN")
	windowName := mustUTF16Ptr("Yagnoetik VPN Client")

	wndClass := WNDCLASSEX{
		CbSize:        uint32(unsafe.Sizeof(WNDCLASSEX{})),
		Style:         0,
		LpfnWndProc:   syscall.NewCallback(g.wndProc),
		CbClsExtra:    0,
		CbWndExtra:    0,
		HInstance:     syscall.Handle(hInstance),
		HIcon:         0,
		HCursor:       g.loadCursor(IDC_ARROW),
		HbrBackground: g.getStockObject(WHITE_BRUSH),
		LpszMenuName:  nil,
		LpszClassName: className,
		HIconSm:       0,
	}

	registerClassW.Call(uintptr(unsafe.Pointer(&wndClass)))

	hwnd, _, _ := createWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		WS_OVERLAPPEDWINDOW|WS_VISIBLE,
		100, 100, 400, 300,
		0, 0,
		uintptr(hInstance),
		uintptr(unsafe.Pointer(g)),
	)

	g.hwnd = syscall.Handle(hwnd)
	g.createControls()

	showWindow.Call(hwnd, SW_SHOW)
	updateWindow.Call(hwnd)

	var msg MSG
	for {
		ret, _, _ := getMessageW.Call(
			uintptr(unsafe.Pointer(&msg)),
			0, 0, 0,
		)
		if ret == 0 || ret == 0xFFFFFFFF {
			break
		}

		translateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		dispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}

	return nil
}

func (g *GUI) createControls() {
	hInstance, _, _ := getModuleHandleW.Call(0)

	// Create status label
	statusText := mustUTF16Ptr("Status: Disconnected")
	hwndStatus, _, _ := createWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(mustUTF16Ptr("STATIC"))),
		uintptr(unsafe.Pointer(statusText)),
		WS_VISIBLE|SS_LEFT,
		20, 20, 300, 20,
		uintptr(g.hwnd), 0, uintptr(hInstance), 0,
	)
	g.statusLabel = syscall.Handle(hwndStatus)

	// Server address input
	serverText := mustUTF16Ptr("your-server.com")
	hwndEdit, _, _ := createWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(mustUTF16Ptr("EDIT"))),
		uintptr(unsafe.Pointer(serverText)),
		WS_VISIBLE|0x80, // WS_BORDER
		20, 50, 200, 25,
		uintptr(g.hwnd), 0, uintptr(hInstance), 0,
	)
	g.serverAddrEdit = syscall.Handle(hwndEdit)

	// Connect button
	connectText := mustUTF16Ptr("Connect")
	hwndBtn, _, _ := createWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(mustUTF16Ptr("BUTTON"))),
		uintptr(unsafe.Pointer(connectText)),
		WS_VISIBLE|BS_PUSHBUTTON,
		240, 50, 80, 25,
		uintptr(g.hwnd), 1001, uintptr(hInstance), 0,
	)
	g.connectBtn = syscall.Handle(hwndBtn)

	// Stats label
	statsText := mustUTF16Ptr("Up: 0 KB | Down: 0 KB")
	hwndStats, _, _ := createWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(mustUTF16Ptr("STATIC"))),
		uintptr(unsafe.Pointer(statsText)),
		WS_VISIBLE|SS_LEFT,
		20, 90, 300, 20,
		uintptr(g.hwnd), 0, uintptr(hInstance), 0,
	)
	g.statsLabel = syscall.Handle(hwndStats)
}

func (g *GUI) wndProc(hwnd syscall.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		if wParam == 1001 { // Connect button
			g.handleConnect()
		}
	case WM_CLOSE:
		if g.vpnClient.IsConnected() {
			g.vpnClient.Disconnect()
		}
		postQuitMessage.Call(0)
	case WM_DESTROY:
		postQuitMessage.Call(0)
	default:
		ret, _, _ := defWindowProcW.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
		return ret
	}
	return 0
}

func (g *GUI) handleConnect() {
	if g.vpnClient.IsConnected() {
		// Disconnect
		err := g.vpnClient.Disconnect()
		if err != nil {
			g.showMessage("Error", fmt.Sprintf("Failed to disconnect: %v", err))
			return
		}
		g.updateStatus("Disconnected")
		g.setButtonText("Connect")
	} else {
		// Connect
		serverAddr := g.getEditText(g.serverAddrEdit)
		if serverAddr == "" {
			g.showMessage("Error", "Please enter server address")
			return
		}

		// Update config with server address
		config := &client.Config{
			ServerAddr: serverAddr,
			UUID:       "your-uuid-here",
			Secret:     "your-secret-here",
			Key:        make([]byte, 32), // Should be loaded from config
		}

		newClient, err := client.NewVPNClient(config)
		if err != nil {
			g.showMessage("Error", fmt.Sprintf("Failed to create client: %v", err))
			return
		}

		g.vpnClient = newClient
		err = g.vpnClient.Connect()
		if err != nil {
			g.showMessage("Error", fmt.Sprintf("Failed to connect: %v", err))
			return
		}

		g.updateStatus("Connected")
		g.setButtonText("Disconnect")
		go g.updateStats()
	}
}

func (g *GUI) updateStatus(status string) {
	text := fmt.Sprintf("Status: %s", status)
	setWindowTextW.Call(uintptr(g.statusLabel), uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(text))))
}

func (g *GUI) setButtonText(text string) {
	setWindowTextW.Call(uintptr(g.connectBtn), uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(text))))
}

func (g *GUI) getEditText(hwnd syscall.Handle) string {
	buf := make([]uint16, 256)
	getWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), 256)
	return syscall.UTF16ToString(buf)
}

func (g *GUI) showMessage(title, message string) {
	messageBoxW.Call(
		uintptr(g.hwnd),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(message))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(title))),
		0,
	)
}

func (g *GUI) updateStats() {
	for g.vpnClient.IsConnected() {
		up, down := g.vpnClient.GetStats()
		text := fmt.Sprintf("Up: %d KB | Down: %d KB", up/1024, down/1024)
		setWindowTextW.Call(uintptr(g.statsLabel), uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(text))))
		
		// Sleep for a short time to prevent high CPU usage
		time.Sleep(100 * time.Millisecond)
	}
}

func (g *GUI) loadCursor(id int) syscall.Handle {
	ret, _, _ := loadCursorW.Call(0, uintptr(id))
	return syscall.Handle(ret)
}

func (g *GUI) getStockObject(id int) syscall.Handle {
	ret, _, _ := getStockObject.Call(uintptr(id))
	return syscall.Handle(ret)
}
