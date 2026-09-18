package domain

import (
	"encoding/json"
	"testing"
)

func TestVideoProjectDeserializationNormalizesProjectType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		json string
		want string
	}{
		{name: "legacy", json: `{"id":"legacy"}`, want: "story"},
		{name: "promo", json: `{"id":"promo","type":" PROMO "}`, want: "promo"},
		{name: "reel", json: `{"id":"reel","type":"reel"}`, want: "reel"},
		{name: "unsupported", json: `{"id":"unknown","type":"unknown"}`, want: "story"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var project VideoProject
			if err := json.Unmarshal([]byte(test.json), &project); err != nil {
				t.Fatal(err)
			}
			if project.Type != test.want {
				t.Fatalf("type = %q, want %q", project.Type, test.want)
			}
		})
	}
}
