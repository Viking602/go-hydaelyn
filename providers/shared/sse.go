package shared

import (
	"bufio"
	"io"
	"strings"
)

// Event represents a parsed Server-Sent Event frame.
type Event struct {
	Name    string
	Data    string
	ID      string
	Comment string
}

// Reader incrementally parses SSE frames from a text stream.
type Reader struct {
	scanner *bufio.Scanner
}

func NewReader(body io.Reader) *Reader {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return &Reader{scanner: scanner}
}

// Next reads and returns the next SSE frame.
// It properly handles:
//   - multi-line data: concatenated with '\n'
//   - event: and id: fields
//   - comment lines starting with ':' (used for keepalive heartbeats)
//   - incremental/incomplete frames buffered across scanner reads
func (r *Reader) Next() (Event, error) {
	var (
		name    string
		data    []string
		id      string
		comment string
	)
	for r.scanner.Scan() {
		line := r.scanner.Text()
		if strings.TrimSpace(line) == "" {
			// Empty line terminates a frame. Skip if frame is entirely empty.
			if len(data) == 0 && name == "" && id == "" && comment == "" {
				continue
			}
			return Event{
				Name:    name,
				Data:    strings.Join(data, "\n"),
				ID:      id,
				Comment: comment,
			}, nil
		}
		if strings.HasPrefix(line, ":") {
			// SSE comment (e.g., :keepalive)
			comment = strings.TrimSpace(strings.TrimPrefix(line, ":"))
			continue
		}
		field, value := parseSSELine(line)
		switch field {
		case "event":
			name = value
		case "data":
			data = append(data, value)
		case "id":
			id = value
		// retry: is intentionally ignored
		}
	}
	if err := r.scanner.Err(); err != nil {
		return Event{}, err
	}
	if len(data) > 0 || name != "" || id != "" || comment != "" {
		return Event{
			Name:    name,
			Data:    strings.Join(data, "\n"),
			ID:      id,
			Comment: comment,
		}, nil
	}
	return Event{}, io.EOF
}

// parseSSELine splits an SSE field line into (field, value).
// Per spec, strip one leading space after the colon if present.
func parseSSELine(line string) (field, value string) {
	colonIdx := strings.Index(line, ":")
	if colonIdx == -1 {
		return "", ""
	}
	field = line[:colonIdx]
	if colonIdx+1 < len(line) && line[colonIdx+1] == ' ' {
		value = line[colonIdx+2:]
	} else if colonIdx+1 < len(line) {
		value = line[colonIdx+1:]
	}
	return field, value
}
