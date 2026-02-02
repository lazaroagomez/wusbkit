package usb

import (
	"regexp"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	cfgmgr32                  = windows.NewLazySystemDLL("cfgmgr32.dll")
	procCMLocateDevNodeW      = cfgmgr32.NewProc("CM_Locate_DevNodeW")
	procCMGetDevNodePropertyW = cfgmgr32.NewProc("CM_Get_DevNode_PropertyW")
	procCMGetParent           = cfgmgr32.NewProc("CM_Get_Parent")
	procCMGetDeviceIDW        = cfgmgr32.NewProc("CM_Get_Device_IDW")
)

// DEVPROPKEY structure for Windows device properties
type DEVPROPKEY struct {
	FmtID windows.GUID
	PID   uint32
}

// Device property keys
var (
	// DEVPKEY_Device_LocationInfo: {a45c254e-df1c-4efd-8020-67d146a850e0}, 15
	DEVPKEY_Device_LocationInfo = DEVPROPKEY{
		FmtID: windows.GUID{
			Data1: 0xa45c254e,
			Data2: 0xdf1c,
			Data3: 0x4efd,
			Data4: [8]byte{0x80, 0x20, 0x67, 0xd1, 0x46, 0xa8, 0x50, 0xe0},
		},
		PID: 15,
	}

	// DEVPKEY_Device_Parent: {4340a6c5-93fa-4706-972c-7b648008a5a7}, 8
	DEVPKEY_Device_Parent = DEVPROPKEY{
		FmtID: windows.GUID{
			Data1: 0x4340a6c5,
			Data2: 0x93fa,
			Data3: 0x4706,
			Data4: [8]byte{0x97, 0x2c, 0x7b, 0x64, 0x80, 0x08, 0xa5, 0xa7},
		},
		PID: 8,
	}
)

// Configuration Manager return codes
const (
	CR_SUCCESS               = 0
	CR_BUFFER_SMALL          = 26
	CM_LOCATE_DEVNODE_NORMAL = 0
)

// Device property types
const (
	DEVPROP_TYPE_STRING = 18
)

// portRegex matches port patterns in LocationInfo
var portRegex = regexp.MustCompile(`Port[_# ]*(\d+)`)

// GetHubPortLocation retrieves USB hub port location by walking up the device tree.
// It returns the first LocationInfo containing "Port" and the parent hub's instance ID.
// Returns empty strings if not found (e.g., device directly connected without hub).
func GetHubPortLocation(pnpDeviceID string) (locationInfo, parentInstanceId string, err error) {
	if pnpDeviceID == "" {
		return "", "", nil
	}

	// Convert PNPDeviceID to UTF-16
	deviceID, err := syscall.UTF16PtrFromString(pnpDeviceID)
	if err != nil {
		return "", "", err
	}

	// Locate the device node
	var devInst uint32
	ret, _, _ := procCMLocateDevNodeW.Call(
		uintptr(unsafe.Pointer(&devInst)),
		uintptr(unsafe.Pointer(deviceID)),
		CM_LOCATE_DEVNODE_NORMAL,
	)
	if ret != CR_SUCCESS {
		return "", "", nil // Device not found, return empty
	}

	// Walk up the device tree (max 10 levels)
	currentDevInst := devInst
	for i := 0; i < 10; i++ {
		// Get LocationInfo for current device
		locInfo := getDevicePropertyString(currentDevInst, DEVPKEY_Device_LocationInfo)

		// Check if LocationInfo contains "Port"
		if locInfo != "" && strings.Contains(strings.ToLower(locInfo), "port") {
			// Found port info, now get the parent instance ID
			parentID := getDevicePropertyString(currentDevInst, DEVPKEY_Device_Parent)
			return locInfo, parentID, nil
		}

		// Move to parent device
		var parentDevInst uint32
		ret, _, _ = procCMGetParent.Call(
			uintptr(unsafe.Pointer(&parentDevInst)),
			uintptr(currentDevInst),
			0,
		)
		if ret != CR_SUCCESS {
			break // No more parents
		}
		currentDevInst = parentDevInst
	}

	return "", "", nil // Not found
}

// getDevicePropertyString retrieves a string property from a device node
func getDevicePropertyString(devInst uint32, propKey DEVPROPKEY) string {
	// First call to get required buffer size
	var propType uint32
	var bufferSize uint32 = 0

	procCMGetDevNodePropertyW.Call(
		uintptr(devInst),
		uintptr(unsafe.Pointer(&propKey)),
		uintptr(unsafe.Pointer(&propType)),
		0,
		uintptr(unsafe.Pointer(&bufferSize)),
		0,
	)

	if bufferSize == 0 {
		return ""
	}

	// Allocate buffer and get property
	buffer := make([]uint16, bufferSize/2+1)
	ret, _, _ := procCMGetDevNodePropertyW.Call(
		uintptr(devInst),
		uintptr(unsafe.Pointer(&propKey)),
		uintptr(unsafe.Pointer(&propType)),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(unsafe.Pointer(&bufferSize)),
		0,
	)

	if ret != CR_SUCCESS {
		return ""
	}

	return syscall.UTF16ToString(buffer)
}

// ParsePortNumber extracts the port number from a LocationInfo string.
// Examples: "Port_#0002.Hub_#0002" -> "2", "Port #1" -> "1"
// Returns empty string if no port number found.
func ParsePortNumber(locationInfo string) string {
	if locationInfo == "" {
		return ""
	}

	matches := portRegex.FindStringSubmatch(locationInfo)
	if len(matches) >= 2 {
		// Remove leading zeros
		port := strings.TrimLeft(matches[1], "0")
		if port == "" {
			port = "0"
		}
		return port
	}
	return ""
}
