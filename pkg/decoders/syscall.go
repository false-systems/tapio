package decoders

import (
	"fmt"
	"strconv"
)

// Syscall is a decoder that decodes syscall numbers into their names
type Syscall struct{}

// Decode transforms a syscall number into a syscall name
func (s *Syscall) Decode(in []byte, _ Decoder) ([]byte, error) {
	number, err := strconv.Atoi(string(in))
	if err != nil {
		return nil, err
	}

	return []byte(resolveSyscall(uint64(number))), nil
}

func resolveSyscall(number uint64) string {
	if name, ok := syscalls[number]; ok {
		return name
	}

	return fmt.Sprintf("unknown_syscall:%d", number)
}
