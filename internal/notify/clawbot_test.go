package notify

import (
	"strings"
	"testing"
)

func TestClawBotMenuTextUsesCardLayout(t *testing.T) {
	menu := ClawBotMenuText()
	for _, want := range []string{
		"NAS COMMAND CENTER",
		"╰────────────────",
		"Query Deck",
		"Control Deck",
		"UPS",
		"风扇2",
		"CPU1",
		"风扇控制  `风扇2`",
		"性能模式  `CPU1`",
	} {
		if !strings.Contains(menu, want) {
			t.Fatalf("menu does not contain %q:\n%s", want, menu)
		}
	}
}
