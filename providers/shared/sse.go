package shared

import (
	"bufio"
	"io"
	"strings"
)

type Event struct {
	Name string
	Data string
}

type Reader struct {
	scanner *bufio.Scanner
}

func NewReader(body io.Reader) *Reader {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return &Reader{scanner: scanner}
}

func (r *Reader) Next() (Event, error) {
	var (
		name string
		data []string
	)
	for r.scanner.Scan() {
		line := r.scanner.Text()
		if strings.TrimSpace(line) == "" {
			if len(data) == 0 && name == "" {
				continue
			}
			return Event{
				Name: name,
				Data: strings.Join(data, "\n"),
			}, nil
		}
		if strings.HasPrefix(line, "event:") {
			name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := r.scanner.Err(); err != nil {
		return Event{}, err
	}
	if len(data) > 0 || name != "" {
		return Event{Name: name, Data: strings.Join(data, "\n")}, nil
	}
	return Event{}, io.EOF
}
