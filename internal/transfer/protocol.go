package transfer

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

var protocolMagic = [4]byte{'W', 'S', 'K', '1'}

type Control struct {
	Update   bool     `json:"update"`
	Manifest Manifest `json:"manifest,omitempty"`
}

func WriteControl(w io.Writer, control Control) error {
	payload, err := json.Marshal(control)
	if err != nil {
		return fmt.Errorf("encode control data: %w", err)
	}
	if len(payload) > 256<<20 {
		return fmt.Errorf("control data exceeds 256 MiB")
	}
	if _, err := w.Write(protocolMagic[:]); err != nil {
		return err
	}
	if err := binary.Write(w, binary.BigEndian, uint32(len(payload))); err != nil {
		return err
	}
	_, err = w.Write(payload)
	return err
}

func ReadControl(r io.Reader) (Control, error) {
	var control Control
	var magic [4]byte
	if _, err := io.ReadFull(r, magic[:]); err != nil {
		return control, fmt.Errorf("read protocol header: %w", err)
	}
	if magic != protocolMagic {
		return control, fmt.Errorf("unsupported protocol header")
	}
	var size uint32
	if err := binary.Read(r, binary.BigEndian, &size); err != nil {
		return control, fmt.Errorf("read control length: %w", err)
	}
	if size > 256<<20 {
		return control, fmt.Errorf("control data exceeds 256 MiB")
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(r, payload); err != nil {
		return control, fmt.Errorf("read control data: %w", err)
	}
	if err := json.Unmarshal(payload, &control); err != nil {
		return control, fmt.Errorf("parse control data: %w", err)
	}
	if control.Manifest == nil {
		control.Manifest = Manifest{}
	}
	return control, nil
}
