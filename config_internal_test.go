package olly

import "testing"

func TestDeriveInsecure(t *testing.T) {
	cases := []struct {
		endpoint string
		override *bool
		want     bool
	}{
		{"localhost:4319", nil, true},
		{"127.0.0.1:4319", nil, true},
		{"http://localhost:4318", nil, true},
		{"https://in-otel.hyperdx.io", nil, false},
		{"in-otel.hyperdx.io:4317", nil, false},
		{"localhost:4319", BoolPtr(false), false},
		{"in-otel.hyperdx.io:4317", BoolPtr(true), true},
	}
	for _, tc := range cases {
		if got := deriveInsecure(tc.endpoint, tc.override); got != tc.want {
			t.Fatalf("%s insecure=%v want %v", tc.endpoint, got, tc.want)
		}
	}
}

func TestNormalizeProtocol(t *testing.T) {
	if normalizeProtocol("http") != ProtocolHTTP {
		t.Fatal("http")
	}
	if normalizeProtocol("HTTP/protobuf") != ProtocolHTTP {
		t.Fatal("http/protobuf")
	}
	if normalizeProtocol("") != ProtocolGRPC {
		t.Fatal("default grpc")
	}
	if normalizeProtocol("weird") != ProtocolGRPC {
		t.Fatal("unknown -> grpc")
	}
}

func TestParseCommaKV(t *testing.T) {
	m := parseCommaKV("a=1, b=2,bad,=x,c=")
	if m["a"] != "1" || m["b"] != "2" || m["c"] != "" {
		t.Fatalf("%v", m)
	}
	if _, ok := m["bad"]; ok {
		t.Fatal("unexpected bad key")
	}
}
