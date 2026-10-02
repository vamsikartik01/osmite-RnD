//go:build windows

package platform

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Processes lists running processes. Command lines are read only for
// descendants of the given roots, since that needs a handle per process.
func Processes(roots []int) ([]Process, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snap)

	var all []Process
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		all = append(all, Process{
			PID: int(e.ProcessID), PPID: int(e.ParentProcessID),
			Args: []string{windows.UTF16ToString(e.ExeFile[:])},
		})
	}

	want := map[int]bool{}
	for _, r := range roots {
		for _, d := range Descendants(all, r) {
			want[d.PID] = true
		}
	}
	for i := range all {
		if !want[all[i].PID] {
			continue
		}
		if args := commandLine(all[i].PID); len(args) > 0 {
			all[i].Args = args
		}
	}
	return all, nil
}

// commandLine reads a process's command line (Windows 8.1+).
func commandLine(pid int) []string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(h)

	buf := make([]byte, 4096)
	var n uint32
	for range 3 {
		err = windows.NtQueryInformationProcess(h, windows.ProcessCommandLineInformation,
			unsafe.Pointer(&buf[0]), uint32(len(buf)), &n)
		if err == nil {
			break
		}
		if n <= uint32(len(buf)) {
			return nil
		}
		buf = make([]byte, n)
	}
	if err != nil {
		return nil
	}
	us := (*windows.NTUnicodeString)(unsafe.Pointer(&buf[0]))
	cmd := us.String()
	args, err := windows.DecomposeCommandLine(cmd)
	if err != nil {
		return []string{cmd}
	}
	return args
}
