package protocol

import (
	"encoding/binary"
	"encoding/json"
	"io"
)

type MessageType uint8

const (
	MsgRegister MessageType = iota
	MsgHeartbeat
	MsgRequest
	MsgResponse
	MsgClose
	MsgRegisterAck
)

type Message struct {
	Type    MessageType `json:"type"`
	ID      string      `json:"id,omitempty"`
	Payload []byte      `json:"payload,omitempty"`
}

type RegisterPayload struct {
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`
	Version    string `json:"version"`
}

type RegisterAckPayload struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type HeartbeatPayload struct {
	Timestamp int64 `json:"timestamp"`
}

type RequestPayload struct {
	RequestID  string            `json:"request_id"`
	Method     string            `json:"method"`
	URL        string            `json:"url"`
	Headers    map[string]string `json:"headers"`
	Body       []byte            `json:"body,omitempty"`
	TargetPort int               `json:"target_port"`
}

type ResponsePayload struct {
	RequestID  string            `json:"request_id"`
	StatusCode int               `json:"status_code"`
	Headers    map[string]string `json:"headers"`
	Body       []byte            `json:"body,omitempty"`
	Error      string            `json:"error,omitempty"`
}

type ClosePayload struct {
	Reason string `json:"reason"`
}

func WriteMessage(w io.Writer, msg *Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	length := uint32(len(data))
	if err := binary.Write(w, binary.BigEndian, length); err != nil {
		return err
	}

	_, err = w.Write(data)
	return err
}

func ReadMessage(r io.Reader) (*Message, error) {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return nil, err
	}

	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}

	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}

	return &msg, nil
}
