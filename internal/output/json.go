package output

import (
	"encoding/json"
	"fmt"
	"os"
)

// PrintJSON outputs data as formatted JSON to stdout
func PrintJSON(data interface{}) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// PrintJSONError outputs an error as JSON to stderr
func PrintJSONError(message string, code string) {
	errObj := map[string]string{
		"error": message,
		"code":  code,
	}
	data, _ := json.Marshal(errObj)
	fmt.Fprintln(os.Stderr, string(data))
}

// Error codes
const (
	ErrCodeUSBNotFound   = "USB_NOT_FOUND"
	ErrCodePwshNotFound  = "PWSH_NOT_FOUND"
	ErrCodeFormatFailed  = "FORMAT_FAILED"
	ErrCodeFlashFailed   = "FLASH_FAILED"
	ErrCodePermDenied    = "PERMISSION_DENIED"
	ErrCodeInvalidInput  = "INVALID_INPUT"
	ErrCodeInternalError = "INTERNAL_ERROR"
	ErrCodeDiskBusy      = "DISK_BUSY"
)
