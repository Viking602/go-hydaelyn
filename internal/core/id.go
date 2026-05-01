package core

import "fmt"

func (r *Runtime) newID(prefix string) string {
	n := r.idSeq.Add(1)
	return fmt.Sprintf("%s-%d", prefix, n)
}
