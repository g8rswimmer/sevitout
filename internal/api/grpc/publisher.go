package grpc

import (
	"encoding/json"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Publisher broadcasts a typed event to WebSocket clients subscribed to a
// SEV's room. Declared here (the consumer) rather than in package ws, per
// this repo's interface-ownership convention — ws.Hub satisfies it
// implicitly. payload is pre-marshaled JSON so package ws never needs to
// know about protobuf types.
type Publisher interface {
	Publish(sevID, eventType string, payload []byte)
}

// protojsonMarshal matches the field naming (snake_case) the REST gateway
// uses (see cmd/server/main.go's runtime.WithMarshalerOption), so a
// WebSocket event's payload has the same shape as the equivalent REST
// response body.
var protojsonMarshal = protojson.MarshalOptions{UseProtoNames: true}.Marshal

// publishProto marshals msg via protojson and publishes it under eventType
// on sevID's room. A nil Publisher (WebSocket support not wired up, e.g. in
// unit tests) is a no-op — checked before marshaling so a nil Publisher
// never pays for encoding work nobody will use.
func publishProto(p Publisher, sevID, eventType string, msg proto.Message) {
	if p == nil {
		return
	}
	b, err := protojsonMarshal(msg)
	publish(p, sevID, eventType, b, err)
}

// publishJSON publishes an ad hoc payload under eventType on sevID's room,
// for events with no proto response to reuse (e.g. a role or task removal).
func publishJSON(p Publisher, sevID, eventType string, v any) {
	if p == nil {
		return
	}
	b, err := json.Marshal(v)
	publish(p, sevID, eventType, b, err)
}

// publish applies the policy shared by publishProto and publishJSON: a
// marshal failure is swallowed rather than propagated, since event delivery
// is best-effort and must never fail the underlying mutation it's attached to.
func publish(p Publisher, sevID, eventType string, b []byte, err error) {
	if err != nil {
		return
	}
	p.Publish(sevID, eventType, b)
}
