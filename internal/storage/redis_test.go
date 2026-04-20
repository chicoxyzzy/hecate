package storage

import (
	"bufio"
	"bytes"
	"errors"
	"testing"
)

func TestWriteCommand(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := writeCommand(&buf, "SET", "k", "v"); err != nil {
		t.Fatalf("writeCommand() error = %v", err)
	}

	want := "*3\r\n$3\r\nSET\r\n$1\r\nk\r\n$1\r\nv\r\n"
	if got := buf.String(); got != want {
		t.Fatalf("writeCommand() = %q, want %q", got, want)
	}
}

func TestReadReply(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    any
		wantErr error
	}{
		{name: "simple string", input: "+OK\r\n", want: "OK"},
		{name: "integer", input: ":42\r\n", want: int64(42)},
		{name: "bulk string", input: "$5\r\nhello\r\n", want: []byte("hello")},
		{name: "nil bulk string", input: "$-1\r\n", wantErr: ErrNil},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := readReply(bufio.NewReader(bytes.NewBufferString(tt.input)))
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("readReply() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("readReply() error = %v", err)
			}

			switch want := tt.want.(type) {
			case string:
				if got.(string) != want {
					t.Fatalf("readReply() = %q, want %q", got, want)
				}
			case int64:
				if got.(int64) != want {
					t.Fatalf("readReply() = %d, want %d", got, want)
				}
			case []byte:
				if string(got.([]byte)) != string(want) {
					t.Fatalf("readReply() = %q, want %q", got, want)
				}
			}
		})
	}
}
