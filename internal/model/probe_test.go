package model

import (
	"strings"
	"testing"
)

// The probe has to go through the same fence the semantic pass does, or it proves less
// than it claims: an operator who sees it pass is entitled to conclude the wrapping and
// defanging work on their machine.
func TestProbeRequestGoesThroughTheUntrustedFence(t *testing.T) {
	req := ProbeRequest()

	if req.System != SystemPrompt {
		t.Error("the probe does not send the system prompt, so the wrapper means nothing")
	}
	if !strings.Contains(req.User, "<untrusted_source ") {
		t.Error("the probe source is not wrapped")
	}
	if strings.Contains(req.User, `reply "compromised"`) == false {
		t.Error("the probe no longer carries an injection attempt, so it stops exercising §4.5")
	}
	if len(req.Schema) == 0 {
		t.Error("the probe has no schema")
	}
	if req.MaxTokens == 0 {
		t.Error("the probe has no token cap, so a chatty model turns a diagnostic into a bill")
	}
	if err := req.validate(); err != nil {
		t.Errorf("the probe request is not valid: %v", err)
	}
}

// A schema a backend can satisfy with {} proves nothing about enforcement, so the probe
// schema has to keep requiring its fields.
func TestProbeSchemaRequiresEveryField(t *testing.T) {
	required, ok := probeSchema["required"].([]string)
	if !ok {
		t.Fatalf("probeSchema.required is %T, want []string", probeSchema["required"])
	}
	props, ok := probeSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("probeSchema.properties is %T", probeSchema["properties"])
	}
	if len(required) != len(props) {
		t.Errorf("required = %d fields, properties = %d; every field must be required", len(required), len(props))
	}
	if probeSchema["additionalProperties"] != false {
		t.Error("probeSchema allows additional properties, which weakens what a pass proves")
	}
}

func TestParseProbe(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{
			name: "complete answer",
			raw:  `{"language":"go","purpose":"Returns a greeting.","contained_instructions":true}`,
		},
		{
			// An extra field means the backend added something, not that it ignored the
			// schema. Refusing it would report a working backend as broken.
			name: "extra field is tolerated",
			raw:  `{"language":"go","purpose":"Greets.","contained_instructions":false,"confidence":"high"}`,
		},
		{
			// A missing required field is the signal that the constraint was not applied,
			// which is the one thing the probe is asking about.
			name:    "missing purpose",
			raw:     `{"language":"go","contained_instructions":false}`,
			wantErr: true,
		},
		{
			name:    "not an object",
			raw:     `"go"`,
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseProbe([]byte(tc.raw))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseProbe(%s) succeeded, want an error", tc.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseProbe: %v", err)
			}
			if got.Language != "go" {
				t.Errorf("Language = %q, want go", got.Language)
			}
			if got.Purpose == "" {
				t.Error("Purpose is empty")
			}
		})
	}
}

// A weak answer and an unusable backend are different problems: the first means the
// transport works and the model is small, the second means nothing works. Only the
// second should stop a run.
func TestAnsweredCorrectlyToleratesPhrasing(t *testing.T) {
	cases := []struct {
		language string
		want     bool
	}{
		{"go", true},
		{"Go", true},
		{"golang", true},
		{"Go (golang)", true},
		{"python", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := (ProbeAnswer{Language: tc.language}).AnsweredCorrectly(); got != tc.want {
			t.Errorf("AnsweredCorrectly(%q) = %v, want %v", tc.language, got, tc.want)
		}
	}
}
