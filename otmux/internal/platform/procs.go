package platform

// Process is one running process, as needed to find what a pane is running.
type Process struct {
	PID, PPID int
	Args      []string // argv; Args[0] may be a full path
}

// Descendants returns every process below root in procs.
func Descendants(procs []Process, root int) []Process {
	children := map[int][]Process{}
	for _, p := range procs {
		if p.PID != p.PPID {
			children[p.PPID] = append(children[p.PPID], p)
		}
	}
	var out []Process
	seen := map[int]bool{root: true}
	queue := []int{root}
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		for _, c := range children[pid] {
			if seen[c.PID] {
				continue
			}
			seen[c.PID] = true
			out = append(out, c)
			queue = append(queue, c.PID)
		}
	}
	return out
}
