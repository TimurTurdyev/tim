package procs

import (
	"os"
	"path/filepath"
	"testing"
)

// Регрессия: сессия, начатая слэш-командой, называлась текстом скилла "Base directory for this skill".
const transcript = `{"type":"user","isMeta":true,"message":{"content":"<local-command-caveat>The command below was run directly</local-command-caveat>"}}
{"type":"user","message":{"content":"<command-name>/clear</command-name>\n<command-message>clear</command-message>\n<command-args></command-args>"}}
{"type":"user","message":{"content":"<command-message>release-notes</command-message>\n<command-name>/release-notes</command-name>\n<command-args>делаем   план</command-args>"}}
{"type":"user","isMeta":true,"message":{"content":[{"type":"text","text":"Base directory for this skill: /x"}]}}
{"type":"user","message":{"content":"потом обычная просьба"}}`

func TestFirstPrompt(t *testing.T) {
	file := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(file, []byte(transcript), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := firstPrompt(file); got != "/release-notes делаем план" {
		t.Fatalf("имя сессии: %q", got)
	}
	ide := []byte(`[{"type":"text","text":"<ide_selection>код</ide_selection>"},{"type":"text","text":"почини\n кнопку"}]`)
	if got := promptText(ide); got != "почини кнопку" {
		t.Fatalf("просьба после вставки IDE: %q", got)
	}
}
