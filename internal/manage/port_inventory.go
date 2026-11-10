package manage

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
)

func checkPortInventory(input io.Reader, tcp bool, ranges []portRange, ownPort int) error {
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		// Only the first four columns matter. Borrow the scanner's bytes instead
		// of allocating a string and a slice of every column for each socket.
		var fields [4][]byte
		count := 0
		for field := range bytes.FieldsSeq(scanner.Bytes()) {
			fields[count] = field
			count++
			if count == len(fields) {
				break
			}
		}
		if count == 0 || bytes.Equal(fields[0], []byte("sl")) {
			continue
		}
		separator := bytes.IndexByte(fields[1], ':')
		if count < len(fields) || separator <= 0 {
			return errors.New("系统端口记录不完整")
		}
		port, portErr := strconv.ParseUint(string(fields[1][separator+1:]), 16, 16)
		state, stateErr := strconv.ParseUint(string(fields[3]), 16, 8)
		if portErr != nil || stateErr != nil {
			return errors.New("系统端口记录的端口或状态无效")
		}
		if tcp && state != 0x0a {
			continue
		}
		if int(port) != ownPort && portInRanges(int(port), ranges) {
			return fmt.Errorf("端口 %d 已被其他程序使用", port)
		}
	}
	return scanner.Err()
}
