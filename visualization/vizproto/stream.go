package vizproto

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

type Writer struct {
	buf *bufio.Writer
	enc *json.Encoder
}

func NewWriter(w io.Writer) *Writer {
	buf := bufio.NewWriter(w)
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	return &Writer{buf: buf, enc: enc}
}

// Write sets the message's type and writes it as one line.
func (w *Writer) Write(m Message) error {
	m.Head().Type = m.kind()
	return w.enc.Encode(m)
}

func (w *Writer) Flush() error { return w.buf.Flush() }

type Reader struct {
	sc   *bufio.Scanner
	line int
}

func NewReader(r io.Reader) *Reader {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 64*1024*1024)
	return &Reader{sc: sc}
}

// Next returns the next message, or io.EOF at the end of the stream. Lines of
// an unknown type are skipped.
func (r *Reader) Next() (Message, error) {
	for r.sc.Scan() {
		r.line++
		data := r.sc.Bytes()
		if len(data) == 0 {
			continue
		}
		var h Header
		if err := json.Unmarshal(data, &h); err != nil {
			return nil, fmt.Errorf("vizproto: line %d: %w", r.line, err)
		}
		var m Message
		switch h.Type {
		case TypeHello:
			m = &HelloMsg{}
		case TypeTrack:
			m = &TrackMsg{}
		case TypeSwitch:
			m = &SwitchMsg{}
		case TypeTrain:
			m = &TrainMsg{}
		case TypeEvent:
			m = &EventMsg{}
		case TypeEnd:
			m = &EndMsg{}
		case TypeError:
			m = &ErrorMsg{}
		default:
			continue
		}
		if err := json.Unmarshal(data, m); err != nil {
			return nil, fmt.Errorf("vizproto: line %d (%s): %w", r.line, h.Type, err)
		}
		return m, nil
	}
	if err := r.sc.Err(); err != nil {
		return nil, err
	}
	return nil, io.EOF
}

// ReadAll reads a whole recording. It must start with a hello.
func ReadAll(r io.Reader) (*HelloMsg, []Message, error) {
	rd := NewReader(r)
	var msgs []Message
	for {
		m, err := rd.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		msgs = append(msgs, m)
	}
	if len(msgs) == 0 {
		return nil, nil, fmt.Errorf("vizproto: empty recording")
	}
	hello, ok := msgs[0].(*HelloMsg)
	if !ok {
		return nil, nil, fmt.Errorf("vizproto: recording starts with %s, not hello", msgs[0].Head().Type)
	}
	if hello.Version != Version {
		return nil, nil, fmt.Errorf("vizproto: recording is version %d, this program reads %d", hello.Version, Version)
	}
	return hello, msgs[1:], nil
}
