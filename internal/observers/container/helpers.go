//go:build linux
// +build linux

package container

// nullTerminatedString converts C-style null-terminated byte array to Go string
func nullTerminatedString(b []byte) string {
	n := 0
	for n < len(b) && b[n] != 0 {
		n++
	}
	return string(b[:n])
}
