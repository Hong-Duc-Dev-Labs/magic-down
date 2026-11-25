package nativemessaging

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// MaxMessageSize limits the incoming message size to 1MB to prevent DOS.
const MaxMessageSize = 1024 * 1024

// ReadMessage reads a 4-byte little-endian length-prefixed JSON message from r.
func ReadMessage(r io.Reader) ([]byte, error) {
	var length uint32
	if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
		return nil, err
	}

	if length > MaxMessageSize {
		return nil, fmt.Errorf("message length %d exceeds maximum allowed size %d", length, MaxMessageSize)
	}

	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, fmt.Errorf("failed to read complete message of %d bytes: %w", length, err)
	}

	return buf, nil
}

// WriteMessage writes a 4-byte little-endian length-prefixed message to w.
func WriteMessage(w io.Writer, data []byte) error {
	length := uint32(len(data))
	if length > MaxMessageSize {
		return errors.New("message exceeds max allowed size")
	}

	if err := binary.Write(w, binary.LittleEndian, length); err != nil {
		return fmt.Errorf("failed to write length prefix: %w", err)
	}

	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("failed to write message payload: %w", err)
	}

	return nil
}
