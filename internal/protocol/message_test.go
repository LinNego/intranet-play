package protocol

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestEncodeDecodeJoin(t *testing.T) {
	env, err := NewEnvelope(TypeJoin, "Bob", JoinPayload{Name: "Bob"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := Encode(env)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(raw, []byte("\n")) {
		t.Fatal("encode should end with newline")
	}
	got, err := DecodeLine(bytes.TrimRight(raw, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != TypeJoin {
		t.Fatalf("type: want %s, got %s", TypeJoin, got.Type)
	}
	if got.From != "Bob" {
		t.Fatalf("from: want Bob, got %s", got.From)
	}
	var p JoinPayload
	if err := json.Unmarshal(got.Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.Name != "Bob" {
		t.Fatalf("name: want Bob, got %s", p.Name)
	}
}
func TestEncodeDecodePlace(t *testing.T) {
	env, err := NewEnvelope(TypePlace, "Alice", PlacePayload{Pos: Pos{X: 7, Y: 7}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := Encode(env)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeLine(bytes.TrimRight(raw, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	var p PlacePayload
	if err := json.Unmarshal(got.Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.Pos.X != 7 || p.Pos.Y != 7 {
		t.Fatalf("pos: want (7,7), got (%d,%d)", p.Pos.X, p.Pos.Y)
	}
}
func TestEncodeDecodeChat(t *testing.T) {
	env, err := NewEnvelope(TypeChat, "Alice", ChatPayload{Message: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := Encode(env)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeLine(bytes.TrimRight(raw, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	var p ChatPayload
	if err := json.Unmarshal(got.Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.Message != "hello" {
		t.Fatalf("message: want hello, got %s", p.Message)
	}
}
func TestEncodeDecodeNilPayload(t *testing.T) {
	env, err := NewEnvelope(TypeResign, "Alice", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := Encode(env)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeLine(bytes.TrimRight(raw, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != TypeResign {
		t.Fatalf("type: want %s, got %s", TypeResign, got.Type)
	}
}
func TestDecodeInvalidJSON(t *testing.T) {
	if _, err := DecodeLine([]byte(`{not-json`)); err == nil {
		t.Fatal("want error for invalid json")
	}
}
