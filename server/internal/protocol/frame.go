package protocol

import (
	"encoding/binary"
	"errors"
	"io"
)

const (
	FrameTypeData = 0
	FrameTypePing = 1
	FrameTypePong = 2
)

type Frame struct {
	Type byte
	Data []byte
}

func (f *Frame) Marshal() []byte {
	length := uint32(len(f.Data) + 1) // +1 for type byte
	buf := make([]byte, 4+1+len(f.Data))
	
	binary.BigEndian.PutUint32(buf[0:4], length)
	buf[4] = f.Type
	copy(buf[5:], f.Data)
	
	return buf
}

func ReadFrame(r io.Reader) (*Frame, error) {
	// Read length (4 bytes)
	lengthBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, lengthBuf); err != nil {
		return nil, err
	}
	
	length := binary.BigEndian.Uint32(lengthBuf)
	if length == 0 {
		return nil, errors.New("invalid frame length")
	}
	
	if length > 65536 { // Max frame size 64KB
		return nil, errors.New("frame too large")
	}
	
	// Read type + data
	frameBuf := make([]byte, length)
	if _, err := io.ReadFull(r, frameBuf); err != nil {
		return nil, err
	}
	
	frame := &Frame{
		Type: frameBuf[0],
		Data: frameBuf[1:],
	}
	
	return frame, nil
}

func WriteFrame(w io.Writer, frame *Frame) error {
	data := frame.Marshal()
	_, err := w.Write(data)
	return err
}
