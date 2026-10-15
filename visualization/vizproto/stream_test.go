package vizproto

import (
	"strings"
	"testing"
)

func TestReadsVersionOneAndTwo(t *testing.T) {
	for _, v := range []string{"1", "2"} {
		rec := `{"type":"hello","t":0,"version":` + v + `,"world":{"points":[],"tracks":[],"switches":[],"signals":[],"stations":[],"trains":[]}}
{"type":"decision","t":3,"id":"hold:A>B@X","action":"hold","train":"A","for":"B","at":"X","status":"active","source":"auto","since":3}
{"type":"end","t":4}
`
		_, msgs, err := ReadAll(strings.NewReader(rec))
		if err != nil {
			t.Fatalf("version %s: %v", v, err)
		}
		st := NewState()
		for _, m := range msgs {
			st.Apply(m)
		}
		if got := st.ActiveDecisions("A"); len(got) != 1 || got[0].For != "B" {
			t.Errorf("version %s: decisions %+v", v, got)
		}
	}
	if _, _, err := ReadAll(strings.NewReader(`{"type":"hello","t":0,"version":3,"world":{}}`)); err == nil {
		t.Error("version 3 accepted")
	}
}
