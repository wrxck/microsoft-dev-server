package scenarios

import (
	"encoding/json"
	"sync"
	"testing"
)

func TestMatchAndConsume_OneShot(t *testing.T) {
	s := NewStore()
	s.Add(&Scenario{Method: "POST", PathContains: "/sendMail", Status: 503, BodyJSON: `{}`, OneShot: true})
	if s.MatchAndConsume("POST", "/v1.0/me/sendMail") == nil {
		t.Fatal("first match")
	}
	if s.Count() != 0 {
		t.Errorf("expected one-shot consumed")
	}
}

func TestMatchAndConsume_Sticky(t *testing.T) {
	s := NewStore()
	s.Add(&Scenario{Method: "POST", PathContains: "/sendMail", Status: 503, BodyJSON: `{}`, OneShot: false})
	s.MatchAndConsume("POST", "/v1.0/me/sendMail")
	s.MatchAndConsume("POST", "/v1.0/me/sendMail")
	if s.Count() != 1 {
		t.Errorf("sticky should remain")
	}
}

func TestMatchAndConsume_MethodFilter(t *testing.T) {
	s := NewStore()
	s.Add(&Scenario{Method: "POST", PathContains: "/sendMail", Status: 503, BodyJSON: `{}`, OneShot: true})
	if s.MatchAndConsume("GET", "/v1.0/me/sendMail") != nil {
		t.Errorf("GET should not match POST")
	}
}

func TestConcurrent(t *testing.T) {
	s := NewStore()
	for i := 0; i < 50; i++ {
		s.Add(&Scenario{Method: "POST", PathContains: "/x", Status: 500, BodyJSON: `{}`, OneShot: true})
	}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); s.MatchAndConsume("POST", "/x") }()
	}
	wg.Wait()
	if s.Count() != 0 {
		t.Errorf("expected all consumed; count=%d", s.Count())
	}
}

// TestPresets_ProductionFidelity asserts every preset returns a body
// matching the documented graph error envelope so the consumer's
// production decode path is exercised.
func TestPresets_ProductionFidelity(t *testing.T) {
	for _, p := range Presets {
		t.Run(p.Key, func(t *testing.T) {
			if p.Status < 200 || p.Status >= 600 {
				t.Errorf("status %d outside http range", p.Status)
			}
			var decoded map[string]any
			if err := json.Unmarshal([]byte(p.Body), &decoded); err != nil {
				t.Fatalf("body invalid json: %v", err)
			}
			errObj, ok := decoded["error"].(map[string]any)
			if !ok {
				t.Fatalf("missing error envelope")
			}
			if _, ok := errObj["code"].(string); !ok {
				t.Errorf("error.code missing")
			}
			if _, ok := errObj["message"].(string); !ok {
				t.Errorf("error.message missing")
			}
			inner, ok := errObj["innerError"].(map[string]any)
			if !ok {
				t.Errorf("error.innerError missing")
			} else {
				if _, ok := inner["request-id"].(string); !ok {
					t.Errorf("error.innerError.request-id missing")
				}
			}
		})
	}
}

func TestPresets_UniqueKeys(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range Presets {
		if seen[p.Key] {
			t.Errorf("duplicate preset key: %s", p.Key)
		}
		seen[p.Key] = true
	}
}
