package ingest

import "testing"

func TestParseGatewayStatusTopic(t *testing.T) {
	cases := []struct {
		topic string
		id    string
		ok    bool
	}{
		{"iot/gateway/GW-A/status", "GW-A", true},
		{"iot/gateway/3zhang-test/status", "3zhang-test", true},
		{"iot/gateway//status", "", false},
		{"iot/gateway/GW-A/state", "", false},
		{"iot/ingest/GW-A", "", false},
		{"iot/gateway/a/b/status", "", false},
	}
	for _, c := range cases {
		id, ok := parseGatewayStatusTopic(c.topic)
		if id != c.id || ok != c.ok {
			t.Fatalf("%q → (%q,%v)，期望 (%q,%v)", c.topic, id, ok, c.id, c.ok)
		}
	}
}
