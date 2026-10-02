package keys

import (
	"runtime"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/vamsikartik01/osmite-RnD/otmux/internal/protocol"
)

var (
	ctrlB = uv.KeyPressEvent{Code: 'b', Mod: uv.ModCtrl}
	keyC  = uv.KeyPressEvent{Code: 'c', Text: "c"}
	shift = uv.KeyPressEvent{Code: uv.KeyLeftShift, Mod: uv.ModShift}
)

func TestPlainKeysForward(t *testing.T) {
	m := NewMachine(Default(""))
	if r, _ := m.Handle(keyC); r != Forward {
		t.Fatalf("got %v, want Forward", r)
	}
}

func TestPrefixThenBinding(t *testing.T) {
	m := NewMachine(Default(""))
	if r, _ := m.Handle(ctrlB); r != Consumed || !m.Pending() {
		t.Fatalf("prefix not consumed")
	}
	r, a := m.Handle(keyC)
	if r != Run || a.Name != ActionPromptNewTab {
		t.Fatalf("got %v %+v, want prompt-new-tab", r, a)
	}
	if m.Pending() {
		t.Fatal("prefix still pending after binding")
	}
}

func TestModifierDoesNotCancelPrefix(t *testing.T) {
	m := NewMachine(Default(""))
	m.Handle(ctrlB)
	m.Handle(shift)
	r, a := m.Handle(uv.KeyPressEvent{Code: '7', Text: "&", Mod: uv.ModShift})
	if r != Run || a.Name != protocol.ActionCloseTab {
		t.Fatalf("got %v %+v, want close-tab", r, a)
	}
}

func TestDoublePrefixSendsPrefix(t *testing.T) {
	m := NewMachine(Default(""))
	m.Handle(ctrlB)
	if r, a := m.Handle(ctrlB); r != Run || a.Name != ActionSendPrefix {
		t.Fatalf("got %v %+v, want send-prefix", r, a)
	}
}

func TestUnknownBindingSwallowed(t *testing.T) {
	m := NewMachine(Default(""))
	m.Handle(ctrlB)
	if r, _ := m.Handle(uv.KeyPressEvent{Code: 'y', Text: "y"}); r != Consumed {
		t.Fatalf("got %v, want Consumed", r)
	}
	if r, _ := m.Handle(keyC); r != Forward {
		t.Fatal("prefix should be cleared after unknown binding")
	}
}

func TestSplitKeys(t *testing.T) {
	m := NewMachine(Default(""))
	for key, want := range map[uv.KeyPressEvent]string{
		{Code: 'v', Text: "v"}:                    protocol.ActionSplitRight,
		{Code: 'h', Text: "h"}:                    protocol.ActionSplitDown,
		{Code: '5', Text: "%", Mod: uv.ModShift}:  protocol.ActionSplitRight,
		{Code: '\'', Text: `"`, Mod: uv.ModShift}: protocol.ActionSplitDown,
	} {
		m.Handle(ctrlB)
		if r, a := m.Handle(key); r != Run || a.Name != want {
			t.Errorf("%q: got %v %+v, want %s", key.Text, r, a, want)
		}
	}
}

func TestRepeatableResize(t *testing.T) {
	m := NewMachine(Default(""))
	now := time.Unix(0, 0)
	m.now = func() time.Time { return now }
	ctrlLeft := uv.KeyPressEvent{Code: uv.KeyLeft, Mod: uv.ModShift}

	m.Handle(ctrlB)
	if r, a := m.Handle(ctrlLeft); r != Run || a.Name != protocol.ActionResizeL {
		t.Fatalf("first: %v %+v", r, a)
	}
	now = now.Add(300 * time.Millisecond)
	if r, a := m.Handle(ctrlLeft); r != Run || a.Name != protocol.ActionResizeL {
		t.Fatalf("repeat within window: %v %+v", r, a)
	}
	now = now.Add(time.Second)
	if r, _ := m.Handle(ctrlLeft); r != Forward {
		t.Fatalf("after window: got %v, want Forward", r)
	}
}

func TestCustomPrefix(t *testing.T) {
	m := NewMachine(Default("ctrl+a"))
	if r, _ := m.Handle(ctrlB); r != Forward {
		t.Fatal("ctrl+b should pass through with a ctrl+a prefix")
	}
	m.Handle(uv.KeyPressEvent{Code: 'a', Mod: uv.ModCtrl})
	if !m.Pending() {
		t.Fatal("ctrl+a should be the prefix")
	}
}

func TestNoShiftNeededForPrimaryKeys(t *testing.T) {
	km := Default("")
	for _, cmd := range Catalog {
		k := km.KeyFor(cmd.Action)
		if k == "" || strings.HasPrefix(k, "shift+") { // Shift+arrows resize on purpose
			continue
		}
		if len(k) == 1 && (k[0] >= 'A' && k[0] <= 'Z' || strings.Contains(`~!@#$%^&*()_+{}|:"<>?`, k)) {
			t.Errorf("%s: primary key %q needs Shift", cmd.Title, k)
		}
	}
}

func TestLabel(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("mac labels use symbols")
	}
	for in, want := range map[string]string{"ctrl+b": "Ctrl+B", "%": "%", "alt+left": "Alt+←", "C": "C"} {
		if got := Label(in); got != want {
			t.Errorf("Label(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSelectTabDigit(t *testing.T) {
	m := NewMachine(Default(""))
	m.Handle(ctrlB)
	r, a := m.Handle(uv.KeyPressEvent{Code: '3', Text: "3"})
	if r != Run || a.Name != protocol.ActionSelectTab || a.Arg != "3" {
		t.Fatalf("got %v %+v", r, a)
	}
}
