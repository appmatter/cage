package secrets

import (
	"testing"

	"github.com/appmatter/cage/internal/config"
)

func TestApply(t *testing.T) {
	vals := Values{
		"onepassword": {"OPENAI_API_KEY": "sk-live"},
	}
	got, err := Apply("Bearer {{ secrets.onepassword.OPENAI_API_KEY }}", vals)
	if err != nil || got != "Bearer sk-live" {
		t.Fatalf("got %q err=%v", got, err)
	}
	if _, err := Apply("{{ secrets.missing.X }}", vals); err == nil {
		t.Fatal("expected unknown secret")
	}
}

func TestApplyMap(t *testing.T) {
	vals := Values{"onepassword": {"K": "secret"}}
	in := map[string]string{
		"A": "{{ secrets.onepassword.K }}",
		"B": "literal",
	}
	got, err := ApplyMap(in, vals)
	if err != nil {
		t.Fatal(err)
	}
	if got["A"] != "secret" || got["B"] != "literal" {
		t.Fatalf("%#v", got)
	}
	if !MapHasTemplate(in) || MapHasTemplate(map[string]string{"B": "literal"}) {
		t.Fatal("MapHasTemplate")
	}
}

func TestOrderSeatsDeps(t *testing.T) {
	seats := map[string]config.SecretStore{
		"b": {Uses: []string{"a"}, Vars: map[string]string{"Y": "y"}},
		"a": {Vars: map[string]string{"X": "x"}},
		"c": {Vars: map[string]string{"Z": "{{ secrets.b.Y }}"}},
	}
	order, err := orderSeats(seats)
	if err != nil {
		t.Fatal(err)
	}
	idx := map[string]int{}
	for i, s := range order {
		idx[s] = i
	}
	if !(idx["a"] < idx["b"] && idx["b"] < idx["c"]) {
		t.Fatalf("order=%v", order)
	}
}

func TestOrderSeatsCycle(t *testing.T) {
	seats := map[string]config.SecretStore{
		"a": {Uses: []string{"b"}, Vars: map[string]string{"X": "x"}},
		"b": {Uses: []string{"a"}, Vars: map[string]string{"Y": "y"}},
	}
	if _, err := orderSeats(seats); err == nil {
		t.Fatal("expected cycle")
	}
}
