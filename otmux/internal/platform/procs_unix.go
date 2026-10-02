//go:build !windows

package platform

import (
	"bufio"
	"bytes"
	"os/exec"
	"strconv"
	"strings"
)

// Processes lists running processes with their arguments, using ps, which
// behaves the same here on Linux and macOS.
func Processes(_ []int) ([]Process, error) {
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,args=").Output()
	if err != nil {
		return nil, err
	}
	var procs []Process
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 3 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		if err1 != nil || err2 != nil {
			continue
		}
		procs = append(procs, Process{PID: pid, PPID: ppid, Args: f[2:]})
	}
	return procs, nil
}
